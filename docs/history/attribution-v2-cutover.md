# Attribution V2 Cutover and Legacy Cleanup

> Historical evidence. This file records why and how the v1 POC was replaced,
> cut over, reset, and removed. Current attribution behavior is owned only by
> [Attribution V2](../contracts/attribution-v2.md).

This record contains sanitized aggregate evidence and authority boundaries. It
omits raw Requests, users, credentials, database rows, production connection
details, implementation checklists, and old remaining-work narration.

## Source Dispositions

| Source | Historical value retained |
| --- | --- |
| [v1 compact-ledger POC design](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/specs/2026-08-05-codex-token-attribution-ledger-poc-design.md) | POC accounting vocabulary, compression result, privacy boundary, and limitations. |
| [v2 qualification evidence](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-08-12-codex-attribution-v2-qualification-evidence.md) | Failure matrix, fail-closed canaries, and shadow-epoch admission proof. |
| [production cutover runbook](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-08-12-codex-attribution-v2-cutover-runbook.md) | Ordered export/reset/cutover gates and forward-only rollback boundary. |
| [legacy cleanup preflight and closeout](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-08-13-attribution-v1-cleanup-preflight.md) | Ordinary-adoption gate, two-phase removal, no-`CASCADE` schema conservation, and final production evidence. |

## V1 Compact-Ledger POC

The v1 POC tested whether Codex local Token atoms could be reduced to compact,
privacy-safe accounting facts without persisting one central row per response.
It used rollout JSONL as the primary Token source, SQLite only as a fallback for
a conversation without usable JSONL facts, and optional AE OTLP as advisory
Request correlation rather than another Token source.

The POC separated measured Token into bound and unbound pools. Shared
multi-Repository evidence was counted once globally, while allocation revisions
changed the presentation vector without mutating the immutable measurement.
Repository/worktree evidence and Git checkpoint/rewrite/lineage were retained;
prompt, response, code, patch, command, local path, raw JSONL/SQLite/OTLP, and
legacy Session payloads were excluded from compact upload.

Its local sample compressed 11,531 Token atoms into 312 bucket payloads in
about 0.8 seconds. Counting each bucket plus its first allocation revision gave
624 central rows, 94.59 percent fewer than the atoms. This demonstrated compact
storage and replay identity; it was not a production latency or correctness
claim.

The POC could not supply the later production guarantees:

- Request correlation was advisory and time-bounded rather than an exact
  provider-owned usage identity;
- immutable buckets plus complete allocation revisions did not model a bounded
  hot proof lifecycle and long-lived globally deduplicated pool;
- v1 Activity exposed Bucket/Request-oriented operational concepts that were
  inappropriate for the product read model;
- separate `aer_*` and `aeo_*` credentials and AE-managed OTel added an active
  telemetry surface unrelated to the final accounting source;
- production cutover, retry/finalization, epoch isolation, conservation under
  rewrite, and destructive cleanup were outside POC authority.

The v1 data was therefore disposable POC evidence, not history to backfill into
formal v2.

## V2 Qualification and Fail-Closed Canaries

Qualification first established executable coverage for exact owner/provider
identity, Request reconciliation, replay/conflict/response loss, multi-Request
and late Request, final-attempt/finalization boundaries, multi-replica leases,
direct-to-shared conservation, rewrite/orphan/cherry-pick, timezone aggregation,
Usage denominator completeness, authorization, pagination, and privacy.

The admission rule required one complete Request-to-commit-to-pool-to-Activity
chain in `shadow_v2`, while formal Activity/readiness stayed empty. Synthetic
qualification alone was insufficient; a separately authorized real canary had
to pass the same identity boundary.

### WebSocket identity failure

The first real canary produced deterministic mutation/commit evidence and local
Token, but the active Codex WebSocket path exposed no trusted persistent
official Request identity. Handshake IDs returned no matching Relay usage;
existing AE OTLP evidence carried no Request ID; an opt-in timing trace exposed
a logical ID but was unavailable during normal operation.

The canary stopped before claim ingest. Time adjacency, Token similarity,
handshake identity, and stderr-only evidence were explicitly rejected. This
failure prevented a superficially plausible but unverifiable allocation.

