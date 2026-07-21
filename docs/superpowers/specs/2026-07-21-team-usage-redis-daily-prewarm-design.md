# Team Usage Redis Daily Prewarm Design

**Status:** Rejected historical design. The UTC-hour reconstruction contract
failed its mandatory POC and must not be implemented. Its approved replacement
is `2026-07-21-team-usage-segmented-timezone-prewarm-design.md`; that replacement
is separately blocked on its segmented-source POC before implementation.

**Date:** 2026-07-21

**Replaced by:**

- `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`

**Historical references:**

- `docs/superpowers/specs/2026-07-20-team-usage-experiment-matrix-design.md`
- `docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md`

This document and the earlier documents remain historical experiment records.
The replacement spec defines the approved next contract on top of PR #192's
retained Redis scope-origin baseline. Neither design describes current
production runtime unless and until a separately approved implementation is
merged and released.

## Problem

The retained scope-origin baseline removes the former per-user trend fan-out,
but one genuinely cold Team Usage page still waits for a serial Relay chain:

1. paginated Relay user-directory reads;
2. chunked batch usage-stat reads; and
3. one aggregate `users-trend` read for the complete range.

The staging audit of exact PR #192 head `627a7123` measured approximately 13.1
seconds inside the application and 13.7 to 14.4 seconds at the client for the
four concurrent Summary, Trend, Members, and Organization lanes. Relay time
accounted for approximately 11.9 seconds: 2.2 seconds for directory pages, 2.9
seconds for usage-stat batches, and 6.7 seconds for aggregate trend. The
remaining application projection, encoding, and transport work was about 1.2
seconds.

The same audit exposed a separate Redis reliability problem. The runtime
client starts with one connection and applies a 100-millisecond dial, read,
write, and pool timeout. Four concurrent lane reads forced pool expansion,
some dials exceeded that budget, and fail-open behavior caused only part of the
response/origin cache set to be written or read. The immediate second round
therefore still made Relay calls and took about five seconds on three lanes.

The product exposes three dominant windows: today, seven days, and thirty
days. These windows overlap heavily. Recomputing the previous complete days on
each new actor/range cache miss wastes Relay and database work. Only the moving
edge near each user's local "today" requires frequent refresh.

## UTC-Hour POC Decision

The staging POC ran against exact PR #192 head `627a7123` on the immutable
staging image at Helm revision 44. Production remained unchanged at revision
69. The probe made exactly four read-only aggregate requests: one canonical
UTC hourly request and one direct daily comparison for each of `UTC`,
`America/Los_Angeles`, and `Europe/Berlin`.

| Gate | Measured | Limit | Result |
| --- | ---: | ---: | --- |
| Relay duration | 10.081 s | `< 25 s` | Pass |
| Hourly source body | 7,464,074 bytes | `< 33,554,432 bytes` | Pass |
| Decoded hourly points | 45,331 | `< 1,000,000` | Pass |
| Unique users | 364 | `< 5,000` | Pass |
| Probe peak RSS | 59,375,616 bytes | `< 201,326,592 bytes` | Pass |
| Largest serialized UTC-day shard | 157,531 bytes | `< 1,048,576 bytes` | Pass |
| Total serialized 32-day generation | 3,165,743 bytes | `< 16,777,216 bytes` | Pass |
| `UTC` reconstruction | Mismatch | Exact tokens and cost delta `<= 1e-9` | **Fail** |
| `America/Los_Angeles` reconstruction | Mismatch | Exact tokens and cost delta `<= 1e-9` | **Fail** |
| `Europe/Berlin` reconstruction | Mismatch | Exact tokens and cost delta `<= 1e-9` | **Fail** |

This is a semantic failure, not a capacity failure. The measured body, point,
user, memory, and serialized-size gates all have headroom, but all three
required equivalence checks failed. UTC-hour facts therefore cannot be used to
reconstruct exact timezone-local daily totals under the current Relay contract.

Sub2API source confirms the contract gap. `parseTimeRange` in
`backend/internal/handler/admin/dashboard_handler.go` uses the request timezone
to calculate only the query's start and end instants. `GetUserUsageTrend` in
`backend/internal/repository/usage_log_repo_trend.go` groups rows with
`TO_CHAR(u.created_at, format)` and does not receive or apply the request
timezone in SQL. An hourly response label consequently cannot be interpreted
as a reliable UTC bucket and then shifted into another timezone. The original
UTC-hour design is blocked, Tasks 2-10 in its implementation plan must not run,
and its proposed production constants are not locked by this failed gate.

