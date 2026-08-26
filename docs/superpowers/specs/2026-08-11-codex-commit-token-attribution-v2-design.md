# Codex Commit Token Attribution v2 Design

**Date:** 2026-08-11
**Status:** Active production contract since the verified 2026-08-12 cutover. The Responses WebSocket extension uses Codex-local Token and shipped in `ae-cli/v0.2.0-preview.8` plus platform `v0.1.0-preview.85`; the trusted-log repair shipped in CLI-only preview.9, and preview.10 completed the exact dual-baseline and scan-progress repair for current Codex 0.147. CLI-only `ae-cli/v0.2.0-preview.11` preserves the original single-allocation evidence digest and quarantines unchanged terminal conflicts without retransmission or later-trigger blockage. CLI-only preview.12 added coordinated drain, deleted-worktree recovery, and backlog migration; preview.13 then shipped the exact current-runtime inline-wrapper parser repair and scanner-progress invalidation. The authorized #305 Helm operator canary at commit `c35758f5` produced one `relay_official` group whose 112 official Requests materialized exactly once into four direct pools and `21,668,159` Token; a later managed-hook replay left the group, pools, relations, Request identities, and Token unchanged. This controlled canary does not satisfy #252's ordinary-workflow gate. A 2026-08-14 production read found that personal Activity used cumulative Usage stats instead of the selected-window trend total, invalidating the previously recorded #252 Day 0. PR #299 repaired the denominator and non-zero percentage display; the repair first shipped in platform `v0.1.0-preview.87`, exact production readback established the replacement #252 Day 0 at `2026-08-17T05:32:57.925948Z`, and platform preview.88 then ran at Helm revision 88. AI Efficiency PR #319 later satisfied the ordinary-workflow gate with five direct pools and `23,210,615` Token. The 2026-08-20 evidence snapshot passed every non-destructive execution-time gate and the operator authorized implementation, separate releases, deployment, and destructive migration. Phase 2 merged as `319735ac`, shipped in CLI `ae-cli/v0.2.0-preview.14` and platform `v0.1.0-preview.89`, and completed at Helm revision 89. Phase 3 source contraction merged as `996c23ea`, shipped in platform `v0.1.0-preview.90`, and now runs as Helm revision 90/chart `0.1.76`. The legacy tables and installation columns are absent; exact formal conservation and every health, Activity, SCM, and removed-route readback passed. No fixed elapsed-time wait applied.
**Current implementation note:** Preview.15 shipped the task/hook recovery hardening from PR #392. Preview.16 shipped issue #394's Codex 0.149.1 exact unique-turn fallback, retained-evidence lower bound, terminal expiry classification, and scanner-progress v5 rebuild. Production v5 status then classified the historical local backlog but exposed issue #396: the compact reporter checkpoint still executed legacy tool-usage binding and timed out before v2. Preview.17 attempted to reverse delivery order, but production verification rejected that contract because backend v2 ingest requires the checkpoint to exist before accepting an allocation; its GitHub Release was withdrawn and the operator machine returned to checksum-verified preview.16. PR #399 restored checkpoint-before-v2 and gave the compact checkpoint endpoint a no-legacy-binding service path. Platform `v0.1.0-preview.99` deployed that repair as Helm revision 102/chart `0.1.86`: backend and prewarmer were Ready with zero restarts on one multi-arch image digest, live/ready reported commit `44400277`, and the local checkpoint ledger moved from 31 pending / 6 uploaded to 0 pending / 37 uploaded. The resumed v5 scan completed ten root-workspace triggers without a new accepted claim; thirteen older triggers remained fail-closed because their commits were unavailable after squash-merge branch cleanup. A new reachable post-deploy documentation commit then completed its checkpoint task, but `accepted` remained 7 and `latest_accepted_at` remained `2026-08-24T09:45:48.770101Z`: the active client emitted the still-unsupported `codex_app_server_transport::transport::remote_control::websocket` shape, so the CLI correctly produced no trusted Request evidence or synthetic attribution.
**Scope:** `ae-cli`, backend attribution/reconciliation/read models, frontend Activity, repository administration
**Supersedes for active behavior:** [Codex Token Attribution Ledger POC](./2026-08-05-codex-token-attribution-ledger-poc-design.md)
**Related:**

- [Architecture](../../architecture.md)
- [End-to-End Page Loading Performance](./2026-07-14-end-to-end-page-loading-performance-design.md)
- [Stateless Team Usage Prewarm Worker](./2026-07-25-stateless-team-usage-prewarm-worker-design.md)
- [Sessionless Local Tool Attribution](./2026-05-13-sessionless-local-tool-attribution-design.md)
- [Post-Commit Async Attribution Sync](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)

## 1. Contract Status And Migration Boundary

This specification is the active production contract for Codex commit Token
attribution. The verified 2026-08-12 cutover selected `formal_v2`, rejected v1
writes, reset the exported v1 POC dataset, and enabled formal Activity and
readiness. The 2026-08-05 document remains historical POC context; its v1 and
`shadow_v2` data never becomes formal truth.

This contract does not authorize a later release, deployment, data migration,
legacy cleanup, or `sub2api` change. Those actions retain their own execution
authority and verification gates.

## 2. Product Boundary

AI Efficiency answers two different questions on two different surfaces:

- `/usage`: all visible AI consumption, quota, subscription, cost, general
  model distribution, and general Token composition;
- `/activity`: Codex Token that can be associated with committed code, where
  that Token went by Repository and PR, and how the period changed;
- `/repos` and `/repos/:id`: administrator-only repository integration
  operations, including inventory, SCM credential binding, webhook health and
  repair, and explicit synchronization operations.

Activity does not claim that high Token use is good or bad productivity. It
does not add individual contribution rankings, composite efficiency scores,
chargeback, or cost accounting.

## 3. Accounting Truth

