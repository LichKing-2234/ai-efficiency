# Repo Auto Binding Design

**Date:** 2026-06-02
**Status:** Review Requested
**Scope:** `backend/`, `frontend/`, `docs/`
**Related:**
- [`2026-04-15-cli-start-auto-repo-sync-design.md`](./2026-04-15-cli-start-auto-repo-sync-design.md)
- [`2026-04-14-scm-credentials-provider-binding-design.md`](./2026-04-14-scm-credentials-provider-binding-design.md)
- [`2026-05-23-global-git-hooks-design.md`](./2026-05-23-global-git-hooks-design.md)

Project-level architecture and current runtime boundaries remain anchored in [`docs/architecture.md`](../../architecture.md).

## Overview

The current repository lifecycle intentionally allows a repo to exist before it is bound to an SCM provider. That made `ae-cli init`, hook eligibility, checkpoints, and local attribution independent of admin SCM setup. It also created a new operational gap: once Code Platforms are already configured, newly discovered repos can remain `unbound` even when the correct provider is obvious from the remote host.

This design adds deterministic automatic repo-to-Code-Platform binding while preserving the existing admin-managed binding boundary:

- new repos can bind automatically when exactly one active Code Platform matches the repo remote host
- existing unbound repos can be repaired by an admin-triggered batch action
- read-only hook and eligibility paths stay read-only
- ambiguous or unknown matches remain unbound and are handled through the existing manual binding UI

The goal is to reduce unnecessary admin work without turning SCM provider binding into a hidden guessing system.

## Relationship To Existing Specs

This spec is an evolution of [`2026-04-15-cli-start-auto-repo-sync-design.md`](./2026-04-15-cli-start-auto-repo-sync-design.md). That spec deliberately made automatic repo discovery independent from provider binding and listed automatic provider inference as a non-goal for the first version.

The new contract keeps the important part of that split: repo durable state may still exist without an SCM provider, and sessionless attribution still works for unbound repos. It changes only the follow-up behavior when the backend can make a deterministic provider choice.

This spec does not rewrite the older specs. Historical specs continue to document the contract at the time they were written.

## Goals

1. Automatically bind newly discovered repos when the correct active Code Platform is deterministic.
2. Provide an admin-only batch repair action for existing unbound repos.
3. Keep `resolve-remote` and `hook-eligible` read-only.
4. Preserve manual binding as the fallback for no-match and ambiguous cases.
5. Keep a successful binding even if webhook registration fails.
6. Make binding decisions explainable in API responses, frontend summaries, and logs.

## Non-Goals

1. Do not bind repos directly to credentials.
2. Do not let ordinary users change repo provider binding.
3. Do not introduce a scheduled background auto-repair job in this version.
4. Do not call SCM APIs from hook-time read-only eligibility paths.
5. Do not make provider matching depend on repo names alone.
6. Do not add a new `repo_host` or `clone_host` field in this version.

## Core Decisions

| Topic | Decision | Reason |
| --- | --- | --- |
| Trigger scope | Auto-bind on new repo creation and admin batch repair | Fixes new and old data without hidden periodic writes |
| Read-only paths | `resolve-remote` and `hook-eligible` never bind | Preserves hook/cache contracts and avoids surprise writes |
| Matching key | Canonical remote host to canonical provider host | Host is stable and independent of SSH/HTTPS URL spelling |
| GitHub SaaS | `https://api.github.com` matches repo host `github.com` | Existing provider config uses API URL while remotes use clone host |
| Enterprise and Bitbucket | Match by same canonical host | Avoids guessing enterprise URL topology |
| Ambiguous matches | Keep repo unbound | Prevents accidental binding when multiple providers share a host |
| Post-bind work | Best-effort metadata refresh and webhook registration | Binding should survive transient SCM or webhook failures |

## Provider Matching

The backend derives a canonical repo host from the repo identity:

1. Prefer the parsed host from `repo.clone_url`.
2. If the clone URL is unavailable or unparsable, use the host prefix from `repo.repo_key`.
3. Lowercase the host and remove a leading default port when it is present.

The backend derives a canonical provider host from each active `ScmProvider`:

1. Parse `scm_provider.base_url`.
2. Lowercase the host and remove a leading default port when it is present.
3. For `type=github`, map `api.github.com` to `github.com`.
4. For GitHub Enterprise and Bitbucket Server, keep the parsed host as-is.

A repo is auto-bindable only when exactly one active provider has the same canonical provider host as the repo host.

Result reasons:

- `matched`: exactly one active provider matched and the repo was bound
- `already_bound`: repo already has a provider; auto-bind did not change it
- `no_match`: no active provider matched the repo host
- `ambiguous`: more than one active provider matched the repo host
- `invalid_repo_host`: the repo host could not be derived
- `provider_error`: a deterministic provider was selected and bound, but provider setup or post-bind API work failed

Provider status matters: only `status=active` providers participate in automatic matching.

## Backend Data Flow

### New Repo Creation

When `FindOrCreateFromRemote` creates a new repo:

1. Derive the existing `RepoIdentity`.
2. Save the unbound repo row using the current repo metadata.
3. Attempt automatic binding.
4. If one active provider matches, set `scm_provider_id`.
5. After binding, run post-bind work best-effort:
   - call `SCMProvider.GetRepo(full_name)` to verify the repo and refresh metadata
   - register the existing pull request and push webhook
   - set `status=webhook_failed` if webhook registration fails
6. Return the repo with provider edge loaded.

