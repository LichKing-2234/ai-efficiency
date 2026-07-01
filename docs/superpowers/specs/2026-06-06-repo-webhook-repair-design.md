# Repo Webhook Repair Design

**Date:** 2026-06-06
**Status:** Implemented
**Scope:** `backend/`, `frontend/`, `deploy/`, `docs/`
**Related:**
- [2026-06-02-repo-auto-binding-design.md](./2026-06-02-repo-auto-binding-design.md)
- [2026-05-23-global-git-hooks-design.md](./2026-05-23-global-git-hooks-design.md)
- [docs/architecture.md](../../architecture.md)

## Spec Relationship

This spec extends the repo auto-binding contract. The 2026-06-02 design made webhook registration a best-effort post-bind step: deterministic provider binding survives even when webhook registration fails, and the repo is marked `webhook_failed`.

That contract is still correct. This spec adds the missing operational repair path for repos that are already bound to an SCM provider but have no usable webhook. It does not change hook eligibility: `active` and `webhook_failed` repos remain reporting-enabled for `ae-cli` attribution.

## Problem

Today `POST /api/v1/repos/auto-bind-unbound` repairs only unbound repos. It scans repos where `scm_provider_id IS NULL` and `status IN ('active', 'webhook_failed')`. If a repo is already bound and its status is `webhook_failed`, `AutoBindRepo` returns `already_bound` and does not retry webhook registration.

For Bitbucket Server this leaves a practical failure loop:

1. A repo can bind correctly to a Bitbucket Server provider.
2. Webhook creation can fail, marking the repo `webhook_failed`.
3. Admins can manually edit status, but no existing endpoint re-registers the webhook or writes a new `webhook_id` / `webhook_secret`.
4. The current Bitbucket provider supports a webhook callback URL parameter, but the repo provider factory does not pass one, so registration may send an empty callback URL.
5. Incoming Bitbucket webhook handling reads the stored secret but does not validate `X-Hub-Signature` today.

An admin PAT can be sufficient only when the Bitbucket instance accepts the token for REST API calls and the authenticated user has repository admin permission. The current product must still surface upstream 401/403/400 bodies clearly, because permission is only one possible failure mode.

## External Contract Facts

Bitbucket Server/Data Center exposes repository webhook REST APIs under:

```text
/rest/api/1.0/projects/{projectKey}/repos/{repositorySlug}/webhooks
```

The Atlassian REST reference states that creating, listing, reading, updating, deleting, testing, and viewing latest/statistics for repository webhooks requires `REPO_ADMIN` permission on the repository. The create payload includes `name`, `events`, `configuration`, `url`, and `active`; examples include `configuration.secret`.

Atlassian's Bitbucket Data Center webhook documentation says a configured secret signs each request with HMAC, defaulting to HMAC-SHA256, and sends the value in `X-Hub-Signature` as `sha256=<hex>`. The receiver should compute the HMAC over the received body with the stored secret and reject mismatches.

References:

- https://docs.atlassian.com/bitbucket-server/rest/7.10.0/bitbucket-rest.html
- https://confluence.atlassian.com/bitbucketserver0811/manage-webhooks-1252005251.html

## Goals

1. Let admins repair already-bound `webhook_failed` repos without deleting or recreating repo config.
2. Use an explicit backend public callback base URL for GitHub and Bitbucket webhook registration.
3. Pass the Bitbucket callback URL into the Bitbucket provider instead of registering empty webhook URLs.
4. Store a fresh `webhook_id` and `webhook_secret` after successful repair and set repo status back to `active`.
5. Preserve repo binding, usage history, checkpoints, PR records, and local hook eligibility during repair.
6. Validate Bitbucket `X-Hub-Signature` when a repo has a stored webhook secret.
7. Surface upstream SCM errors clearly enough to distinguish invalid callback URL, permission failure, repo missing, and network failure.

## Non-Goals