The Token authority is explicit and mutually exclusive per claim group:

- `relay_official`: Responses HTTP uses exact Request IDs and the current
  `sub2api` admin usage result;
- `codex_local`: Responses WebSocket uses measured Codex JSONL
  `token_count.info.last_token_usage` because no stable Request identity is
  available through the supported Relay API.

The formal contribution total is:

```text
total_tokens
= input_tokens
+ output_tokens
+ cache_creation_tokens
+ cache_read_tokens
```

For `codex_local`, Codex `input_tokens` already includes cached input. The CLI
normalizes it to `input_tokens = raw input - cached input - cache write` before
upload, so the four stored components still sum exactly to `total_tokens`.
Reasoning output remains part of `output_tokens` and is not added again.

The ledger preserves all four components, requested model, and a 15-minute UTC
usage bucket. It does not persist API key, account, upstream routing model,
prompt, response, code, patch, command, local path, or a WebSocket response ID
in the long-lived pool.

`codex_local` is authenticated to the reporting installation and frozen Relay
provider, but without an upstream Request identity AE cannot independently
revalidate Relay user/API-key ownership. It is therefore suitable for the
committed-code Activity metric, not billing, chargeback, or security audit.

AI Efficiency does not attempt to equal all AI usage. An HTTP Request or local
WebSocket response without deterministic committed-code evidence remains local
and never contributes formal AE Token.

The accounting invariant is:

> Every accepted HTTP Request or WebSocket local usage aggregate associated
> with committed code contributes exactly once globally.

## 4. Local Claim Construction

The local v2 claim group binds:

```text
relay_provider_id
token_source: relay_official | codex_local
request_ids[]                 # relay_official only
local_usage[]                 # codex_local only
thread_id / turn_id
structured mutation evidence
repo / worktree evidence
commit allocation sequence
local calibration envelope    # relay_official only
```

Rules:

- `relay_provider_id` is the backend provider selected by discover and is
  frozen when the group is created;
- provider and Token source switching never rewrite an older pending group;
- `relay_official` binds the successful Responses HTTP completion's
  `x-client-request-id`, normalized to one `client:` prefix, directly to the
  same trusted SQLite `thread.id + turn.id` as the primary identity. Codex
  0.149.1 may preserve the same turn UUID while the JSONL and trusted SQLite
  thread identities differ. In that shape only, the CLI may fall back to the
  exact turn UUID when it maps to exactly one JSONL local claim identity and
  exactly one trusted SQLite thread identity across the complete active and
  archived source set; all successful Request IDs for that one SQLite turn are
  retained. Duplicate or conflicting turn identities are
  `ambiguous_request_evidence` and remain local. Transport connection IDs,
  `x-request-id`, Kong IDs, SSE `response.id`, timing proximity, cwd, paths,
  model, and unmatched evidence are rejected;
- `codex_local` requires trusted SQLite WebSocket transport and successful
  sampling evidence for the exact local `thread.id + turn.id`. For Codex
  0.147, the transport side is a non-warmup `response.in_progress` event emitted under
  `codex_api::sse::responses` and the
  `model_client.stream_responses_websocket` span, while the success side is
  the `codex_core::session::turn` `post sampling token usage` event emitted
  only after the sampling request returns successfully. A `generate=false`
  WebSocket warmup is never completion evidence. The two rows are
  joined only by their exact thread and turn identities; target, process, or
  timestamp proximity is insufficient. An older raw WebSocket
  `response.completed` row remains accepted when it contains the same exact
  identities;
- any raw `resp_*` value is only local transport evidence. It is neither a
  Relay Request identity nor uploaded or persisted by AE;
- WebSocket Token comes from JSONL `last_token_usage`, which is an incremental
  response total. A matching cumulative `total_token_usage` snapshot is
  required to suppress repeated terminal rows. At the first Token row of a new
  turn, the cumulative counter may either continue the prior session baseline
  or restart from zero; the row must match one of those exact deltas, after
  which the selected cumulative sequence remains strict for that turn. One
  explicit top-level `compacted` row for that same turn permits the next
  `token_count` row to repeat the exact prior cumulative components with a
  different valid `last_token_usage` snapshot. That row is a compaction
  baseline restatement: it contributes zero Token and zero `request_count`,
  then becomes the repeated-terminal comparison snapshot for later rows. The
  same unchanged-cumulative/different-last pattern without that exact boundary
  remains invalid. Missing model, usage timestamp, cumulative snapshot,
  invalid cache decomposition, inconsistent totals, or overflow fails the
  local source closed;
- WebSocket Token is normalized and aggregated locally by requested model and
  15-minute UTC usage bucket. The aggregate `request_count` counts accepted
  incremental response rows; it does not preserve their identities;
- a turn containing both trusted HTTP Request IDs and trusted WebSocket
  completion evidence is `mixed_token_sources` and is not uploaded;
- one turn may contain multiple HTTP Requests or WebSocket responses and all
  share the turn's mutation set;
- HTTP local Token exists once at group level for calibration only. WebSocket
  local usage is the formal source and never carries calibration or Request IDs;
- the stable group ID must not depend on a file path, line number, or the
  current count of late-arriving Requests.
- the earliest retained successful trusted HTTP Request row is the local
  request-evidence lower bound. A previously discovered HTTP candidate before
  that bound is `request_evidence_expired`, remains fail-closed, and is not
  retried as `missing_request_id`; no Request ID or Token is synthesized.

The local privacy boundary excludes prompt, response, reasoning text, code,
diff, patch content, command arguments/output, API key, API key ID, raw JSONL,
raw SQLite rows, raw spans, and local paths from upload.

## 5. Deterministic Commit Evidence

Formal commit association accepts only:

- structured Codex mutation evidence;
- deterministic comparison with Git index/tree/commit content;
- explicit Git rewrite or lineage evidence.

