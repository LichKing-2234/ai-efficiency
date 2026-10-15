# Account Configuration Copy Design

## Status

This document records an approved set of scope decisions for copying a narrow,
allow-listed subset of account-level configuration from one existing Relay
(Sub2API) account onto other existing accounts, together with the implementation
of that copy on `feat/account-config-copy`. It authorizes that implementation. It
does not authorize a schema change, staging, a production release, or any Relay
mutation beyond the copy endpoint described here. Every one of those requires
separate authorization, consistent with
`docs/adr/0002-managed-relay-relationship-operations.md`.

The decisions below were reached by interview on 2026-10-14/15 and supersede an
earlier "pre-create dormant account rows" direction, which was abandoned by the
requester. That earlier direction's artifacts (two dormant rows in the
production Sub2API instance) are not part of this design.

**Authority and privacy-invariant note — the largest risk in this design.**
`docs/adr/0002-managed-relay-relationship-operations.md:7` states that "AI
Efficiency owns the expected state only for relationships explicitly managed by
a Relay Group Mapping; Sub2API remains the observed runtime and owns every
unrelated relationship." Account configuration is not a relationship, and this
design has AI Efficiency write it. That is an extension of the platform's write
authority beyond ADR-0002's stated scope.

The read side is worse than merely unaddressed: it contradicts an explicit
existing instruction.

- `docs/contracts/relay-group-mapping.md:221-230` carries the rule: `Account.PlanType`
  is the one field the relationship read takes out of the credential blob, and
  every other credential and provider-private field stays unrepresented on that
  path.
- `backend/internal/relay/sub2api.go:2370` carries the same rule in code:
  "Only plan_type is read out of the credential blob. Keep it that way; the rest
  of the blob is credential material."
- The same contract places account configuration outside Mapping authority and
  requires a relationship fingerprint to never contain Account private
  configuration.

This design reads one further credential-blob field, `credentials.model_mapping`,
and applies it to other accounts. Sub2API's admin API does return non-sensitive
credential keys such as `model_mapping`, and returns `extra`, to an admin API-key
caller — only sensitive keys are stripped — so the data is reachable. What is
missing is authorization in this repository's own terms.

This is surfaced rather than silently widened. It is resolved by
`docs/adr/0004-allow-listed-account-configuration.md`, which records AI
Efficiency's authority over an exhaustive allow-list. The contract statement has
since been amended to name that allow-list and to scope itself to the
relationship read; the remaining code comment is tracked in the follow-up issue.

## Spec Relationship

- Read alongside `docs/contracts/relay-group-mapping.md`,
  which remains the contract for account-to-group relationships. This document
  does not change Mapping authority, drift detection, or the Replan Baseline.
- `backend/internal/relay/provider.go` remains the only Relay boundary. The new
  capability is an optional interface on that boundary, like every other
  capability there.
- The abandoned direction's exploratory notes are not a source of truth.

## Problem

Seats on Sub2API are created through Sub2API's own OpenAI OAuth authorization,
because authorizing an upstream account requires a human in a browser. Once the
account exists, every seat is then customized by hand: in the production
instance all 60 existing seats carry `credentials.model_mapping` (13 entries
each, for example `gpt-5.6-terra -> gpt-6.1-sol`) plus a set of feature flags in
`extra`. Re-entering that by hand for each new seat is the cost this design
removes.

There is no way to copy account configuration today:

- AI Efficiency exposes no account configuration at all. Its `relay.Account`
  projection (`backend/internal/relay/types.go:87-100`) carries `ID`, `Name`,
  `Platform`, `Type`, `Status`, `Schedulable`, `PlanType`, and group
  relationships — and nothing else. `plan_type` is the single field deliberately
  lifted out of the credential blob, and `extra` is never declared in any Go
  struct.
- Sub2API has no account configuration copy either. `POST /api/v1/admin/accounts/:id/duplicate`
  rejects rotating-credential accounts outright (`canDuplicateAccountType` allows
  only `apikey`, `upstream`, `bedrock`, `service_account`), and
  `POST /api/v1/admin/accounts/data` only creates new accounts, never updates
  existing ones.

## Goals

- An administrator can select one source Relay account and one or more target
  Relay accounts, review the configuration diff, and apply the source's
  allow-listed configuration to every target.
- The operation never reads, transports, stores, or overwrites credential
  material (`access_token`, `refresh_token`, `id_token`, identity anchors).
- The operation cannot race Sub2API's own token-refresh workers in either
  direction.
- Every target is read back and verified before the operation reports success.
- The platform stores no account configuration, no credential material, and no
  new table for this feature.

## Non-Goals

- Column-level account fields: `concurrency`, `priority`, `rate_multiplier`,
  `auto_pause_on_expired`, `notes`, `load_factor`. Deferred; the only endpoint
  that can write them is also the endpoint this design deliberately avoids.