### Forced-HTTP identity mismatch

A later forced-HTTP canary persisted a shadow group and ACKed three claims, but
reconciliation remained pending: Codex had stored `resp_*`, while Relay indexed
the official rows by normalized `client:<x-client-request-id>`. The result was
treated as an identity-contract failure, not accepted usage.

The scanner was narrowed to successful HTTP completion logs, exact thread/turn
binding, and one normalized `client:` prefix. Other headers, targets, failed
responses, unmatched turns, and ambiguity stayed invalid.

### Successful shadow admission

CLI release
[`ae-cli/v0.2.0-preview.4`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/ae-cli/v0.2.0-preview.4)
then produced a successful forced-HTTP shadow canary: two exact Request claims
reconciled on their first attempt into one direct shadow pool with 19,607 Token.
Shadow Activity showed the pool; formal Activity stayed at zero and readiness
remained waiting. This completed qualification without authorizing cutover or
deletion.

## Production Cutover

The production change required independent authority for candidate code, CLI
release, platform release, deployment, and v1 deletion. A missing permission or
failed volatile gate was a no-go even when the software tests were green.

Required gates included:

- one immutable candidate with backend/CLI/frontend/build/race/role/scale/query
  verification and no blocking review finding;
- a real shadow canary and proof that shadow could not affect formal reads;
- v1 write rejection, one explicit UTC `cutover_at`, and agreement across every
  serving replica;
- zero near-expiry/finalization/cleanup errors and stable reconciliation;
- current backup/restore plus a private v1 export verified by SHA-256 outside
  ephemeral database-host storage;
- aggregate capture of shadow/canary data before exact cleanup;
- explicit operator authority for every release, deployment, and destructive
  step.

The ordered production sequence was:

1. Deploy the candidate with v1 writes gated while formal reads stayed off.
2. Freeze one immutable UTC cutover boundary.
3. Export v1 buckets/revisions, verify the manifest, and capture aggregates.
4. Remove/exclude exact shadow/canary data; never relabel it formal.
5. Enable formal writes and formal-only Activity/readiness at the boundary.
6. Delete revisions before their buckets in one locked transaction and assert
   both v1 tables empty before commit.
7. Read back v1 rejection, v2 acceptance, formal isolation, Activity/readiness,
   boundary comparison behavior, zero v1 totals, and operational gates.

Staging rehearsed the same order and reset. Production platform
[`v0.1.0-preview.83`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.83)
froze `cutover_at=2026-08-12T11:22:58Z`. One response-header canary produced a
4,395-Token formal pool. A separate invalid request-side-ID canary was removed
under exact guards while still unmaterialized, changing no pool. The v1 reset
deleted 22 revisions and 21 buckets; both tables and all v1 totals read back
zero, while formal and shadow pools remained isolated.

## Forward-Only Rollback

Before the v1 reset, the application could roll back while v1 data remained
untouched and v2 reads stayed hidden/isolated.

After reset, v1 could never become formal truth again. Normal rollback meant
pausing/hiding v2 as needed and rolling forward to a corrected v2 build. The
verified v1 export was evidence, not an import source for a serving database.
A database restore was disaster recovery requiring separate authority because
it would also restore old credentials and unrelated application state.

The frozen `cutover_at` could not move during repair. Moving it would omit or
double-count claims accepted under the original epoch boundary.

## Cleanup Admission: Evidence, Not a Clock