Structured patch evidence may arrive as a direct `apply_patch` payload or as
one of the exact generated `exec` wrappers recognized by the scanner. The
inline wrapper must contain one JSON string patch literal, bind the tool result
to one JavaScript identifier, and pass that same identifier to
`text(JSON.stringify(...))`. Comments, template literals, multiple calls,
malformed calls, and mismatched result identifiers remain unrecognized and
fail closed. A wrapper match never replaces the deterministic Git-content
proof below.

CLI-only `ae-cli/v0.2.0-preview.13` released this exact wrapper contract from
commit `f54184a6`. Its production Helm canary proved the wrapper through the
normal managed-hook path and exact Activity readback; replay remained
idempotent, and no platform release or Helm rollout was part of the CLI fix.

For one commit allocation, the claim-group evidence digest is exactly that
allocation's evidence digest. The ordered composite digest is introduced only
after a second allocation is appended. An exact rescan of a single allocation
must not change an already acknowledged group envelope.

It rejects cwd, timing, path proximity, text similarity, ordinary command
workdir, and "the next commit probably owns it" heuristics.

The system proves only that mutation evidence is present in a commit. It does
not claim to distinguish later human editing, formatters, conflict resolution,
or another agent's rewrite. Evidence that cannot be proven is `unverified` and
does not enter formal commit Token.

## 6. Event-Driven Delivery

The runner/recovery hardening below is the current source contract. It does not
claim release or production rollout until a later CLI release is independently
published and verified.

v2 remains Git-event-driven and adds no daemon or periodic synchronization:

- `post-commit` closes or revises deterministically matched groups;
- `post-rewrite` records explicit old-to-new mappings;
- `pre-push` wakes the outbox and returns fail-open immediately;
- `ae-cli sync` remains a manual diagnosis and recovery entry.

Failed delivery remains in a durable local outbox. Hooks never block commit or
push. A runner that receives new work while draining must consume it or
reliably start a successor; it may not require another Git event.

Checkpoint persistence remains before v2 scan and acknowledgement because the
backend validates every allocation against the exact owner, Repository,
workspace, commit, and checkpoint event. The compact reporter endpoint records
that minimized authorization checkpoint without querying or binding legacy
`tool_usage_events`; those events are not an input to the formal v2 claim. The
older user-authenticated checkpoint endpoint retains its historical usage-window
binding for non-v2 compatibility. This keeps authorization ordering strict
without allowing legacy usage binding to consume the compact workspace quantum.

All reporting work on one machine is serialized by one transient drain owner.
Per-workspace tasks remain the durable queue and evidence boundary, but a
detached owner drains every runnable task for the active server and reporting
owner rather than competing for the machine-scoped claim ledger. Contenders
coalesce through one idempotent wake keyed by normalized server and reporting
owner. Exactly one contender per scope remains
as a bounded waiter with that scope's in-memory uploader; other same-scope
contenders exit immediately. The waiter acquires the global ledger lock after
the owner releases it, so an account/config switch cannot redirect the wake. A
bounded five-minute
workspace quantum that exhausts its context becomes `yielded`; the detached
owner itself exits after ten minutes and starts one successor when retained
work remains. The process exits when its matching queue is empty; this is
neither a daemon nor a periodic synchronizer. A dead owner lock is recoverable
from the persisted tasks and process liveness evidence.

Every newly captured v2 commit trigger freezes the selected Relay provider ID
alongside its original Repository, workspace, checkpoint event, commit, and
capture time. If the original worktree directory or Git worktree metadata no
longer exists, another checkout can lend its Git root to that retained task
only when the normalized server and reporting owner already match, the exact
Repository ID and canonical remote identity match, and every full commit object
is evaluated independently for reachability from that checkout's `HEAD` or a
Git ref. Recovery changes only the runtime Git root: workspace, checkpoint,
commit, provider, and evidence identity remain the original values. Runnable
triggers revalidate their frozen server, reporting owner, Repository ID/key,
workspace, provider, and checkpoint before they are scanned and acknowledged
exactly once, while a missing provider,
missing or unreachable commit, or mismatched checkpoint remains in the same
durable task with a safe local-state failure. One retained trigger never blocks
a runnable sibling or a later unrelated trigger. Cross-Repository checkout and
different-owner recovery still reject the complete task before scanning. No
cwd, timestamp, branch name, patch similarity, or code-content heuristic can
rebind a trigger. `pre-push` and `ae-cli sync` wake this path through the same
transient owner; neither adds a daemon or periodic scan.

On first runner, status, or doctor activity after upgrade, the same reporting
installation scans the existing workspace tasks for its normalized server and
reporting owner. Legacy single-trigger fields are promoted into the retained
trigger list, exact duplicates are collapsed, and a missing historical provider
is frozen to that installation's persisted Relay provider. Already-frozen
providers and every Repository, workspace, checkpoint, commit, and capture time
remain unchanged. Stale scanner versions discard only local digest progress so
the current scanner rebuilds them; current progress deduplicates exact source,
unit, and turn keys. A busy or conflicting workspace is deferred without
blocking unrelated migration, and future task/progress versions are left alone.
Hook-time task upsert applies this schema promotion before canonical trigger
comparison, so the first exact replay after upgrade cannot conflict merely
because the retained trigger predates frozen identity fields.
The same first-activity boundary quarantines only the exact synthetic Repository
identity emitted by affected historical Git fixtures under the current
normalized server and reporting owner. Its complete workspace directory and
matching unresolved JSONL events move into an audit quarantine;
all non-matching bytes and every legitimate missing-worktree task remain active.
This migration is idempotent, local-only, and performs no backend request or
server-side deletion. Missing paths, temporary-looking names, age, and failure
reason are never sufficient quarantine evidence. It holds the same machine
ownership as the runner, journals the active binding before any move, performs
audit-first atomic file/directory replacements, and resumes an interrupted
journal on the next first-activity pass.

