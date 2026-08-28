# Attribution V2 Contract

This contract describes the current production `formal_v2` path for associating
Codex Token with committed code. Read it before changing reporting enrollment,
managed Git hooks, local claim scanning, compact checkpoints, claim ingest,
Request reconciliation, hot-detail retention, usage pools, commit lineage, or
Activity/readiness aggregation.

Repository identity follows the [repository binding](./repository-binding.md)
contract. Relay access and identity follow
[relay user access](./relay-user-access.md). Activity denominator semantics
reuse [usage and quota](./usage-and-quota.md), while its collection controls
follow [collection navigation](./collection-navigation.md).

## Scope and Ownership

Attribution v2 answers where deterministically proven committed-code Codex Token
went. It does not measure all AI use, productivity, cost, billing, chargeback,
or individual contribution quality.

The current path separates five authorities:

```text
Relay identity          -> provider and upstream usage owner
Local workspace evidence -> repository, workspace, checkpoint, commit trigger
Commit proof             -> structured mutation replayed against Git content
Attribution accounting   -> hot claim, reconciliation, long-lived usage pool
Usage aggregation        -> Activity projection and Usage-backed ratio
```

These layers exchange minimized identities and validated facts. No layer may
infer another layer's authority from time, cwd, path similarity, model, Token
similarity, branch name, or UI state.

`formal_v2` is the only epoch consumed by current Activity and readiness.
`shadow_v2` remains isolated non-formal data. The server owns one protocol
contract containing `ledger_epoch`, `v1_write_policy`, and
`minimum_cli_version`; contradictory combinations fail router startup and fail
closed in the CLI.

## Relay Identity

Enrollment binds one reporter installation to one local platform user. The
scoped `aer_*` reporter credential authenticates compact checkpoint,
Repository-resolution, and v2 claim requests; it does not grant browser
repository administration.

`ae-cli discover` persists the selected Relay provider ID. Each newly captured
commit trigger and claim group freezes that provider. Later provider switching
does not rewrite pending or materialized evidence.

For `relay_official`, backend reconciliation re-resolves the exact frozen
provider and the installation owner's current Relay user mapping. Upstream
usage must belong to that Relay user. Provider absence/disablement, missing
owner mapping, owner mismatch, ambiguity, or inconsistent usage fails closed.
The backend never substitutes the current primary provider or compares an
upstream Relay user ID with the local database user ID.

## Workspace and Checkpoint Evidence

A retained trigger freezes the normalized server, reporting owner, Repository
ID/key and canonical remote identity, workspace ID, compact checkpoint event,
commit SHA, Relay provider ID, and capture time.

The managed `post-commit` path persists the compact checkpoint before scanning
or delivering v2 claims. This order is required because claim admission checks
every allocation against the existing checkpoint's owner, Repository,
workspace, event, and commit. The reporter checkpoint deliberately skips
legacy `tool_usage_events` binding.

`post-rewrite` records explicit old-to-new commit evidence. Fail-open
`pre-push` only wakes existing reporting work. Checkpoint/rewrite response loss
is retained in the local hook queue; a later runner replays it by stable event
identity before the dependent claim can be accepted.

Another checkout may lend its Git root to a retained task only when server,
reporting owner, Repository ID/key, and canonical remote all match. Each commit
must be independently reachable from that checkout's `HEAD` or a Git ref. The
runtime root may change; the original workspace, checkpoint, commit, provider,
and evidence identity do not.

## Deterministic Commit Proof

A formal allocation requires structured Codex file-mutation evidence and exact
comparison with Git index/tree/commit content. Supported Add, Update, and Delete
operations must replay to the committed tree. An exact supported generated
wrapper may expose an `apply_patch` payload, but wrapper recognition never
replaces Git-content proof.

The proof rejects timing windows, cwd/path proximity, patch similarity alone,
ordinary command workdir, and the assumption that the next commit owns prior
activity. It proves only that the structured mutation is present in the commit;
it does not distinguish later human editing, formatting, conflict resolution,
or another actor's rewrite. Unprovable evidence remains local and contributes
no formal Token.

One allocation uses that allocation's evidence digest. Appending another
allocation produces an ordered composite digest. An exact replay of the first
allocation preserves its original identity and acknowledgement.

## Token Source Contract

Each claim group selects exactly one Token source:

- `relay_official` carries trusted Responses HTTP client Request IDs and an
  optional local calibration envelope. Relay usage is the accounting truth.
- `codex_local` carries bounded requested-model/15-minute usage aggregates from
  trusted Responses WebSocket transport and successful sampling evidence. It
  carries no Request ID or calibration and is suitable for committed-code
  Activity, not billing or security audit.