1. Do not change sub2api, relay providers, or local attribution semantics.
2. Do not make `webhook_failed` ineligible for `ae-cli` hook reporting.
3. Do not turn repair into a scheduled background job in this iteration.
4. Do not rewrite existing repo binding or auto-binding matching logic.
5. Do not expose webhook secrets through frontend or API responses.
6. Do not support project-level Bitbucket webhooks in the first repair implementation.
7. Do not delete repo usage/checkpoint/PR history as part of webhook repair.

## Configuration Contract

Add a backend public URL configuration value:

```yaml
server:
  public_url: "https://ai-efficiency.example.com"
```

Environment variable:

```text
AE_SERVER_PUBLIC_URL=https://ai-efficiency.example.com
```

Rules:

1. `server.public_url` is the canonical public origin that external SCM systems can call.
2. Webhook callback URLs are derived as:
   - GitHub: `{server.public_url}/api/v1/webhooks/github`
   - Bitbucket Server: `{server.public_url}/api/v1/webhooks/bitbucket`
3. The value must be an absolute `http` or `https` URL with a host.
4. In production, empty `server.public_url` makes webhook registration and repair fail fast with a clear configuration error.
5. For local development only, the implementation may fall back to `server.frontend_url` when it is an absolute URL whose origin serves the backend. The fallback must be logged as compatibility behavior.
6. Do not derive callback URLs from request `Host` headers during repair; proxy and ingress host rewriting would make that non-deterministic.

This keeps `server.frontend_url` focused on browser/OAuth/CORS compatibility and gives webhook registration its own public backend callback contract.

## Backend Design

### Service Methods

Add repo service methods:

```go
type RepairWebhookRequest struct {
    Force bool `json:"force"`
}

type RepairWebhookResult struct {
    RepoConfigID   int    `json:"repo_config_id"`
    FullName       string `json:"full_name"`
    PreviousStatus string `json:"previous_status"`
    Status         string `json:"status"`
    WebhookStatus  string `json:"webhook_status"`
    WebhookID      string `json:"webhook_id,omitempty"`
    CallbackURL    string `json:"callback_url,omitempty"`
    Error          string `json:"error,omitempty"`
}

func (s *Service) RepairWebhook(ctx context.Context, repoID int, req RepairWebhookRequest) (RepairWebhookResult, error)
func (s *Service) RepairFailedWebhooks(ctx context.Context, req RepairWebhookRequest) (*RepairWebhookBatchResult, error)
```

`RepairWebhook` flow:

1. Load the repo with its SCM provider and API credential.
2. Reject unbound repos with `repo_unbound` because auto-binding is the correct repair path.
3. Reject inactive repos unless a later spec defines inactive repair.
4. Resolve the provider credential.
5. Create the SCM provider with the configured callback URL.
6. Verify the repo exists through `SCMProvider.GetRepo`.
7. If a stored `webhook_id` exists and `force=false`:
   - For `status=active`, return `webhook_status=already_registered`.
   - For `status=webhook_failed`, continue to create a new webhook because the stored id may be absent or stale.
8. If `force=true` and a stored `webhook_id` exists, delete it best-effort before registering the new webhook. Delete failure should be reported but should not block creating a replacement webhook.
9. Generate a new secret.
10. Register the webhook with `pull_request` and `push` events.
11. On success, update the repo:
    - refreshed SCM metadata
    - `webhook_id`
    - `webhook_secret`
    - `status=active`
12. On failure, update `status=webhook_failed`, preserve existing webhook fields unless a new deletion already succeeded, and return the upstream error summary.

The result must never include `webhook_secret`.

### Batch Repair

Add:

```text
POST /api/v1/repos/repair-webhooks
```

Request:

```json
{
  "force": false
}
```

Access:

- authenticated admin only

First version target set:

- repos with `scm_provider_id IS NOT NULL`
- `status = webhook_failed`

Response:

```json
{
  "summary": {
    "scanned": 3,
    "repaired": 2,
    "already_registered": 0,
    "failed": 1
  },
  "items": [
    {
      "repo_config_id": 42,
      "full_name": "PROJ/repo",
      "previous_status": "webhook_failed",
      "status": "active",
      "webhook_status": "registered",
      "webhook_id": "99",
      "callback_url": "https://ai-efficiency.example.com/api/v1/webhooks/bitbucket"
    }
  ]
}
```