One runner pass performs one 90-day-bounded Codex transport-evidence query and
one discovery of active `sessions` plus `archived_sessions`. For HTTP Request
correlation, the effective source-discovery cutoff is the later of the 90-day
window and the earliest retained successful trusted HTTP evidence row. Existing
`missing_request_id` candidates before that lower bound become
`request_evidence_expired`; sources wholly before it are not reopened when no
deterministic proof can remain. File modification time and the indexed SQLite
timestamp predicate apply the window before JSONL or log contents are read.
Every discovered source is streamed once for all
pending commit triggers in that pass. Durable, digest-only `source × trigger`
completion units are saved after the corresponding claim candidates are saved,
so timeout, process exit, or backend failure resumes the exact remaining units.
A completed source batch with no candidates may persist several sources in one
atomic progress update to avoid quadratic state-file rewrites. A batch that
does produce a claim persists the claim and its completion units immediately,
then attempts backend delivery before scanning unrelated sources. Response
loss therefore replays the already-persisted claim without rescanning its
completed units, while deterministic server ACKs still prevent duplicate pools,
relations, or Token.
A later trigger adds only its own units; it does not invalidate completed work
for older triggers. For each source, progress also retains digest-only turn keys
and the digest of trusted SQLite HTTP Request or WebSocket transport/success evidence
relevant to those turns. Late transport evidence invalidates completed units
only for the affected source; unrelated events do not restart completed
sources or older triggers. Raw Request, response, thread, and turn identifiers
are not persisted in scan progress. Any scanner-semantics change that can alter
claim classification increments the scan-progress version. An older version is
rebuilt before completed units are consulted, so a previously failed-closed
turn can recover without new transport evidence or a new Git trigger.
Scanner-progress v5 performs this rebuild once for the Codex 0.149.1
correlation contract. It preserves exact `thread + turn` matches as primary,
then finalizes unique-turn fallback against the complete compact candidate
state after every source has been streamed. It does not perform a second JSONL
read or persist a raw turn index. Successful delivery removes the transient
progress file.

When reporter-authenticated Repository resolution returns `not_found`, the
same hook may narrowly ensure the minimum Repository identity from the
canonical Git remote and continue the exact triggering event. This path is
idempotent through the Repository identity constraint. It does not create or
change SCM credentials, provider binding, webhook configuration, or unrelated
Repository settings. An unbound Repository remains eligible for commit Token
reporting. Whether Codex evidence exists controls claim creation only: a manual
commit can register the Repository but creates no claim, pool, or Token.

The client deletes only data covered by explicit server ACKs:

- an HTTP Request ID only after `persisted` or `duplicate_identical`;
- the HTTP calibration envelope only after its independent ACK;
- a WebSocket group envelope only after the acknowledged source, allocation,
  and complete `local_usage[]` aggregate still match local state;
- conflicts, unknown responses, and unacknowledged items remain local.

A `conflict`, `rejected`, or `rolled_back` acknowledgement is terminal for the
unchanged local claim snapshot. It remains in the 90-day local state and in
status/doctor diagnostics, but is excluded from automatic retransmission and
does not keep its completed trigger or later unrelated triggers pending. A
runner pass that leaves only terminal conflicts completes normally. Pending
uploadable claims, `upgrade_required`, and malformed, unknown, missing, or
protocol-mismatched acknowledgements continue to fail closed.

If later JSONL rows monotonically increase an acknowledged WebSocket aggregate,
the group is reopened and redelivered. The client never sends a decreasing or
source-switched replacement.

The local unresolved and audit-minimal state is retained for at most 90 days
and cleaned lazily on later hook, sync, or CLI activity.

Local status and doctor output distinguish the current Repository task from
machine-wide `queued`, `running`, `yielded`, and `recoverable` task totals, plus
terminal-conflict and seven-day-expiry-warning counts. The v2 delivery summary
also reports aggregate accepted, `missing_request_id`,
`ambiguous_request_evidence`, and `request_evidence_expired` counts without raw
identifiers. Failure diagnostics
contain only the stage, a fixed safe reason, the first failure time, and the
remaining trigger count; raw Request or response identifiers are never printed.
They also expose only aggregate synthetic-fixture quarantine workspace/event
counts and its first migration time, never the quarantined paths or payloads.
Migration and lazy 90-day cleanup mutate only local recovery detail. They never
delete acknowledged formal pools, relations, or any server data.

The backend owns one cutover protocol contract and returns it from installation
enrollment and every v2 batch acknowledgement:

```text
ledger_epoch
v1_write_policy: accept | upgrade_required
minimum_cli_version
```

Before cutover the exact default is `shadow_v2 + accept` with no minimum
version. The transition may advertise `upgrade_required` for the supported
shadow or formal v2 epoch only when a non-empty minimum CLI version is also
present. Unknown or contradictory combinations fail startup and fail closed on
the client; they are never inferred from deployment capabilities. The CLI
persists the enrolled contract in its existing reporting configuration. Formal
mode does not create or require a new v1 baseline and does not use a rejected
v1 request as a normal feature probe. A transition-era v1 `409
upgrade_required` is not an ACK, does not advance local v1 state, and cannot
prevent the same runner pass from delivering eligible v2 claims.

Phase 2 no longer mounts either v1 write endpoint. The three compatibility
fields remain in enrollment and v2 acknowledgements so installed clients keep
one validated protocol contract; `v1_write_policy=upgrade_required` no longer
implies that a v1 route exists. Requests to the removed routes return 404.