## Proposed Common-Timezone Daily-Bucket Replacement

This replacement preserves the current source semantics instead of attempting
cross-timezone reconstruction. It is a proposal only; implementation requires
explicit user approval and a new or replaced implementation plan.

The deployment provides an explicit allowlist of IANA timezone names. The
initial recommended set is:

```text
UTC
Asia/Shanghai
America/Los_Angeles
Europe/Berlin
```

The list is configuration, not a hard-coded product promise. Empty or invalid
configuration disables daily prewarming. Duplicate names are rejected after
canonical validation, and a bounded maximum list length must be fixed in the
follow-up plan.

For each configured timezone, the prewarmer requests provider-wide
`users-trend` data directly with `granularity=day` and that exact timezone. It
caches only the returned per-user date label, optional token total, and actual
cost after applying the existing truncation and validation rules. Names,
emails, credentials, raw responses, and directory records are discarded. The
implementation must not derive one timezone from another and must not rebuild
daily facts from hourly labels.

Each immutable generation contains the current usage-stat snapshot plus daily
shards keyed by provider version, configured timezone digest, source timezone,
and returned date label. Its publish-last manifest records coverage separately
for every configured timezone. The newest local dates are refreshed on the
bounded moving schedule; a jittered correction cycle refetches the complete
thirty-date window directly for every configured timezone so late usage and
billing corrections converge. A follow-up daily-source POC must fix request,
decoded-point, per-shard, total-generation, timeout, and Redis TTL bounds before
production code is approved.

A standard Team Usage request may use this prewarm only when its requested IANA
timezone exactly matches one configured entry and its requested dates and
granularity are fully covered by that timezone's direct daily generation. The
request still resolves current representative authorization and Relay identity
mapping, intersects cached facts with the current authorized Relay IDs, and
projects the existing DTOs. Every unconfigured timezone, custom range,
unsupported granularity, missing or invalid generation, and Redis error remains
request-driven through the retained exact scope-origin path.

This design intentionally trades bounded source duplication across a small
configured timezone set for semantic equivalence with the current Relay API.
It does not claim timezone-neutral reuse and does not change Sub2API.

> **Rejected candidate record:** The remaining sections describe the UTC-hour
> candidate as it stood before the hard gate. They are retained for design
> history only and must not be implemented. Where they conflict with the POC
> decision or proposed replacement above, the sections above are authoritative.

## Goals

1. Move standard-window Relay aggregation off the user request path by
   prewarming provider-wide Redis facts.
2. Reuse one timezone-neutral history for today, seven-day, and thirty-day
   requests across all currently authorized representative scopes.
3. Refresh the moving global-day edge frequently while retaining bounded
   correction of late or adjusted historical usage.
4. Let Summary, Members, and Organization consume stats/history without
   waiting for an actor-specific full-range trend origin.
5. Keep current authorization authoritative and intersect every cached Relay
   fact with the freshly resolved representative scope.
6. Preserve fail-open behavior, arbitrary-range compatibility, and all current
   Team Usage HTTP response contracts.
7. Make concurrent Redis reads reliable without introducing a Pod-local
   completed-result cache.

## Non-Goals

- Do not modify Sub2API source or require a coordinated Sub2API release.
- Do not store completed Team Usage results in Pod memory.
- Do not cache authorization decisions, representative assignments, names,
  emails, credentials, tokens, or raw usage logs in the prewarm values.
- Do not change the frontend's today, seven-day, or thirty-day controls.
- Do not remove support for custom ranges, granularities, or browser timezones.
- Do not promise that a request after total Redis loss is fast. That request
  remains a bounded authoritative refill path.
- Do not treat completed historical days as permanently immutable; delayed
  ingestion and billing corrections remain possible.
- Do not weaken the existing 5,000-user aggregate-trend truncation guard.

## Design Overview

The implementation adds one provider-wide Redis prewarm generation containing:

1. a current usage-stat snapshot keyed by Relay user ID; and
2. sparse UTC-hour trend facts physically sharded by UTC day.

One day shard contains up to 24 logical UTC-hour buckets. A thirty-day browser
window plus the global timezone margin therefore needs at most 32 Redis day
shards rather than hundreds of individual hour keys. The payload contains only
Relay user IDs, actual cost, and optional token totals.

A background prewarmer acquires a token-protected Redis lease, fetches Relay
data, writes immutable generation keys, and publishes a manifest pointer only
after every required value is available. Other Pods keep no completed result;
they observe the Redis manifest or skip the cycle while the lease is live.