The batch endpoint is synchronous for the first version because webhook repair is one SCM call per repo and the expected count is bounded by failed repos, not all users or all PRs. If production evidence shows large repair batches timing out, move it to the same persisted job pattern as subscription jobs in a later spec.

`force` uses the same semantics as single-repo repair for each item in the batch. The frontend should default to `force=false` and reserve forced replacement for an explicit admin confirmation.

### Single Repo Repair

Add:

```text
POST /api/v1/repos/:id/repair-webhook
```

Access:

- authenticated admin only

Request:

```json
{
  "force": false
}
```

Use cases:

1. Repair one visible `webhook_failed` repo from repo detail.
2. Force replacement if an admin suspects the stored webhook id points to a stale or wrong external hook.

### Provider Factory

Repo service must carry webhook callback configuration into SCM provider construction.

Recommended shape:

```go
type ServiceOptions struct {
    WebhookPublicURL string
}
```

or an equivalent setter if changing `NewService` would create too much churn.

`newBitbucketProvider` must call:

```go
bitbucket.New(baseURL, secret, logger, bitbucketCallbackURL)
```

`bitbucket.RegisterWebhook` should fail before the HTTP call if its callback URL is empty. This prevents silent registration attempts with invalid payloads and makes configuration errors obvious.

GitHub should use the same derived public callback URL model if it currently relies on provider defaults or hard-coded callback behavior.

## Bitbucket Secret Validation

Update Bitbucket inbound webhook parsing or handler validation:

1. If a repo has no stored webhook secret, preserve current compatibility behavior and accept unsigned payloads.
2. If a repo has a stored secret, require `X-Hub-Signature`.
3. Accept only `sha256=<hex>` in the first version.
4. Compute HMAC-SHA256 over the exact request body bytes using the stored secret.
5. Compare with constant-time equality.
6. On mismatch, store a dead letter and return an unauthorized response.
7. Do not log the secret, computed HMAC, or raw request body in normal logs.
8. Resolve the Bitbucket repository identity from the inbound payload before loading the stored secret. Prefer top-level `repository`, then `pullRequest.toRef.repository`, then `pullRequest.fromRef.repository`. Bitbucket Server PR events can omit the top-level `repository`; those events must not fail with `missing repository info` when a PR ref contains the target repository.
9. Repository lookup must tolerate Bitbucket Server identity spelling drift. Try exact `full_name` first, then case-insensitive `full_name`, then normalized identity candidates derived from payload clone/self URLs (`repo_key` and clone URL). This covers split API/SSH hosts and project-key casing such as `SDK/repo` vs stored `sdk/repo` while still returning `404` for truly unconfigured repos.

This aligns Bitbucket security with GitHub-style signed webhook handling and uses the secret that the registration path already stores.

## Frontend UX

### Repo List

Extend the repo health/admin action area:

1. Keep `Auto-bind repositories` for unbound repos.
2. Add `Repair failed webhooks` for admins.
3. Show a concise result summary:
   - repaired
   - already registered
   - failed
4. Refresh the repo list after repair completes.

### Repo Detail

For repos with `status=webhook_failed` and `binding_state=bound`, show:

- `Repair webhook`
- optional `Force replace` confirmation when a stored `webhook_id` exists
- last repair result or error message from the API response

Do not expose `webhook_secret`.

## Error Semantics