The same backend-owned cutover configuration has one server-only `cutover_at`
UTC instant. It is empty before cutover, required as an explicit RFC3339 `Z`
timestamp for `formal_v2`, and immutable across rollback or repair. A shadow
epoch with `cutover_at`, or a formal epoch without it, fails startup. This
boundary is not a second client protocol field: clients continue to consume the
three-field contract above, while Activity uses `cutover_at` to decide whether
the complete adjacent period is comparable even when that period contains zero
formal pools.

## 7. Hot Claim And Reconciliation Contract

The backend keeps hot claim groups, HTTP Request claims, and WebSocket local
aggregates for at most 90 days.

The Request identity constraint is:

```text
UNIQUE(relay_provider_id, request_id)
```

An identical owner/group/evidence replay returns `duplicate_identical`.
Different canonical content for the same identity is a conflict and cannot be
ACKed as persisted.

Reconciliation occurs behind a narrow `relay.Provider` Request usage reader:

```text
0 rows
  -> pending

1 row and usage user matches the installation owner's relay_user_id
  -> reconciled

more than 1 row
  -> ambiguous invariant violation
```

Provider missing/disabled, owner mismatch, ambiguous results, negative or
overflowed Token, and inconsistent totals fail closed. The backend never
falls back to a primary provider or compares the upstream usage user with the
local AE database user ID.

A database lease, bounded concurrency, retry backoff with jitter, and lease
expiry prevent backend replicas from multiplying the same upstream lookup.

`codex_local` is a separate fail-closed ingest contract:

- it requires non-empty `local_usage[]` and forbids Request IDs and calibration;
- each model/bucket aggregate must have non-negative components, a positive
  total and count, exact component conservation, and a 15-minute UTC boundary;
- the ingest transaction materializes it directly into the same usage-pool
  model without creating an `attribution_request_claim` row or calling Relay;
- identical replay is a no-op; late aggregate growth and appended deterministic
  allocations atomically replace only that group's prior contribution;
- every existing component, count, and bucket must remain present and
  monotonic. Regression, source switching, duplicates, or overflow fails closed.

Partial groups expose only the reconciled lower bound. Before source expiry,
the reconciler performs a final attempt. The finalization deadline is at least
24 hours earlier than the nominal upstream retention boundary so scheduled
cleanup cannot race the last lookup. At HTTP finalization:

1. freeze allocation;
2. ensure every reconciled Request is materialized;
3. materialize unresolved coverage gaps with zero official Token;
4. remove Request ID, local calibration, and hot claim detail in the same
   retryable transaction boundary.

WebSocket finalization has no unresolved Request lookup or coverage gap. It
only freezes the already materialized aggregate and removes `local_usage` plus
the other hot proof details. In both paths, long-lived pool facts remain.

Long-lived product data never contains Request ID. Operational metrics cover
pending age, reconciliation latency, mismatch/ambiguity, expiry, finalization,
and cleanup failure without exposing Request identifiers in the UI.

## 8. Long-Lived Usage Pool

Formal Token is stored once in a unified long-lived pool:

```text
attribution_usage_pools
- canonical_pool_key
- relay_provider_id
- user_id
- requested_model
- bucket_start_utc
- input_tokens
- output_tokens
- cache_creation_tokens
- cache_read_tokens
- total_tokens
- request_count
- coverage_gap_count

attribution_usage_pool_commits
- pool_id
- repo_config_id
- commit_sha
- relation_kind: direct | shared | inherited_non_counting
```

The canonical pool identity is partitioned by ledger epoch and, within that
epoch, covers the Relay provider, user, sorted counting commit set, requested
model, and a non-empty 15-minute UTC bucket based on the authoritative source
usage time: Relay usage time for HTTP or JSONL event time for WebSocket.
Otherwise identical shadow and formal contributions cannot collide or merge.
Fifteen-minute buckets support local natural-day aggregation for IANA zones
with whole-hour, half-hour, and quarter-hour offsets without preallocating empty
rows.

- one counting commit makes the pool direct;
- more than one counting commit makes the pool shared;
- the Token remains stored once in both cases;
- adding a second commit atomically migrates/merges the contribution instead
  of duplicating it;
- unresolved finalization adds only a zero-Token gap;
- because an unresolved Request has no authoritative upstream model or usage
  time, its gap is stored in a deterministic coverage-only pool using the
  reserved model `unresolved` and the claim group's first server-received
  15-minute UTC bucket; this pool never contributes Token or Request count;
- Request claims may be deleted without affecting Activity reads;
- WebSocket hot aggregates may be deleted without affecting Activity reads;
- `request_count` means reconciled Relay Requests for `relay_official` and
  accepted incremental response rows for `codex_local`; product UI does not
  expose this operational count.

## 9. Rewrite, Reachability, And PR Projection

An explicit Git old-to-new rewrite can migrate the commit relation during or
after the hot claim window. Mapping chains and repeated events are idempotent;
conflicting mappings and cycles are rejected.

Without explicit mapping, reset, branch deletion, remote force-push, similar
patches, or nearby time never trigger migration. A previously counted commit
may be marked orphaned/unreachable, but its Token is not silently deleted.

Cherry-pick is `inherited_non_counting` only with explicit lineage evidence and
a stable patch match. It remains visible without increasing accounting totals.

Commit-to-PR is many-to-many. There is no inferred primary PR. A pool involved
in several PRs contributes its full `involved_tokens` to each relevant PR.
PR rows therefore cannot be summed and never display a percentage of the
global total.

## 10. Activity Read Model

The backend is the only accounting aggregator. The frontend must not sum
commit, member, Repository, or PR rows into scope totals.

Protected v2 reads support personal, authorized member, authorized team,
Repository/PR rankings and pages, daily trend, readiness, and denominator
status. Every read revalidates actor role and current directory scope before
using a reconstructible cache.

Repository semantics:

- branch/worktree activity for the same SCM Repository is combined;
- ranking uses `direct_tokens` only;
- the row may show direct share of the formal scope total;
- participating shared Token is a separate non-additive field.