A turn containing both sources is `mixed_token_sources` and remains local.
Transport connection IDs, generic request IDs, upstream response IDs, raw
`resp_*` values, or unmatched rows are not official Request identity.
Unrecognized WebSocket transports and incomplete/ambiguous evidence remain
fail-closed; the system never synthesizes a Request ID or Token.

Both sources conserve four components:

```text
total_tokens
= input_tokens
+ output_tokens
+ cache_creation_tokens
+ cache_read_tokens
```

For local WebSocket usage, cached input and cache-write components are removed
from raw Codex input before storing the normalized input component. Reasoning is
already part of output and is not added again. Negative values, overflow,
invalid cache decomposition, inconsistent totals, missing requested model or
usage time, and non-15-minute bucket boundaries fail closed.

WebSocket cumulative snapshots admit only exact baseline/delta sequences.
Repeated terminal rows count once. An explicit same-turn compaction boundary
may establish one zero-Token baseline restatement; the same contradiction
without that boundary is invalid.

## Event-Driven Local Delivery

Managed `post-commit`, `post-rewrite`, and `pre-push` hooks plus manual
`ae-cli sync` are the current triggers. There is no daemon or periodic local
synchronizer.

Hooks fail open for Git. They persist/wake durable per-workspace tasks and start
a short-lived detached runner. One transient machine owner serializes work for
the normalized server/reporting owner. Contenders coalesce, work arriving during
a pass is consumed or handed to one bounded successor, each workspace receives
a bounded quantum, and the owner exits when matching work is drained.

One retained trigger cannot block a runnable sibling. Missing/unreachable
commits, missing providers, checkpoint mismatches, and Repository/owner
mismatches retain the original trigger with a safe categorized local failure.
Restoring the exact commit or identity can make it runnable later without
rebinding heuristics or duplication.

The scanner discovers active and archived Codex sources once per runner pass,
streams each source once for all retained triggers, and stores digest-only
source/trigger completion. Claim candidates are persisted before completion is
marked and before upload, so timeout, process loss, and response loss resume
without rereading completed units. A scanner-semantics version change rebuilds
only local progress.

Local evidence, triggers, claim candidates, acknowledgement digests, and
terminal diagnostics are retained for at most 90 days and cleaned lazily. Only
an exact known synthetic test-Repository identity may move to the local audit
quarantine. Missing paths, age, temporary-looking names, or failure reasons are
never general deletion evidence.

## Claim Admission and Replay

The reporter-only claim endpoint accepts strict bounded JSON: 1-20 groups per
batch, 1-100 allocations per group, at most 100 Request IDs or local-usage
buckets per group, bounded identities, and a bounded request body. Each group
is admitted in its own transaction.

Admission requires:

- an authenticated enabled installation principal;
- one existing enabled Relay provider matching the frozen provider ID;
- schema-v2, non-empty stable group/evidence identity, and valid source shape;
- ordered non-empty commit allocations;
- for every allocation, an existing checkpoint with the same installation
  owner, Repository, workspace, event ID, and commit SHA;
- valid conserved Token/calibration values and source exclusivity.

Group identity is unique. Official Request identity is unique by
`(relay_provider_id, request_id)`. Identical canonical replay returns
`duplicate_identical` and changes no pool or claim count. Conflicting canonical
content is rejected and cannot receive a success acknowledgement.

Allocation updates are append-only: an incoming sequence must preserve the
existing prefix. Local WebSocket usage may add buckets or grow existing
components/counts monotonically. Removing/regressing a bucket, decreasing a
component, switching source, duplicating an identity, or overflowing fails
closed. An accepted change atomically replaces only that group's prior pool
contribution.

The CLI deletes or minimizes local evidence only after explicit matching
`persisted` or `duplicate_identical` acknowledgements. Unknown, missing,
duplicate, partial, or protocol-mismatched acknowledgements remain blocking
recovery state. An unchanged `conflict`, `rejected`, or `rolled_back` snapshot
is retained as terminal local evidence, excluded from retransmission, and does
not block later unrelated triggers. Monotonic new local usage reopens an
acknowledged WebSocket group.

## HTTP Reconciliation and Hot Lifecycle

Hot claim groups and Request claims live for at most 90 days. HTTP reconciliation
uses the frozen provider's narrow Request-usage reader with a lookup limit of
two:

- zero rows remains pending and retryable;
- exactly one row with the current Relay owner and conserved usage reconciles;
- more than one row is an ambiguous invariant failure.

Database leases, bounded concurrency, jittered retry, and expired-lease
recovery prevent replicas from multiplying one lookup. Scheduling is capped at
the final-attempt boundary 24 hours before hot expiry. A final attempt must
finish; merely acquiring a lease is not enough to finalize the group.

Each reconciled Request is materialized into the long-lived pool in the same
transaction that records reconciliation. Before expiry, group finalization:

1. waits for every final Request attempt and any active lease;
2. verifies every reconciled Request is materialized;
3. adds zero-Token unresolved coverage for terminal gaps;
4. deletes Request rows and strips Request/calibration/allocation/local-usage
   hot detail in the same retryable transaction.

WebSocket ingest materializes directly without Request rows or Relay calls;
finalization only freezes its pool contribution and strips hot local detail.
Finalization failure receives bounded retry while other groups continue. At the
hard boundary, fail-safe cleanup strips and deletes poisoned hot detail without
inventing Token, Request count, or coverage.

## Long-Lived Pool Conservation

Accepted contribution is stored exactly once in `attribution_usage_pools`. The
canonical identity includes ledger epoch, Relay provider, user, requested
model, sorted counting-commit set, and a non-empty 15-minute UTC bucket from the
authoritative usage time. Long-lived rows contain no Request/response ID,
calibration, local aggregate, API key/account, prompt, code, patch, command, or
path.

Pool-to-commit relations have three meanings:

- one counting commit is `direct`;
- multiple counting commits are `shared`, while Token remains stored once;
- explicit cherry-pick lineage is `inherited_non_counting` and never increases
  accounting totals.

Appending or explicitly rewriting counting commits migrates or merges the
contribution rather than copying it. Explicit old-to-new rewrite chains are
idempotent; conflicting mappings and cycles fail closed. Authoritative
reachability may mark a relation orphaned, but orphaning never deletes
historical Token or readiness evidence.

Commit-to-PR is many-to-many. A pool's full involved Token may appear in each
related PR, so PR rows are non-additive and expose no global-total percentage.

The conservation behavior is current, while transaction ownership remains
distributed across implemented callers. No caller may assume every transition
is already owned by one module.

## Activity and Readiness

Activity aggregates long-lived `formal_v2` pools in PostgreSQL and counts each
pool once for scope totals. Every read revalidates personal/member/team scope
and current provider configuration. Shadow pools, provider-zero migration rows,
inherited-only relations, and hot claims cannot become formal Activity truth.

Repository ranking uses direct Token; shared participation is separate and
non-additive. PR ranking uses involved Token and overlap state. Daily buckets
use the authoritative usage time converted to the requested IANA timezone, not
commit time. Product DTOs exclude Request IDs and hot-detail state.

The code-Token ratio uses formal committed Token over complete fresh Usage Token
for the exact scope, provider set, range, and timezone. Personal/member
denominators are overflow-checked sums of selected-window Usage trend points,
never cumulative stats. The numerator is clamped to fully elapsed 15-minute
pools at Usage `as_of`.

Missing/stale/partial/contradictory Usage, provider mismatch, negative/overflow,
or coverage gaps make only the ratio unavailable or lower-bound. They do not
hide committed totals, trend, Repository/PR projections, or readiness. Provider
switching likewise preserves historical formal Activity while making a ratio
unavailable when exact provider coverage no longer matches.

Reporting readiness becomes `active` only after a formal direct/shared pool.
It never activates from an installation alone, an uncommitted claim, shadow/v1
data, or inherited-only lineage, and it does not regress after provider switch
or orphan marking. The authenticated status endpoint is `no-store`, exposes one
aggregate state and latest qualifying acceptance time, and contains no device
inventory or hot identity.

## Privacy Boundary

Uploaded and long-lived data excludes prompt/response/reasoning text, code,
diff/patch content, tool arguments/output, raw JSONL/SQLite rows, raw spans,
local paths, API keys/accounts, and WebSocket response identity. Local scan
progress uses digests for source/turn/Request evidence.

Metrics expose bounded claim status, age, near-expiry, reconciliation,
finalization, cleanup, and aggregate local recovery outcomes. Request IDs,
thread/turn IDs, users, providers, cache keys, local paths, and raw errors are
not metric labels or normal product fields.

## Retired and Compatibility Boundaries

The v1 usage-bucket/revision schema and routes, AE OTLP ingest/authentication,
legacy `aeo_*` issuance/acceptance, and installation OTLP/OTel columns are
absent. CLI compatibility code may recognize and strictly scrub an exact
AE-managed exporter during upgrade; it preserves user-managed OTel and is not
an attribution data path.

Developer-visible platform sessions, session lifecycle commands, and the local
proxy data plane are retired. Formal v2 uses compact checkpoints and does not
consume legacy `tool_usage_events`. The older user-authenticated checkpoint and
tool-usage path remains only for non-v2 compatibility, including current
non-Codex collectors; it cannot feed formal Codex Activity pools.

The current implementation parses local Codex evidence directly. It does not
consume LoongSuite output, use resident listeners/timers, materialize
Claude/Kiro formal pools, or credit Kiro into formal pools.