- Proxy (`proxy_id`) and group (`group_ids`) relationships. Group relationships
  are Mapping authority (ADR-0002). Proxy is not part of the customization the
  requester wants copied.
- Creating, deleting, enabling, or disabling accounts, and any OAuth
  authorization flow. Accounts are created and authorized in Sub2API.
- Any background or automatic application. The administrator confirms each
  operation explicitly; ADR-0002 rejects background repair.
- A custody registry, custodian/owner fields, handover state, or procurement
  data. Discussed and explicitly dropped when the dormant-row direction was
  abandoned.
- Any persistence of copied configuration, in any table, for any duration.

## Capability Contract

Two narrow optional interfaces on the Relay boundary. Both follow the existing
capability pattern in `backend/internal/relay/provider.go` (an interface declared
beside `AccountRelationshipReader`/`AccountRelationshipUpdater`, implemented by
`sub2apiRelay`, type-asserted by the caller).

### `AccountConfigurationReader`

```go
// AccountConfigurationReader exposes only the allow-listed account-level
// settings. It never returns credential material.
type AccountConfigurationReader interface {
    ReadAccountConfigurations(ctx context.Context, platform string) (map[int64]AccountConfiguration, error)
}

// AccountConfigurationApplier merges only allow-listed keys into the stored
// configuration, leaving every other key in place.
type AccountConfigurationApplier interface {
    ApplyAccountConfiguration(ctx context.Context, accountIDs []int64, config AccountConfiguration) error
}
```

`AccountConfiguration` carries the allow-list and nothing else. It is produced by
a narrowed wire decode that declares only allow-listed keys, so credential
material and every other private configuration field stays unmaterialized rather
than being decoded and then dropped. The allow-listed `extra` values stay as raw
JSON, so an unexpected upstream type cannot fail the whole read. A second,
exhaustive key list gates both the projection and the outgoing payload. The two
are kept in step by `TestAccountConfigurationAllowListMatchesItsWireStruct`,
which fails when a field or a list entry is added without the other, and
`TestReadAccountConfigurationsProjectsOnlyAllowListedKeys` asserts with a
whole-value diff that a poisoned fixture's tokens, `chatgpt_account_id`,
`codex_fingerprint_seed`, and runtime telemetry keys cannot appear in the result.

Allow-listed credential keys:

- `credentials.model_mapping`

Allow-listed `extra` keys:

- `privacy_mode`
- `openai_long_context_billing_enabled`
- `openai_oauth_responses_websockets_v2_enabled`
- `openai_oauth_responses_websockets_v2_mode`
- `codex_fingerprint_mode`
- `codex_cli_only`
- `codex_cli_only_allow_app_server`
- `auto_reset_credit_enabled`
- `auto_reset_credit_5h_threshold`
- `auto_reset_credit_7d_threshold`
- `openai_compact_mode`

Explicitly excluded, with reasons:

- `codex_fingerprint_seed` — Sub2API must mint a fresh seed per account
  (`sanitizedCodexFingerprintExtraUpdates` strips it on write). Copying it would
  make two accounts share a fingerprint.
- All runtime/telemetry keys: `codex_5h_*`, `codex_7d_*`, `codex_primary_*`,
  `codex_secondary_*`, `codex_usage_updated_at`, `codex_credits_snapshot`,
  `codex_reset_credit_snapshot`, `email`.
- `upstream_model_metadata` — present on only 6 of 60 seats; it is
  account-specific upstream metadata, not shared customization.

### `AccountConfigurationApplier`

```go
// AccountConfigurationApplier merges the allow-listed configuration onto
// existing accounts without carrying credential material.
type AccountConfigurationApplier interface {
    ApplyAccountConfiguration(ctx context.Context, accountIDs []int64, config AccountConfiguration) error
}
```

## Write Semantics

The apply primitive is:

```
POST /api/v1/admin/accounts/bulk-update
{"account_ids": [...], "credentials": {"model_mapping": {...}}, "extra": {...allow-list...}}
```

This is the only admin path that performs the change as a single atomic,
database-side, key-level JSONB merge
(`account_repo.go:3084` `credentials = COALESCE(credentials, '{}'::jsonb) || $1::jsonb`;
`:3138` `extra || $1::jsonb`). There is no Go-side read-modify-write, so it
cannot overwrite a concurrent token refresh with a stale credential snapshot and
cannot be overwritten by one. It writes only these two columns; `status`,
`schedulable`, and every other column are absent from the `SET` list.

`model_mapping` is replaced **whole** (the merge replaces the value at the
`model_mapping` key). Per-key merging of two sources' mappings would produce a
mapping combination nobody has validated.

Rejected primitives:

| Primitive | Why rejected |
| --- | --- |
| `POST /admin/accounts/:id/apply-oauth-credentials` | Intended to overwrite tokens. Its `MergeCredentials` runs Go-side and the write replaces the entire `credentials` column (`account_repo.go:541`), so a refresh committing between read and write is clobbered. |
| `POST /admin/accounts/batch-update-credentials` | `field` is a hard `oneof=account_uuid org_uuid intercept_warmup_requests`; it cannot express `model_mapping` and has no `extra` field. Its snapshot is the oldest of all paths. |
| `PUT /admin/accounts/:id` | Read-modify-write; replaces the entire `credentials` **and** `extra` columns from a stale snapshot, and drops any pre-existing `extra` key the caller does not send. |
| `POST /admin/accounts/data` | Creates new accounts; requires full credentials; never updates. |
| `POST /admin/accounts/:id/duplicate` | Rejects OAuth accounts; creates a new account. |

Consequences the implementation must handle:

1. **Success is not proof.** `BulkUpdateAccounts` reports success for account IDs
   that do not exist (the affected-row count is discarded). Every apply must be
   followed by a read-back through `AccountConfigurationReader` and a
   key-by-key comparison against the source, and only then reported as applied.
2. **No `Idempotency-Key`.** The endpoint is not wrapped in
   `executeAdminIdempotent`. This is acceptable because a key-level merge is
   naturally idempotent: re-running the same apply converges to the same state.
3. **Cache freshness must be verified, not assumed.** The endpoint emits
   `SchedulerOutboxEventAccountBulkChanged`, but the acceptance criteria require
   an observed prompt effect, not an inference from the event name.

### Operation shape

- One apply call for the selected target IDs, because the merge is a single
  statement.
- Execution is synchronous and capped at 100 targets per copy
  (`MaxAccountConfigurationCopyTargets`), rather than introducing a persisted
  job.

  This follows the repository's own lighter precedent for a bulk admin
  operation. Mapping Renewal states: "Execution is synchronous and reports
  per-member `succeeded`, `skipped`, or `failed` results in the current dialog.
  It does not create a background job or renewal-history entity"
  (`docs/contracts/relay-group-mapping.md:546-548`).
  `AdminSubscriptionJob` was evaluated and rejected as overkill: it would cost an
  Ent schema, a migration, three routes, and a polling lifecycle for an operation
  that is one SQL statement per batch, and its `MaxTargets = 500` far exceeds any
  realistic account count.
- Per-target results are reported independently, and each verdict comes from a
  read-back comparison rather than from the write response, because the endpoint
  reports success for identifiers that do not exist. A target carries whether it
  converged plus, when it did not, the reason: the account was absent at
  read-back, or its configuration still differed from the baseline. There is no
  batch-level state machine.
- A baseline that carries no allow-listed configuration is rejected before any
  write, because the copy would otherwise report success while changing nothing.
- Failure of one target does not abort the others.
- **No temporary disable/enable window.** An earlier draft disabled scheduling
  around the write. The atomic merge makes a torn state impossible, and the
  endpoint does not touch `status`/`schedulable`, so the window buys nothing and
  would add a "left disabled after a crash" failure mode. It is dropped.
- **No operation record.** Nothing is persisted. A retry is the recovery path;
  re-running an apply is safe and is also how an operator confirms state.

### Window between account creation and configuration apply

A normally created account lands `status='active'`, `schedulable=true`
(`buildAccountForCreate` hardcodes both) and is not yet customized. During that
window it can receive traffic with default configuration. This design does not
close that window, because the account is created by a human in Sub2API outside
AI Efficiency's control. It is an operational procedure, and the new admin
surface should make the gap visible by showing, per account, whether its
allow-listed configuration matches a chosen source.

## Frontend Contract

A new admin-only surface, `Relay Account Management`, hosted by a new route and
view rather than added to `RelayPlanningView`:

- Lists every Relay account on the platform with `name`, `#id`, `platform`,
  `type`, `status`, and `schedulable`. The list is not paginated in this phase;
  a platform holds tens of accounts, not thousands.
- The administrator designates one account as the baseline. Its allow-listed
  configuration is shown read-only, and every other row reports how it deviates:
  a match, or the counts and names of missing, differing, and target-only keys.
- Deviation is the primary reading of the screen, because most accounts already
  share one configuration. A filter shows only the accounts that deviate, and a
  matching account is not selectable as a target.
- Targets are multi-selected from the deviating accounts. Confirmation states
  plainly how many targets, which baseline, that credential material is neither
  read nor written, and that the operation is synchronous and leaves no record.
- Results are per target and come from the server's read-back, and the list
  refreshes afterwards so the new state is visible.
- The screen carries no create, delete, enable, disable, or authorization action
  in this phase.

Terminology follows the repository rule that "Relay" is the platform's word for
Sub2API in user-facing surfaces.