PR semantics:

- ranking uses `involved_tokens`;
- shared/overlap is explicit;
- PR totals and percentages are absent;
- Repository and PR pages default to Token descending with stable identity
  tie-breaking;
- full lists use server-side search/sort and cursor pagination with 20 rows per
  page.

Daily trend semantics:

- scope trend counts every formal pool once globally;
- Repository-filtered trend uses direct Token as the primary series and shows
  shared participation only as a secondary non-additive series/tooltip;
- PR-filtered trend uses involved Token and labels it non-additive across PRs;
- local natural days are computed by the backend using the browser IANA zone;
- Token belongs to its authoritative source usage time, not commit time.

## 11. Code Token Ratio And Usage Reuse

The Activity headline ratio is:

```text
formal committed Codex Token
-----------------------------------------------
all sub2api Token for the same scope/provider-set/window
```

The current upstream Usage surface cannot isolate every Codex client request,
so the denominator is all `sub2api` Token for the exact scope/provider
set/window. Every provider and every authorized subject contributing to that
scope must be covered; one missing or stale provider/subject makes the whole
denominator unavailable rather than partial. The UI must not describe the
remainder as non-development work.

Activity reuses Usage business read services, not Redis keys or copied HTTP
fan-out:

- personal dispatches to the personal usage metrics-only service;
- team dispatches to the existing team summary service;
- member uses a new authorization-isolated subject metrics reader/cache;
- a single denominator resolver returns total, exact range/timezone coverage,
  provider-set coverage, `as_of`, freshness, and `complete`.

For personal and member reads, the selected-window denominator is the
overflow-checked sum of the Usage trend points returned for that exact range.
The cumulative `/usage/dashboard/stats` total is never a range denominator.
Negative or overflowing trend totals make the ratio unavailable. This matches
the Personal Usage Center's selected-range Token cards.

The ratio is calculated only when the denominator is fresh, complete, and
matches the exact requested coverage. The committed numerator is cut off at
the same `as_of`.

Provider switching never removes already formal committed Token from Activity
scope totals, trends, Repository rows, or PR rows. If the requested window
contains formal pools outside the exact provider set covered by Usage, the
ratio is unavailable; the backend does not hide those historical pools to make
the numerator appear comparable.

- complete denominator plus incomplete committed attribution may display
  `at least X%`;
- incomplete/missing denominator displays no percentage;
- complete zero denominator displays `No AI Token use in this period`;
- a true complete zero numerator with non-zero denominator displays `0%`;
- a non-zero percentage uses enough precision to remain non-zero, with values
  below `0.01%` displayed as `<0.01%`;
- stale/error Usage never produces an estimate.

A transient Usage resolver error is represented as a retryable local ratio
error with `denominator_unavailable`; it does not suppress committed totals,
trend, readiness, Repository, or PR data from the same Activity context.

The donut labels are `Used for committed code` and `Other Token`. `Other Token`
does not mean non-development work. When the adjacent equal period is fully
comparable, the headline displays percentage-point change (for example,
`+8 percentage points`), not relative growth. The comparison is omitted when
the prior period crosses the frozen `cutover_at` or either period is
incomplete. The boundary is never inferred from the first formal pool; a
complete zero-data prior period wholly after `cutover_at` remains comparable.

## 12. Activity Frontend Contract

### 12.1 Routes And Information Architecture

The existing scope routes remain authoritative:

- `/activity`: personal;
- `/activity/members/:user_id`: authorized member;
- `/activity/teams` and `/activity/teams/:team_id`: authorized organization and
  team views.

No separate Repository or PR analysis route is added. `repo_id` and
`pr_record_id` are shareable query filters on the current Activity scope route.
`from`, `to`, and `timezone` are preserved through refresh, share, browser
history, team/member navigation, and filter clearing.

Activity clicks do not navigate to `/repos/:id`. `/repos` and `/repos/:id`
become administrator-only integration operations and remove Activity, PR usage,
and Token analysis. CLI repository discovery/resolve/hook APIs keep their
appropriate authenticated/reporter access and are not accidentally placed
behind the administrator UI guard.

### 12.2 Overview Layout

Desktop order:

```text
Header + shared range control

Code Token ratio donut | committed Token daily trend
Repository Top 5       | PR Top 5

Repository / PR full-list tabs
```

Top 5 uses horizontal bars. Repository uses direct Token, direct share, period
change, and secondary shared participation. PR uses involved Token, period
change, and overlap/shared status without a percentage.

Charts use the existing asynchronous Chart.js seam and never block text,
empty, or error states.

### 12.3 Range And Comparison

- default: latest 30 local natural days including today;
- presets: 7, 30, and 90 local natural days;
- custom: inclusive date range, at most 90 local natural days;
- today is marked in progress;
- comparison uses the adjacent equal count of local calendar days;
- ratio percentage-point comparison appears only when both adjacent periods
  are complete and entirely inside the comparable formal epoch;
- `from`, `to`, and browser IANA timezone live in the URL;
- 90 days is a product/query bound, not an upstream retention guarantee.

### 12.4 Filtering

The scope ratio and Top 5 remain overall context. A selected Repository/PR is
highlighted. Repository selection changes the daily trend and filters the PR
page. PR selection changes the trend and expands related commits. A visible
filter chip clears the selection while preserving scope/range.

The backend accepts a Repository or PR filter for SCM coverage only when its
Repository is already present in the actor's authorization-revalidated formal
Activity projection. An arbitrary numeric filter behaves like no matching
Activity data and does not reveal global Repository/PR existence or integration
health.

There is no independent commit ranking tab and no low-value Repository detail
dashboard for model or Token composition. General model/composition analysis
remains `/usage`.

### 12.5 Team And Member Navigation

Activity reuses the Usage organization navigation recipe: organization tree,
department detail, member list, member detail, cursor behavior, identity
fields, and data-availability affordance.