Team Usage requests continue to resolve the current representative scope from
the authoritative local database and Relay identity mapping. They then read the
published Redis generation, filter it to currently authorized Relay IDs, and
compose the requested local-time window. Existing response caches remain the
outermost per-lane acceleration layer.

## Timezone-Neutral Window

The supported IANA timezone range spans UTC-12 through UTC+14. Prewarming the
requested thirty local dates with one additional UTC day on both sides covers
every whole-hour timezone boundary. The canonical history window is therefore
32 UTC dates.

The prewarmer requests `granularity=hour` in UTC. A request converts each
cached UTC hour to the requested IANA location and then groups it into local
hours or local dates. This preserves daylight-saving transitions for zones
whose offsets align to whole hours.

Sub2API does not expose a finer aggregate than one hour. Timezones whose active
offset contains non-zero minutes, such as half-hour or 45-minute zones, cannot
be reconstructed exactly from UTC-hour facts. Those requests use the existing
timezone-specific scope-origin path and retain its Redis response caching. The
implementation must inspect the effective offset across the requested window;
it must not silently round or approximate a non-hour boundary.

Custom windows outside the 32-day coverage or unsupported granularities also
use the existing authoritative scope-origin path.

## Redis Data Contract

All keys use the configured deployment namespace and SHA-256-digested
dimensions, following the existing read-model key style. Human identities and
raw ID lists do not appear in key text.

### Manifest

The manifest is keyed by namespace, provider ID, provider configuration
version, schema version, and the canonical UTC coverage end. It contains:

- schema version;
- opaque generation ID;
- provider ID and provider configuration version;
- coverage start and end UTC dates;
- creation time and source status;
- the current-stats value key;
- the ordered UTC-day shard keys; and
- the refresh class of each referenced value.

The manifest is published last. Readers never discover a partially written
generation. Generation values outlive the manifest so a reader holding an old
manifest cannot lose its referenced data during one bounded request.

### Current Stats

The current-stats envelope maps Relay user ID to the existing validated
`TeamUserUsageStats` facts required for today and lifetime values. It contains
no Relay directory identity. It is refreshed every 60 seconds and expires only
after a bounded stale window long enough for one failed refresh to recover.

### UTC-Day Shards

Each shard contains one UTC date and a sparse map of Relay user IDs to ordered
hour facts. Each hour fact contains:

- UTC hour start;
- non-negative actual cost; and
- optional non-negative total tokens.

Missing token data retains the current nil-token semantics. Missing users or
hours mean zero usage only when the source response passed the existing
truncation and validation rules. Invalid, duplicate, out-of-order, future, or
out-of-shard points reject the candidate generation.

The last 48 UTC hours are moving shards and refresh every 60 seconds. Older
shards are historical and refresh during the daily correction cycle. Historical
values use a TTL that exceeds one daily cycle, so a delayed cycle does not
create a synchronous user-facing miss.

### Size Bounds

The prototype must measure the actual 32-day hourly response before production
code fixes payload constants. The implementation then sets explicit bounds for:

- aggregate HTTP response bytes;
- decoded point count;
- one Redis day-shard payload;
- one current-stats payload; and
- total referenced generation bytes.

An over-limit response is never cached. The request or prewarm cycle fails open
through the existing scoped path, and metrics record the rejected reason
without logging the payload or IDs.

## Prewarm Lifecycle

The server starts the prewarmer only after database migration, Relay provider
runtime, Redis, and metrics initialization succeed. It does not block liveness
or HTTP startup.

The current primary Relay provider receives an independent Redis lease keyed by
namespace, provider ID, provider version, refresh class, and canonical window.
Team Usage does not read non-primary providers, so the prewarmer does not query
them. Lease release remains token checked. A canceled or superseded primary
provider version cannot publish a manifest for the new version.

The lifecycle has three bounded cycles:

1. **Startup recovery:** Read the current manifest. Fetch only missing or hard
   expired current/history facts. A healthy Redis deployment normally makes
   this a no-origin operation.
2. **Moving-edge refresh:** Every 60 seconds, fetch the current stats and the
   latest 48 UTC hours, write new generation values, then publish the manifest.
3. **Historical correction:** Once per UTC day after a jittered delay, refresh
   all 32 UTC dates so delayed usage and billing corrections converge.

Failures retain the last valid manifest until its stale deadline. The cycle
uses bounded exponential retry on later scheduled attempts; it does not loop
inside one tick or block user traffic. No error result is cached as success.

## Relay Calls

The retained Sub2API contracts permit two immediate call-count reductions:

- request `/api/v1/admin/users` with `page_size=1000` instead of 200; and
- send at most 500 Relay IDs to one `POST /dashboard/users-usage` request
  instead of the current 100-ID chunk.

Sub2API's pagination contract accepts up to 1,000 rows. Its batch usage handler
normalizes an unbounded ID array and executes one `user_id = ANY($1)` query; it
does not impose a 100-ID HTTP contract. AI Efficiency retains a 500-ID safety
bound to limit request/response size.

The prewarmer uses the provider-wide user list only to determine the Relay IDs
whose current stats should be fetched. The published values omit the directory
records. User requests still perform current identity reconciliation and
authorization filtering before exposing any cached fact.

## Request Path

For a supported standard window, each Team Usage lane follows this sequence:

1. resolve the current representative scope and current Relay mappings;
2. read one valid prewarm manifest;
3. MGET the current-stats value and required UTC-day shards;
4. validate provider generation, coverage, times, payload bounds, and point
   ordering;
5. intersect all facts with the currently authorized Relay ID set;
6. convert UTC hours into the requested browser timezone; and
7. project the existing lane DTO and freshness metadata.

Summary, Members, and Organization no longer require a full actor-specific
trend origin when the prewarm generation is usable. They aggregate selected
range cost/tokens from the UTC-hour facts and today/lifetime values from current
stats. Trend projects the same authorized hour facts into its current team,
member, and department series.

If only moving-edge values are unavailable, the request may reuse validated
historical shards and fetch the missing current interval. It must not refetch
the previous complete days. If the manifest, current stats, or required
historical coverage cannot be trusted, the complete existing scope-origin path
remains the correctness fallback and may refill Redis after validation.

Response-cache keys, cursor contracts, stale-if-error rules, and DTOs remain
unchanged.

## Redis Runtime Reliability

The shared go-redis client must stop treating connection establishment and
large prewarm reads as 100-millisecond operations while preserving the existing
bounded fail-open behavior of unrelated caches.

The implementation uses:

- at least four pre-created idle Redis connections;
- a one-second dial timeout for background pool establishment;
- a bounded read/write timeout suitable for measured day-shard payloads;
- the existing 100-millisecond caller command contexts for current small read
  models; and
- one separately bounded Team Usage prewarm read budget selected from the POC
  evidence.

Increasing the client transport timeout does not make every cache caller wait
that long because existing domain command contexts remain authoritative. Redis
pool metrics and cache error outcomes remain required staging evidence.

## Concurrency And Consistency

- Redis leases collapse prewarm work across Pods.
- Existing process-local flights may collapse concurrent in-flight work but
  may not retain completed payloads.
- A manifest publication is the only commit point for a generation.
- Readers that start from one manifest never mix current values from a newer
  manifest.
- Historical shards may be reused across generations only when their complete
  key dimensions and validation metadata match.
- Provider configuration changes create new keys and cannot reuse credentials
  or values from an earlier provider version.
- Authorization is resolved before projection and checked again before the
  response is returned, preserving the existing scope-version race guard.

## Failure Behavior

- Redis unavailable: use the existing authoritative scoped path; do not write a
  Pod-local result.
- Prewarm lease busy: serve the last valid manifest or use the scoped fallback;
  do not poll full values while the lease is live.
- Relay failure during prewarm: keep the prior valid manifest and retry on a
  later tick.
- Current stats unavailable: preserve the current explicit unavailable/stale
  semantics for affected fields.
- One moving UTC shard unavailable: fetch only the missing moving interval when
  historical coverage is valid.
- Historical coverage invalid or truncated: reject the generation and use the
  complete existing origin path.
- Non-hour timezone offset: use an exact timezone-specific fallback; never
  approximate day boundaries.
- Redis write failure: the unpublished generation is unreachable and expires
  naturally.

## Observability

Add bounded-cardinality metrics for:

- prewarm cycles by `startup`, `moving`, or `historical` and outcome;
- prewarm cycle duration;
- source response bytes and decoded point count histograms;
- Redis generation bytes and day-shard bytes;
- manifest/current-stats/history cache outcomes;
- last successful moving and historical refresh timestamps; and
- request fallback reason using a closed enum.

Dependency logs retain the existing request ID for user-originated fallbacks.
Background prewarm logs use one generated operation ID and never include user
IDs, emails, cache keys, request bodies, or response bodies.

## Prototype Gate

Before implementing the production cache, run one staging-only POC against the
existing Sub2API source contract:

1. fetch the canonical 32-day UTC window with `granularity=hour` and the
   retained aggregate limit;
2. record sanitized HTTP duration, response bytes, decoded point count, peak
   process memory, and per-day serialized bytes;
3. verify the response is not exactly at the truncation boundary;
4. verify UTC-hour ordering and reconstruct today, seven-day, and thirty-day
   values for a synthetic comparison harness; and
5. compare reconstructed whole-hour timezone totals with direct exact-timezone
   aggregate responses.

Proceed with UTC-hour day shards only when the response fits the existing
30-second Relay deadline, bounded server memory, and explicit Redis payload
limits with safe headroom. If it does not, the fallback design prewarms daily
facts for a small configured set of common timezones and keeps all other
timezones request-driven. The POC result and chosen path must be recorded before
production implementation starts.

## Testing

Implementation follows RED/GREEN TDD with synthetic identities only.

Required tests cover:

- UTC coverage expansion for UTC-12, UTC, and UTC+14;
- DST spring-forward and fall-back reconstruction for a whole-hour timezone;
- non-hour offset detection and exact fallback;
- today, seven-day, and thirty-day reuse of the same day shards;
- sparse hour aggregation and nil-token propagation;
- manifest publish-last behavior and partial-generation invisibility;
- multi-Pod lease collapse without a completed Pod cache;
- moving-edge refresh reusing historical shards;
- historical daily correction replacing only validated generations;
- provider-version isolation and authorization intersection;
- payload, point-count, and truncation rejection;
- Redis failure, Relay failure, cancellation, and stale-manifest behavior;
- directory page size 1,000 and usage-stat chunks no larger than 500; and
- unchanged Summary, Trend, Members, Organization, and Overview DTOs.

Run the focused cache/Relay/Team Usage tests, full backend tests, race tests,
`go vet ./...`, and `go build ./...`. Environment-sensitive staging evidence
remains separate from unit-test results.

## Staging Acceptance

Deploy an immutable exact-head image through the existing two-phase staging
restore flow. Production remains unchanged.

Run three sanitized audits with the same authorized account and deterministic
window:

1. **Empty Redis:** clear only the new prewarm, existing Team Usage origin,
   response, and lease keys. Confirm authoritative success and Redis refill.
2. **Prewarmed cold UI:** retain prewarm keys, clear only the four response
   caches, then issue the four page lanes concurrently.
3. **Immediate warm:** repeat all four lanes without clearing any key.

Acceptance requires:

- all responses are HTTP 200 and preserve business payload equivalence;
- the prewarmed cold UI completes every lane within five seconds;
- the immediate warm round completes every lane within 1.5 seconds;
- the prewarmed request performs no batch stats or aggregate trend Relay call;
- no individual trend endpoint is used;
- Relay 429, 5xx, transport-error, and timeout counts remain zero;
- Redis/cache/lease errors and Redis dial failures remain zero;
- one prewarm cycle runs across concurrent Pods;
- no user request waits for a historical 32-day origin; and
- the empty-Redis fallback is no slower than the current measured baseline by
  more than one second.

The audit records request/dependency durations and cache metrics by request or
operation ID. It does not retain credentials, raw response bodies, user lists,
or unredacted Redis values.

## Rollout And Rollback

The feature is guarded by a deployment configuration flag and remains disabled
until staging acceptance passes. Enabling it requires no schema migration.

Rollback disables the prewarmer and prewarm reader, returning every request to
the retained PR #192 scope-origin behavior. Generation keys expire by TTL and
do not need a destructive flush. Directory page and stats batch-size changes
are independently revertible.

`docs/architecture.md` must be updated only when the implementation becomes the
current runtime. This historical design spec remains the record of the chosen
contract and trade-offs.

## Alternatives Rejected

### Expand One Timezone's Daily Range

Caching extra Asia/Shanghai daily buckets does not reproduce another timezone's
natural-day boundaries. Expanded coverage works only when the cached source
facts are timezone neutral, which is why the primary design uses UTC hours.

### Cache Three Complete Ranges

Separate today, seven-day, and thirty-day values duplicate the same history and
still fragment reuse by actor and timezone. Day shards let all three windows
share one source generation.

### One Redis Key Per User Per Hour

That layout maximizes overlap but creates thousands of MGETs and high key churn
for one team page. UTC-day shards retain per-user filtering while bounding one
request to at most 32 Redis values.

### Treat Historical Days As Immutable

Late ingestion and billing correction can alter earlier days. A daily bounded
correction cycle provides convergence without moving all history back onto the
request path.