Automatic binding failures must not make `EnsureFromRemote` fail unless the repo row itself cannot be created or loaded. The caller should still receive a repo record that can be used for attribution.

When `FindOrCreateFromRemote` finds an existing repo, it refreshes metadata as today and does not overwrite an existing binding. Existing unbound repo repair is handled by the admin batch endpoint, not by ordinary ensure calls.

### Manual Repo Creation

The existing manual creation paths keep their visible behavior:

- `POST /api/v1/repos` remains an explicit provider-backed creation path.
- `POST /api/v1/repos/direct` may create an unbound repo and should run the same automatic binding attempt when `scm_provider_id` is omitted.

If an admin explicitly supplies `scm_provider_id`, that explicit choice wins and automatic matching does not override it.

### Admin Batch Repair

Add an admin-only endpoint:

```text
POST /api/v1/repos/auto-bind-unbound
```

The endpoint scans repos where:

- `scm_provider_id IS NULL`
- `status IN ('active', 'webhook_failed')`

For each repo, it applies the same matching and post-bind logic as new repo creation.

The response returns a summary and per-repo results:

```json
{
  "summary": {
    "scanned": 12,
    "bound": 8,
    "already_bound": 0,
    "skipped_no_match": 2,
    "skipped_ambiguous": 1,
    "webhook_failed": 1,
    "errors": 0
  },
  "items": [
    {
      "repo_config_id": 42,
      "repo_key": "github.com/acme/platform",
      "full_name": "acme/platform",
      "result": "matched",
      "scm_provider_id": 3,
      "scm_provider_name": "GitHub",
      "webhook_status": "registered"
    }
  ]
}
```

The batch endpoint does not process inactive repos. Admins can manually re-activate and bind those if needed.

## Post-Bind Behavior

Post-bind work is best-effort and isolated from the binding decision.

When provider API verification succeeds:

- refresh `name`
- refresh `full_name`
- refresh `clone_url`
- refresh `default_branch`

When webhook registration succeeds:

- store `webhook_id`
- store `webhook_secret`
- set `status=active`

When webhook registration fails:

- keep the provider binding
- clear or leave empty webhook fields
- set `status=webhook_failed`
- return/report `webhook_status=failed`

When provider API verification fails after a deterministic provider match:

- keep the provider binding
- leave existing repo metadata unchanged
- report `provider_error` in the batch item or creation diagnostics
- log the provider id and repo identity without secret material

This keeps the binding decision based on deterministic Code Platform metadata. Provider credential or SCM API problems are treated as operational repair issues, not as reasons to guess a different provider.

## Frontend UX

The frontend should stay small and operational:

1. Add an admin-only action in the repo list health section: `Auto-bind repositories`.
2. Hide the action for non-admin users.
3. On click, call `POST /api/v1/repos/auto-bind-unbound`.
4. Show a concise result summary:
   - bound count
   - skipped no-match count
   - skipped ambiguous count
   - webhook failed count
   - error count
5. Refresh the repo list after a successful batch call.
6. Keep the existing repo detail manual binding control as the fallback.

The UI should not present automatic binding as a wizard. It is an admin repair action for deterministic cases only.

## Error Handling And Observability

Backend logs should include:

- repo id
- repo key
- repo full name
- selected provider id when one was selected
- result reason
- post-bind webhook status

Logs must not include tokens, credential payloads, webhook secrets, or raw secret-bearing URLs.

API error handling:

- endpoint-level failures return normal backend error responses
- per-repo batch failures stay in `items` when the batch can continue
- `repo_unbound` remains the stable error for SCM-dependent operations on unbound repos

## Documentation Updates During Implementation

When this design is implemented:

1. Update [`docs/architecture.md`](../../architecture.md) to describe deterministic repo binding from configured Code Platform metadata.
2. Keep older specs as historical records.
3. If implementation changes the matching contract beyond this spec, update this spec or write a newer one rather than rewriting older specs.

## Testing Strategy

### Backend

1. GitHub SaaS provider `https://api.github.com` matches repo remote `https://github.com/acme/platform.git`.
2. GitHub SSH remote `git@github.com:acme/platform.git` matches `https://api.github.com`.
3. Bitbucket Server provider and repo remote match by same host.
4. No matching provider keeps the repo unbound.
5. Multiple active providers with the same canonical host keep the repo unbound with `ambiguous`.
6. Inactive providers are ignored.
7. New repo creation auto-binds only when there is one active match.
8. Existing bound repos are not overwritten by auto-binding.
9. `POST /api/v1/repos/auto-bind-unbound` processes only unbound active/webhook_failed repos.
10. Webhook registration failure keeps the binding and marks `webhook_failed`.
11. Provider API verification failure keeps the binding and returns/reports `provider_error`.
12. `resolve-remote` and `hook-eligible` do not create repos and do not bind repos.

### Frontend

1. Admin users see the batch auto-bind action on the repo list.
2. Non-admin users do not see the batch action.
3. Clicking the action calls the batch endpoint and displays the result summary.
4. The repo list refreshes after the batch action.
5. Manual binding remains available on repo detail for skipped repos.

## Acceptance Criteria

This design is implemented when all are true:

1. A newly discovered repo binds automatically when exactly one active Code Platform matches its remote host.
2. Existing unbound repos can be batch repaired by an admin action.
3. Ambiguous and no-match repos remain unbound and manually bindable.
4. Hook eligibility and resolve paths remain read-only.
5. Webhook registration failure no longer prevents a deterministic provider binding.
6. Tests cover GitHub SaaS host normalization, Bitbucket host matching, ambiguity, no-match, and batch repair behavior.