Activity member rows sort by name and show identity, department, Activity data
availability, and the authorized detail action. They do not show or rank
personal Token, code ratio, cost, PR count, commit count, Repository count,
subscription, quota, multiplier, or Relay-specific state.

The organization and member collections come from the Team Usage organization
contract, which returns immediate child departments and direct members with
independent cursors. Activity requests
`GET /api/v1/activity/v2/teams/:team_id/member-availability` only for positive
user IDs on the current direct-member page. The availability result includes a
user only when the selected date range contains a `formal_v2` direct/shared
commit relation in that authorized team scope. Usage Token, local user identity,
member selectability, and shadow/v1 data never imply availability.

### 12.6 Loading, Empty, Error, And Responsive States

Ratio, trend, Repository, and PR sections load independently. A section error
is local and retryable. Same-query refresh keeps prior successful content with
a refreshing state. Range/scope/filter/search/sort changes clear affected old
content so an older context is never presented as the new selection.

Empty states distinguish:

1. no complete Usage in the period;
2. ratio unavailable because reporting/denominator coverage is incomplete;
3. a true complete `0%` committed ratio;
4. a Repository/PR filter with no matching rows.

Desktop full lists use tables. Mobile mounts a card list instead of a hidden
second row tree or a horizontally scrolling table. Cards preserve names,
Token, period change, overlap/shared status, Repository identity where needed,
search, sort, and pagination.

## 13. Readiness And Product Privacy

User-level readiness remains aggregate and has no device list or time-based
stale state. `active` requires the first accepted committed direct/shared v2
group in the formal epoch. An uncommitted, unbound, shadow, or old v1 bucket
cannot activate readiness. Once active, later orphan marking or Relay-provider
switching cannot return that user to `waiting_for_data`.

The authenticated current-user contract advertises two deployment-owned flags:
`setup_available` and `readiness_available`. Both default false, readiness may
not be enabled without setup, and readiness may be enabled only when the single
backend protocol contract selects the formal v2 epoch. The frontend never
infers these flags from an epoch, release, or failed endpoint.

`GET /api/v1/attribution/status` is mounted only when readiness is available,
uses `Cache-Control: no-store`, and returns one aggregate user state:
`not_enrolled`, `revoked`, `disabled`, `waiting_for_data`, or `active`. It never
returns installation counts, identifiers, labels, last-seen timestamps, or a
device list. Installation rows determine the first four states. An enabled
installation becomes `active` only through the same PostgreSQL formal-pool
predicate used by Activity, and `latest_accepted_at` is the latest qualifying
pool server `created_at` across full history. A query failure is a local
retryable error and must not be converted into `waiting_for_data` or block
Activity analytics. No Redis readiness cache is introduced.

Normal setup remains install, login, and discover. Explicit enable, hook
management, repo init, sync, and diagnostics are advanced/recovery paths. v2
does not require Codex OTel. Upgrade removes only AE-managed Codex OTel config
and never touches user-managed OTel. After replacing the executable, both
installers invoke a hidden post-install command on the newly installed binary,
so an older running updater cannot skip the cleanup implementation delivered by
the new release. The command supports users who skip intermediate CLI releases.
It removes the local legacy OTLP plaintext only after exact managed-exporter
removal or when no `otlp-http` exporter exists; a user-modified exporter and its
local ownership evidence are preserved, and the completed install emits a
warning without failing.

Per-Repository `ae-cli init` is not a prerequisite. A managed hook first
resolves the canonical remote and, only for reporter-authenticated `not_found`,
uses the minimum Repository ensure path described in Section 6 before
continuing the same event.

Both a successful new or already-valid login and a successful non-dry-run
discover that persisted at least one supported tool configuration run the same
idempotent, best-effort v2 activation path. Activation preserves a valid
installation and selected provider, recovers a missing reporter credential,
enables managed global hooks, disables legacy AE-managed OTel, and never turns
an otherwise successful login or tool configuration into a failure. No-tool,
no-matching-credential, failed, and dry-run discovery do not activate reporting.

Product DTOs and UI exclude Request ID, WebSocket response ID, raw claim rows,
local calibration, hot local-usage aggregates, API key/account,
prompt/response/code, local paths, pending Request counts, and coverage-gap
Request details. Aggregate operational health belongs in backend metrics rather
than normal Activity UI.

## 14. Cutover And Cleanup

The cutover sequence is fixed:

1. reject new v1 writes with `upgrade_required`;
2. freeze the formal `cutover_at` and ledger epoch;
3. exclude or clean pre-cutover v2 shadow/canary data;
4. switch the v2 Activity/readiness epoch;
5. export exact v1 POC IDs/counts/totals and a verification hash;
6. reset the full v1 POC dataset resolved at execution time;
7. verify v1 rejection, v2 acceptance, Activity/readiness, and zero old totals.

No historical data is backfilled. Rollback may pause or hide v2, but known
incorrect v1 totals never become formal again. Data deletion, release, and
deployment each require explicit authority in the execution turn.

The replacement Day 0 readback is retained as a historical and conservation
baseline, not as a waiting clock. Legacy cleanup requires all of these gates to
be true in the same execution window:

- adoption is a pool delta, not a Token-volume threshold: relative to the
  recorded Day 0 baseline, at least one additional `formal_v2` direct/shared
  pool must come from an ordinary developer workflow; staging, synthetic,
  qualification, cutover, and operator canaries do not qualify;
- production live/readiness remains healthy; before Phase 2, v1 writes continue
  returning the structured `upgrade_required` response, while after Phase 2
  the removed v1 and AE OTLP routes remain absent; near-expiry remains zero,
  pending claims remain before the final-attempt boundary, and no terminal
  reconciliation, source-expiry, hard-expiry, finalization, or cleanup error
  counter increases;