Two domain terms are introduced by this design. They are defined here rather
than edited into the root `CONTEXT.md`, because that file — which
`docs/agents/domain.md` designates as this repository's glossary — is currently
**untracked** in the working tree (`git status` reports `?? CONTEXT.md`) and so
is absent from this branch. When it is committed, these terms must be added
verbatim.

**Relay Account Configuration**:
The allow-listed account-level settings that AI Efficiency may read from and
apply to an existing Relay account: the model mapping plus a fixed set of
feature flags. It excludes credential material, column-level settings, proxy,
and group relationships.
_Avoid_: Account credentials, account settings, Model mapping

**Relay Account Configuration Copy**:
An explicit administrator-confirmed operation that applies one account's Relay
Account Configuration to other existing accounts. It never reads or writes
credential material, never changes group relationships, and is verified by
read-back rather than trusted from the write response.
_Avoid_: Account template, account clone, config sync

## Unchanged Behavior

- `relay.Account` and every existing capability keep their current shapes and
  privacy guarantees. The new reader is additive.
- Mapping authority, drift detection, Replan, Rebind, and Account pool management
  are unchanged. This feature never writes `group_ids`.
- Sub2API's own account creation, OAuth authorization, and token refresh are
  unchanged and remain the only paths that write credential material.
- No Ent schema change. Startup auto-migration (`backend/cmd/server/main.go:230-240`)
  has nothing to apply, and no `helm rollback` schema gap is introduced.

## Acceptance Criteria

1. A test proves the projection drops every non-allow-listed key: a fixture
   containing `access_token`, `refresh_token`, `id_token`, `chatgpt_account_id`,
   and `codex_fingerprint_seed` yields a projection containing none of them.
2. A test proves the apply request body contains only `account_ids`,
   `credentials.model_mapping`, and allow-listed `extra` keys — no token keys, no
   `status`, no `schedulable`, no `group_ids`.
3. A test proves a target whose read-back does not match the source is reported
   as mismatched, not applied.
4. A test proves an apply against a nonexistent account ID is reported as
   not-found rather than success.
5. Re-running the same apply twice leaves both targets byte-identical and reports
   success both times.
6. Against one real, recently created production account: apply the allow-list
   from an existing seat, read back, compare every allow-listed key, and confirm
   the target's `access_token`/`refresh_token`/`_token_version` values are
   unchanged from before the apply (proving the merge did not touch tokens).
7. Against the same account, confirm the copied `model_mapping` is in effect
   promptly after the apply, rather than assuming the outbox event is sufficient.

Test data must follow the repository fixture rule: no real user names, company
domains, emails, passwords, tokens, API keys, or real subscription group names —
use the `alice@example.com` / `Group Alpha` fixtures. New Relay capability tests
follow `backend/internal/relay/sub2api_test.go:26-35` (`newTestProvider`) and
`backend/internal/relay/user_usage_origin_test.go:406-409` (`writeOriginEnvelope`),
and extend the whole-DTO `cmp.Diff` privacy assertion pattern at
`backend/internal/relay/account_relationship_test.go:61-67`. Service and handler
coverage goes in `backend/internal/relayplanning/service_test.go` and
`backend/internal/handler/relay_planning_test.go`; frontend coverage in
`frontend/src/__tests__/` with new i18n keys checked by `i18n.test.ts`.

## Documentation Obligations

- New spec rather than an edit to the 2026-08-19 Relay contract, which is a
  historical design record. This document must state its relationship to that
  contract, which the `Spec Relationship` section does.
- `docs/architecture.md`: the Relay Planning paragraph (line 123, currently
  ending "There is no background synchronizer or direct sub2api database
  coupling.") and the API-surface table row for the Relay integration.
- `CONTEXT.md`: the two terms defined in `Frontend Contract`, when that file is
  committed.
- `backend/internal/relay/provider.go`: the two new capability interfaces, since
  that file is the documented Relay surface (`AGENTS.md:239`).
- Commit label: the design-record commits are 设计文档更新 and the implementation
  commit is a 当前实现变更 (`AGENTS.md:141`). Note that `relay` is not in
  `AGENTS.md`'s listed commit scopes, although the branch's own history uses
  `feat(relay):`.

## Settled During Implementation

Recorded here because these were open in the design interview and are now
decided; the decisions are current behavior, not remaining work.

- A copy is capped at 100 targets (`MaxAccountConfigurationCopyTargets`).
- The baseline is an explicitly designated account. Any account may be
  designated, so no marker or source-authority rule is needed.
- A baseline carrying no allow-listed configuration is rejected before any write,
  because the copy would otherwise report success while changing nothing.

Remaining work lives in
[issue #498](https://github.com/LichKing-2234/ai-efficiency/issues/498): the
service-layer tests for the read-back verdict, and naming the wire struct in the
relationship credential comment.
