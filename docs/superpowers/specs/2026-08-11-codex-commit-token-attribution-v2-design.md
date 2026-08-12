# Codex Commit Token Attribution v2 Design

**Date:** 2026-08-11
**Status:** Approved target contract; cutover remains blocked pending official sub2api usage-reader authorization for the real canary
**Scope:** `ae-cli`, backend attribution/reconciliation/read models, frontend Activity, repository administration
**Supersedes for target behavior:** [Codex Token Attribution Ledger POC](./2026-08-05-codex-token-attribution-ledger-poc-design.md)
**Related:**

- [Architecture](../../architecture.md)
- [End-to-End Page Loading Performance](./2026-07-14-end-to-end-page-loading-performance-design.md)
- [Stateless Team Usage Prewarm Worker](./2026-07-25-stateless-team-usage-prewarm-worker-design.md)
- [Sessionless Local Tool Attribution](./2026-05-13-sessionless-local-tool-attribution-design.md)
- [Post-Commit Async Attribution Sync](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)

## 1. Contract Status And Migration Boundary

This specification defines the approved production target for Codex commit
Token attribution. It does not describe the currently deployed/runtime POC and
does not authorize a release, deployment, data reset, or `sub2api` change.

Until the explicit v2 cutover:

- current code and the 2026-08-05 POC remain the runtime truth;
- v2 writes and reads use an isolated shadow epoch;
- current Activity and readiness must not consume v2 canary data;
- `docs/architecture.md` continues to describe the implemented runtime.

At cutover, this specification becomes the active attribution contract. The
cutover must update `docs/architecture.md` in the same delivery and preserve the
2026-08-05 document as historical POC context.

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

For a Request accepted into the formal commit ledger, `sub2api` is the only
Token value authority. Local Codex artifacts provide correlation and
calibration evidence only.

The formal Request total is:

```text
total_tokens
= input_tokens
+ output_tokens
+ cache_creation_tokens
+ cache_read_tokens
```

The ledger preserves all four components and the effective requested model
returned by the current `sub2api` admin usage contract. It does not persist API
key, account, upstream routing model, prompt, response, code, patch, command, or
local path in the long-lived pool.

AI Efficiency does not attempt to equal all AI usage. A Request without
deterministic committed-code evidence remains local and never contributes
formal AE Token.

The accounting invariant is:

> Every reconciled Request associated with committed code contributes exactly
> the official `sub2api` Token once globally.

## 4. Local Claim Construction

The local v2 claim group binds:

```text
relay_provider_id
request_ids[]
thread_id / turn_id
structured mutation evidence
repo / worktree evidence
commit allocation sequence
local calibration envelope
```

Rules:

- `relay_provider_id` is the backend provider selected by discover and is
  frozen when the group is created;
- provider switching never rewrites an older pending group;
- the local correlation seam must bind the exact `sub2api` usage `request_id`
  to `thread_id + turn_id`; transport connection or handshake IDs are not
  Request identities;
- Codex is deterministically configured with
  `model_providers.<provider>.supports_websockets = false`. The trusted
  `response.completed.response.id` in the Codex Responses SSE SQLite log is
  the official identity. Its `response.output[].id`/`call_id` must intersect
  exactly one rollout turn's `response_item` transport IDs; ambiguous or
  unmatched evidence fails closed. Responses WebSocket remains unsupported
  until Codex provides an equivalent trusted persistent seam;
- any accepted legacy `client:` prefix is normalized exactly once;
- one turn may contain multiple Requests and all Requests share the turn's
  mutation set;
- local Token exists once at group level for calibration and is never treated
  as per-Request official Token;
- the stable group ID must not depend on a file path, line number, or the
  current count of late-arriving Requests.

The local privacy boundary excludes prompt, response, reasoning text, code,
diff, patch content, command arguments/output, API key, API key ID, raw JSONL,
raw SQLite rows, raw spans, and local paths from upload.

## 5. Deterministic Commit Evidence

Formal commit association accepts only:

- structured Codex mutation evidence;
- deterministic comparison with Git index/tree/commit content;
- explicit Git rewrite or lineage evidence.

It rejects cwd, timing, path proximity, text similarity, ordinary command
workdir, and "the next commit probably owns it" heuristics.