| Scenario | API behavior |
| --- | --- |
| Repo not found | `404` |
| Non-admin caller | `403` |
| Repo unbound | `409 repo_unbound`, point admin to auto-bind |
| Repo inactive | `422`, repair is not allowed for inactive repos |
| Missing public callback URL | `422`, configuration error |
| SCM credential missing/decrypt failure | `422` or `500` following existing credential error conventions |
| Bitbucket 401/403 | return repair item `failed` with upstream status/message summary |
| Bitbucket 400 invalid URL/payload | return repair item `failed` with upstream status/message summary |
| Bitbucket repo missing | return repair item `failed`; do not clear local repo |
| Registration success but local save fails | return endpoint error and preserve enough logs to diagnose; do not claim repaired |
| GitHub webhook ping or unsupported signed event | `200 ignored`; do not store a dead letter after payload/signature validation succeeds |
| GitHub webhook missing/invalid signature when a secret is stored | `401`, store a dead letter without logging secret material |
| Bitbucket webhook missing/invalid signature | `401`, store a dead letter without logging secret material |
| Bitbucket PR event without top-level `repository` but with PR ref repository info | resolve the repo from `pullRequest.toRef.repository` or `pullRequest.fromRef.repository`; do not return `400 missing repository info` |
| Bitbucket refs-changed event whose project key casing differs from stored repo config | resolve the configured repo using case-insensitive `full_name` or normalized payload URL identity; validate the stored secret; return `200 processed` for supported push events |

## Testing Strategy

### Backend Unit Tests

1. `RepairWebhook` rejects unbound repos with `repo_unbound`.
2. `RepairWebhook` rejects inactive repos.
3. Missing `server.public_url` causes a clear configuration error before SCM API calls.
4. Bitbucket provider registration receives callback URL `{public_url}/api/v1/webhooks/bitbucket`.
5. Bitbucket registration success writes `webhook_id`, `webhook_secret`, and `status=active`.
6. Bitbucket registration failure leaves or sets `status=webhook_failed` and returns the upstream error summary.
7. `force=true` attempts old webhook deletion before replacement and continues if delete fails.
8. Batch repair scans only bound `webhook_failed` repos.
9. Batch repair does not process unbound repos; those remain the responsibility of auto-bind.
10. Batch repair forwards `force` into each per-repo repair attempt.
11. Bitbucket inbound webhook with stored secret accepts valid `X-Hub-Signature`.
12. Bitbucket inbound webhook with stored secret rejects missing or invalid `X-Hub-Signature`.
13. Bitbucket inbound webhook with no stored secret preserves compatibility behavior.
14. GitHub `ping` and other unsupported events return `200 ignored` after payload/signature validation succeeds.
15. GitHub inbound webhook with a stored secret still rejects invalid `X-Hub-Signature-256`.
16. Bitbucket PR events without a top-level `repository` resolve the repo from `pullRequest.toRef.repository` before falling back to `pullRequest.fromRef.repository`.
17. Bitbucket `repo:refs_changed` delete events match a configured repo when the payload uses uppercase project keys but the stored repo config uses lowercase `full_name`, then validate the stored webhook secret and return `200 processed`.

### Frontend Tests

1. Admin sees `Repair failed webhooks`.
2. Non-admin does not see repair action.
3. Batch repair success shows summary and refreshes repo list.
4. Batch repair failure shows the backend error.
5. Repo detail shows `Repair webhook` for bound `webhook_failed` repo.
6. Repo detail does not expose webhook secret fields.

### Manual Verification

For a Bitbucket Server repo with an admin PAT:

1. Configure `AE_SERVER_PUBLIC_URL` to the externally reachable ai-efficiency origin.
2. Create or choose a bound repo in `webhook_failed`.
3. Run repair from UI or API.
4. Verify Bitbucket shows an active webhook named `ai-efficiency`.
5. Verify the webhook URL is `/api/v1/webhooks/bitbucket`.
6. Trigger a PR event or use Bitbucket's test connection / latest invocation UI.
7. Confirm ai-efficiency receives and processes the event.
8. Confirm invalid signature payload is rejected when a secret is stored.

## Documentation Updates During Implementation

Implementation must update:

1. `docs/architecture.md` to describe webhook repair as the current SCM operations path.
2. `docs/superpowers/specs/2026-06-02-repo-auto-binding-design.md` to link to this repair spec and clarify that auto-bind does not repair already-bound webhook failures.
3. `deploy/config.example.yaml` and `deploy/.env.example` for `server.public_url` / `AE_SERVER_PUBLIC_URL`.
4. Any frontend route/API docs that mention repo health actions.

Historical specs should remain historical. Do not back-edit old specs to claim they always included webhook repair.