The first post-cutover adoption baseline was invalidated when production showed
that personal Activity used cumulative Usage rather than the selected-window
trend. [PR #299](https://github.com/LichKing-2234/ai-efficiency/pull/299)
fixed the denominator and platform
[`v0.1.0-preview.87`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.87)
established the replacement baseline.

Cleanup did not wait a fixed number of days. It required, in one execution
window:

- at least one independently classified ordinary developer workflow after the
  corrected baseline producing a new formal direct/shared pool;
- fresh complete SCM coverage, exact selected-window Usage ratio, active
  readiness, healthy live/ready dependencies, and zero v1 rows;
- no coverage gap, duplicate accounting, terminal/near-expiry claim, lifecycle
  error, or conservation mismatch;
- pending claims still before their final-attempt boundary;
- separate authority for implementation, each release, deployment, and
  destructive schema migration.

Ordinary AI Efficiency [PR #319](https://github.com/LichKing-2234/ai-efficiency/pull/319)
met the adoption requirement with five direct pools and 23,210,615 conserved
Token. A repeatable-read snapshot then conserved 37 formal pools, 37 direct
relations, 946 Requests, and 142,330,988 Token with every named failure/gap
counter zero. Volatile health/coverage/claim gates were repeated immediately
before each later mutation.

## Phase 2: Remove Application Surfaces

[PR #330](https://github.com/LichKing-2234/ai-efficiency/pull/330) removed v1
bucket/revision/report paths, legacy Activity reads/UI, AE OTLP ingest and
authentication, active `aeo_*` issuance, and v1 CLI delivery. It retained the
legacy tables/columns for a non-destructive application rollback, plus the
strict update-time scrubber needed by users who skip CLI releases.

CLI
[`ae-cli/v0.2.0-preview.14`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/ae-cli/v0.2.0-preview.14)
and platform
[`v0.1.0-preview.89`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.89)
were published as separate release units. Every backend/prewarmer role was
proven on the new platform image before rollback compatibility was narrowed.

The same-window gate conserved 46 formal pools, 46 direct relations across 11
commits, 1,272 formal Request/response observations, and 189,117,509 Token. It
reported zero coverage, duplicate, terminal, near-expiry, lifecycle, or v1-row
errors; pending claims remained before their final-attempt boundary. Removed
v1/OTLP/legacy Activity routes returned 404, while readiness, exact ratio, and
claim/SCM coverage stayed healthy.

## Phase 3: Contract the Schema

[PR #332](https://github.com/LichKing-2234/ai-efficiency/pull/332) removed the
two legacy Ent schemas and the installation OTLP hash/enable columns. Before
DDL, both Phase 2 application roles were drained and proven absent. The private
rollback artifact and formal aggregate/relation digest were verified.

The destructive transaction used explicit idempotent statements with no
`CASCADE`, in this dependency order:

1. `attribution_allocation_revisions`;
2. `attribution_usage_buckets`;
3. `reporting_installations.otlp_token_hash` and `otel_enabled`.

An unexpected dependency or conservation mismatch had to abort the transaction.
Formal pools and pool-to-commit relations were never migration targets.

The final pre-DDL gate and in-transaction comparison conserved 48 formal pools,
48 direct relations, 1,313 Requests, and 192,289,908 Token with identical pool
and relation digests. Platform
[`v0.1.0-preview.90`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.90)
then deployed the contracted schema. Post-deploy evidence showed:

- the two legacy tables and two installation columns absent;
- formal pool/relation digests unchanged;
- backend/prewarmer healthy with the same runtime image;
- attribution readiness active, selected-window ratio exact, and claim/SCM
  coverage complete;
- all eleven removed v1, AE OTLP, and legacy Activity routes returning 404.

This schema-only platform phase required no CLI release. Final documentation
closeout merged in [PR #333](https://github.com/LichKing-2234/ai-efficiency/pull/333),
and Issues [#240](https://github.com/LichKing-2234/ai-efficiency/issues/240),
[#250](https://github.com/LichKing-2234/ai-efficiency/issues/250),
[#251](https://github.com/LichKing-2234/ai-efficiency/issues/251), and
[#252](https://github.com/LichKing-2234/ai-efficiency/issues/252) are closed.

## Surviving Compatibility Boundary

The CLI update-time OTel scrubber remains only to remove an exact AE-managed
exporter and old local credential state. A changed protocol, endpoint, headers,
options, or credential is user-managed and must be preserved. The scrubber is
not an active telemetry or accounting path.

The v1 write/report/Activity and AE OTLP routes, `aeo_*` authentication, legacy
tables, and removed installation columns are complete historical work. Old
unchecked plan items do not represent backlog.

Current claim admission, hot retention/finalization, pool conservation,
Activity/readiness, privacy, and failure semantics remain solely in the current
neutral contract.