The system proves only that mutation evidence is present in a commit. It does
not claim to distinguish later human editing, formatters, conflict resolution,
or another agent's rewrite. Evidence that cannot be proven is `unverified` and
does not enter formal commit Token.

## 6. Event-Driven Delivery

v2 remains Git-event-driven and adds no daemon or periodic synchronization:

- `post-commit` closes or revises deterministically matched groups;
- `post-rewrite` records explicit old-to-new mappings;
- `pre-push` wakes the outbox and returns fail-open immediately;
- `ae-cli sync` remains a manual diagnosis and recovery entry.

Failed delivery remains in a durable local outbox. Hooks never block commit or
push. A runner that receives new work while draining must consume it or
reliably start a successor; it may not require another Git event.

The client deletes only data covered by explicit server ACKs:

- a Request ID only after `persisted` or `duplicate_identical`;
- the calibration envelope only after its independent ACK;
- conflicts, unknown responses, and unacknowledged items remain local.

The local unresolved and audit-minimal state is retained for at most 90 days
and cleaned lazily on later hook, sync, or CLI activity.

## 7. Hot Claim And Reconciliation Contract

The backend keeps hot claim groups and Request claims for at most 90 days.

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

Partial groups expose only the reconciled lower bound. Before source expiry,
the reconciler performs a final attempt. The finalization deadline is at least
24 hours earlier than the nominal upstream retention boundary so scheduled
cleanup cannot race the last lookup. At finalization:

1. freeze allocation;
2. ensure every reconciled Request is materialized;
3. materialize unresolved coverage gaps with zero official Token;
4. remove Request ID, local calibration, and hot claim detail in the same
   retryable transaction boundary.

Long-lived product data never contains Request ID. Operational metrics cover
pending age, reconciliation latency, mismatch/ambiguity, expiry, finalization,
and cleanup failure without exposing Request identifiers in the UI.

## 8. Long-Lived Usage Pool

Official Token is stored once in a unified long-lived pool:

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

The canonical pool key covers the Relay provider, user, sorted counting commit
set, requested model, and a non-empty 15-minute UTC bucket based on the upstream usage time.
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
- Request claims may be deleted without affecting Activity reads.

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
- Token belongs to upstream usage time, not commit time.

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
- stale/error Usage never produces an estimate.

A transient Usage resolver error is represented as a retryable local ratio
error with `denominator_unavailable`; it does not suppress committed totals,
trend, readiness, Repository, or PR data from the same Activity context.

The donut labels are `Used for committed code` and `Other Token`. `Other Token`
does not mean non-development work. When the adjacent equal period is fully
comparable, the headline displays percentage-point change (for example,
`+8 percentage points`), not relative growth. The comparison is omitted when
the prior period crosses the v2 cutover or either period is incomplete.

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

Normal setup remains install, login, and discover. Explicit enable, hook
management, repo init, sync, and diagnostics are advanced/recovery paths. v2
does not require Codex OTel. Upgrade removes only AE-managed Codex OTel config
and never touches user-managed OTel.

Product DTOs and UI exclude Request ID, raw claim rows, local calibration
Token, API key/account, prompt/response/code, local paths, pending Request
counts, and coverage-gap Request details. Aggregate operational health belongs
in backend metrics rather than normal Activity UI.

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

After at least seven continuous stable days and explicit adoption/health gates,
remove v1 ingest/read code, v1 frontend Bucket/Request UI, AE OTLP ingest,
`aeo_*` credentials, and fields used only by the POC. Platform and CLI releases
remain separate release units.

## 15. Acceptance Matrix

Implementation is not complete until tests and readbacks cover:

- personal, authorized member, team, administrator Repository operations, and
  unauthorized access;
- provider switching, multi-Request turns, duplicate/conflict, response loss,
  partial reconciliation, expiry, shared, late claims, rewrite, orphan, and
  cherry-pick;
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

- changing `sub2api` source or coupling directly to its database;
- a daemon or periodic local synchronizer;
- uploading uncommitted/unverified usage;
- splitting one Request's Token by lines, files, commits, repositories, or PRs;
- inferring a primary PR;
- individual ranking, productivity scoring, cost allocation, or chargeback;
- a Repository/PR detail dashboard or independent PR route;
- exposing raw Request evidence in the product UI;
- release, production deployment, or destructive reset in the design/ticketing
  phase.