- authenticated readiness remains `active`; Activity shows the additional
  formal pool with complete claim coverage and an exact fresh Usage ratio; the
  final SCM coverage read has no failed, partial, unsynced, or stale Repository;
- formal pool and commit-relation counts and totals have not decreased or been
  recounted, and both reset v1 tables remain zero.

Any failed gate blocks cleanup until it is corrected and read back again; it
does not start or restart a fixed elapsed-time clock. The authorized cleanup is
staged so a rolling application deployment never overlaps a binary that needs
the removed schema:

1. Phase 2 removes v1 bucket/revision/report and legacy Activity code, AE OTLP
   ingest/authentication, active `aeo_*` issuance, and frontend Bucket/Request
   UI. It keeps the two v1 tables plus `reporting_installations.otlp_token_hash`
   and `otel_enabled` only for non-destructive rollback. New installation rows
   use an internal retired sentinel for the still-required legacy hash; no
   credential is returned or accepted.
2. After every serving replica is proven on Phase 2, Phase 3 exports the exact
   legacy schema, repeats formal conservation in the DDL transaction, and drops
   only those tables/columns with explicit no-`CASCADE` statements.

Both phases preserve user-managed OTel and every formal pool and commit
relation. Platform and CLI releases remain separate release units.

Phase 2 completed from commit `319735ac`. CLI workflow `32389268066` published
`ae-cli/v0.2.0-preview.14` as five target archives plus checksums without
replacing the platform-owned repository latest release. Platform workflow
`32389679261` published `v0.1.0-preview.89` with linux/amd64 and linux/arm64
GHCR support. Helm revision 89, chart `0.1.75`, deployed the same preview.89
digest to the Ready backend and prewarmer roles with zero restarts. The final
gate and post-deploy readbacks conserved 46 formal pools and 46 direct
relations across 11 commits, `189,117,509` Token, and 1,272 formal
Request/response observations. They found zero coverage, duplication,
lifecycle, expiry, or v1-row errors; all four pending claims remained before
their final-attempt boundary. The removed v1 batch/revision, AE OTLP, and legacy
Activity summary routes returned 404, authenticated readiness remained
`active`, and Activity v2 retained exact Usage ratio plus complete claim and
SCM coverage.

Phase 3 completed from commit `996c23ea`. Hosted CI `32442883145` passed all
four jobs and release workflow `32443741864` published platform
`v0.1.0-preview.90` for linux/amd64 and linux/arm64; no CLI release was needed.
The final gate conserved 48 formal pools, 48 direct relations, 1,313 Requests,
and `192,289,908` Token. All five pending claims remained before their
final-attempt boundary, while terminal, lifecycle, expiry, duplicate,
component, coverage, and v1-row failures stayed zero. Both Phase 2 application
roles were drained and proven absent before explicit idempotent no-`CASCADE`
DDL committed with identical in-transaction pool and relation digests. Helm
revision 90/chart `0.1.76` then deployed preview.90 to Ready backend and
prewarmer roles with zero restarts and one shared runtime image digest.
Post-deploy readback proved the two legacy tables and two installation columns
absent, the formal digests unchanged, live/ready dependencies healthy,
readiness active, Activity ratio exact, claim and SCM coverage complete, and
all removed routes still returning 404.

## 15. Acceptance Matrix

Implementation is not complete until tests and readbacks cover:

- personal, authorized member, team, administrator Repository operations, and
  unauthorized access;
- provider switching, multi-Request turns, duplicate/conflict, response loss,
  partial reconciliation, expiry, shared, late claims, rewrite, orphan, and
  cherry-pick;
- HTTP/WebSocket source exclusivity, repeated WebSocket terminal snapshots,
  same-bucket aggregation, cache normalization, missing/invalid local usage,
  monotonic late growth, allocation migration, finalization, and cleanup;
- Codex 0.149.1 exact-pair precedence, unique turn fallback, JSONL-side and
  SQLite-side turn ambiguity, retained-evidence boundary, mixed-source
  convergence, scanner-progress v5 replay, and installed-runner ACK readback;
- large active/archive homes, multiple commit triggers sharing one source read,
  timeout/backend-failure resume, and automatic missing-Repository registration
  without claim creation for a manual commit;
- single global pool counting across commit/Repository/PR/team projections;
- 7/30/90/custom, URL restoration, UTC, Asia/Shanghai, a DST zone, and a
  quarter-hour zone;
- exact ratio, lower bound, unavailable denominator, no Usage, and true zero;
- server search/sort/page, Repository/PR filters, clearing, and commit expand;
- independent loading/error/retry, stale denominator, superseded response, and
  snapshot/cursor recovery;
- desktop table, mobile card, long labels, keyboard/focus, touch targets, and
  no page-level horizontal overflow;
- Usage/Activity/Repository navigation and content separation;
- backend, CLI, frontend, role E2E, build measurement, query-plan/scale tests,
  multi-replica reconciliation, and one controlled real Request-to-commit-to-
  Activity readback before cutover.

## 16. Explicit Non-Goals

- changing `sub2api`, coupling directly to its database, Relay turn discovery,
  or depending on upstream persistence of WebSocket client metadata;
- a daemon or periodic local synchronizer;
- uploading uncommitted/unverified usage;
- persisting or uploading WebSocket Request/response identities or one row per
  local response;
- splitting one Request's Token by lines, files, commits, repositories, or PRs;
- inferring a primary PR;
- individual ranking, productivity scoring, cost allocation, or chargeback;
- a Repository/PR detail dashboard or independent PR route;
- exposing raw Request evidence in the product UI;
- treating current `codex_app_server_transport::transport::remote_control::websocket`
  rows as trusted transport evidence without a separate exact transport,
  success, and identity contract;
- release, production deployment, or destructive reset in the design/ticketing
  phase.
