# Team Usage Redis Batch Trend Design

**Status:** Implemented and locally verified on `perf/team-usage-batch-trend`

**Base:** `feat/platform-loading-performance@14098806`

**Refines:**

- `docs/superpowers/specs/2026-07-19-team-usage-shared-trend-cache-design.md`
- `docs/superpowers/specs/2026-07-19-team-usage-cold-loading-design.md`
- `docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md`

This design replaces the process-local per-user Team Usage trend result cache
from the shared-trend-cache design. The historical documents remain records of
the earlier runtime contract. Their response-cache, Redis GET retry, and
provider-wide 24-slot origin-limiter decisions remain active unless this design
explicitly changes them.

## Problem

A truly cold Team Usage load for the staging representative scope still depends
on 235 per-user Sub2API trend GET requests. The existing 60-second cache can
collapse and reuse those reads only inside one `ai-efficiency` Pod and one
provider instance. It cannot reuse a trend loaded by another Pod, another
authorized representative, or the same Relay user's personal usage request.

Sub2API already exposes:

```http
GET /api/v1/admin/dashboard/users-trend
```

The endpoint returns the top active users for a date range, grouped by date and
user. A live proof of concept against the deployed Sub2API version established
that, when the result is not truncated, its selected-user dates, token totals,
and actual costs match the individual admin trend endpoint. On the current
staging path, one cold batch took approximately seven seconds and one warm batch
took less than one second. Replacing 235 individual origins with one batch is
therefore semantically viable and may bring the complete cold page below the
nine-second acceptance target.

The batch endpoint does not accept an explicit user ID list and does not return
a truncation flag. Its `limit` selects globally ranked active users. The adapter
must consequently filter every result through the already authorized request
set and treat an exactly full result as possibly truncated.

## Goals

1. Replace the Pod-local per-user trend result cache with a Redis-backed cache
   shared across Pods and authorized callers.
2. Key reusable trend primitives by provider configuration, Relay user, and
   normalized range rather than by the requesting actor.
3. Let successful personal usage origins write the same reduced trend values
   that Team Usage reads.
4. Use Sub2API's existing users-trend endpoint to resolve multiple Redis misses
   with one upstream request.
5. Preserve correctness when the batch is truncated, unavailable, malformed,
   or only partially useful by falling back for unresolved users.
6. Preserve the existing Team Usage HTTP contracts, independent response
   caches, authorization boundary, and 24-slot individual-origin limit.
7. Keep Redis reconstructible and fail-open: cache failure may cost latency but
   must not grant access, corrupt data, or make a successful origin fail.

## Non-Goals

- Do not modify Sub2API or require a Sub2API upgrade.
- Do not cache one global users-trend response as a large Redis value.
- Do not cache user directory results, batch usage-stat responses, emails,
  usernames, credentials, tokens, or raw Sub2API payloads.
- Do not change Team Usage response DTOs, cursor contracts, response-cache keys,
  fresh/stale windows, or stale-if-error behavior.
- Do not make Redis an authorization, mutation, credential, or quota source of
  truth.
- Do not migrate embedded frontend representations, Relay client objects,
  OAuth protocol state, or process-local coordination primitives in this PR.
- Do not add a second Pod-local trend result cache in front of Redis.
- Do not increase the provider-wide individual trend origin limit beyond 24.

## Current Pod-Local State Inventory

The following long-lived process state exists in the current backend:

| State | Purpose | Decision |
| --- | --- | --- |
| `relay.teamTrendCache` | Successful per-user Team Usage trend results for 60 seconds, bounded to 4096 entries per provider instance | Remove and replace with Redis |
| `web.frontendServer.files` and lazy gzip bytes | Immutable representations embedded in the running binary | Keep process-local |
| `relayruntime.Manager.clients` | Reusable HTTP provider/client objects, bounded by provider version and a five-minute lifetime | Keep process-local |
| `readcache.FlightGroup` instances | Cancellation-aware in-flight coordination with no completed result retention | Keep process-local |
| Team trend 24-slot limiter | Bounds concurrent individual Sub2API origins per provider instance | Keep process-local |
| `usersetup` group credential locks | Serializes concurrent create operations inside one process | Keep as coordination, not a result cache |
| Directory Sync `runningSources` | Prevents duplicate scheduler work inside one process | Keep as coordination, not a result cache |
| OAuth authorization and device-code maps | Temporary protocol state rather than a performance cache | Record as a separate multi-replica concern |

Personal Usage, the four Team Usage response lanes, representative scope,
repository inventory, Work Item counts, and Relay display metadata already use
Redis for reconstructible values. Their local `FlightGroup` fields coordinate
ongoing work only and do not retain completed business results.

## Ownership And Construction

The primitive cache belongs in `backend/internal/relay` because both direct
`TeamMemberTrendProvider` reads and `TeamUsageSummaryProvider` complete-range
fallbacks already pass through the Sub2API adapter. No handler or Team Usage
service receives the cache directly.

`relayruntime.Manager` creates a Sub2API provider with a private trend-cache
configuration containing:

- the shared Redis store;
- deployment namespace;
- provider ID;
- persisted provider configuration version;
- cache metrics.

The existing simple Sub2API constructor remains usable by tests and legacy
single-provider call sites without enabling the Redis primitive cache. The
production DB-backed provider factory must supply the Redis configuration.
Provider construction must not create an in-memory fallback cache when the
Redis configuration is absent or unhealthy.

`backend/internal/readcache` adds a narrow optional `MultiStore` capability for
ordered `MGET` and pipelined per-key `SET` operations. Each set item carries its
own TTL. It composes with the existing token-protected lease methods. Adding
bulk methods must not weaken or replace the existing `readcache.Store` contract
used by unrelated caches. Production Relay construction requires both
capabilities; simple provider construction without them remains uncached.

## Cache Identity And Value

Each value is identified by this canonical tuple:

```text
deployment namespace
cache schema version
provider ID
provider configuration version
Relay user ID
normalized start_date
normalized end_date
normalized granularity
normalized timezone
```

String dimensions use trimmed values. Granularity is lowercased. Team Usage
already defaults an empty timezone to `UTC` before reaching the provider. The
canonical tuple is hashed before it becomes a Redis key. The actor, local user
ID, representative scope hash, personal binding version, email, and username
are intentionally absent.

The versioned JSON envelope contains only:

```text
schema_version
provider_id
provider_configuration_version
relay_user_id
start_date
end_date
granularity
timezone
generated_at
points[]: date, actual_cost, optional total_tokens
```

The decoder rejects unknown schemas, dimension mismatches, invalid user IDs,
malformed points, a `generated_at` in the future, and a value generated 60
seconds or more before the current read. Redis TTL is therefore not the only
freshness guard. A rejected value is a miss. Cache reads and returned maps
defensively clone point slices and token pointers.

Successful values, including proven empty trends, receive an exact 60-second
Redis TTL. The primitive cache has no stale-if-error mode. Team Usage response
caches remain responsible for their existing stale response behavior.

## Authorization And Data Isolation

Authorization is resolved before any Team Usage trend request reaches the Relay
adapter. The adapter accepts only the authorized Relay IDs supplied by its
caller, deduplicates them, and returns only those IDs.

The Sub2API batch payload may contain users outside that set because the
endpoint is globally ranked. The adapter filters rows to the requested Relay
IDs before building cache entries or return values. It never stores batch email
or username fields. A Redis hit does not authorize a user; it can be consumed
only after the caller has independently supplied that Relay ID through the
existing authorization flow.

A personal usage origin may write a primitive only after Relay login succeeds
and the authenticated Relay user matches the requested positive Relay user ID.
No login, password, session token, quota, model, or full dashboard value enters
the primitive cache.

## Team Usage Read Flow

`GetUsageTrendForUsers` follows this sequence:

1. Reject caller cancellation, normalize parameters, discard invalid duplicate
   work, and preserve the current behavior for non-positive Relay IDs.
2. Build keys for unique positive requested IDs and read them with one `MGET`.
3. Decode each value independently. Fresh valid hits immediately satisfy those
   users. A malformed entry affects only its own user and becomes a miss.
4. If no misses remain, return cloned results without a Sub2API origin.
5. If exactly one miss remains, use the existing individual trend origin under
   the provider-wide 24-slot limiter, then cache a successful result.
6. If two or more misses remain, attempt the distributed batch flow.
7. After the batch, resolve only still-missing users through the individual
   fallback and the same 24-slot limiter.
8. Stop before starting another origin whenever the caller is canceled.

If any required individual fallback fails, the method retains its current
error contract: it cancels sibling individual work and returns the error rather
than constructing a partial successful map. Valid Redis and batch entries may
remain cached for later callers, but they do not turn the failed call into a
partial success.

The batch threshold of two avoids downloading a large global response to
resolve one user. The batch-limit contract is:

```text
min(max(total requested unique positive Relay IDs + 250, 500), 5000)
```

The limit uses the total requested set rather than only current Redis misses,
because globally low-ranked requested users still require sufficient ranking
headroom. The lower bound works for small teams, the fixed headroom covers the
current Relay population with margin, and the hard maximum prevents an
unbounded response. The adapter sends `start_date`, `end_date`, `granularity`,
`timezone`, and `limit`. Sub2API's shared time-range parser applies timezone
even though the endpoint's comment does not list that query parameter.

## Batch Decoding And Truncation

Private Relay adapter DTOs decode only fields needed for this feature. For each
requested user returned by the batch, all dated rows are projected to
`UsageTrendPoint` and sorted consistently with the individual endpoint. The
batch row's `tokens` field is copied into a fresh `TotalTokens` pointer; its
`actual_cost` field maps directly to `ActualCost`. Email, username, request
count, and gross cost are neither retained nor logged. A duplicate
`(user_id, date)` row makes the batch invalid rather than being silently
overwritten or double-counted.

The number of unique user IDs in the full decoded batch determines completeness:

- Fewer unique users than `limit`: the global result is proven untruncated for
  the selected range. Requested IDs absent from the result have a successful
  empty trend and may be cached.
- Exactly `limit` unique users: the result may be truncated. Returned requested
  users are valid and may be cached, but absent requested IDs remain unresolved
  and must use the individual fallback.
- More than `limit` unique users, invalid IDs, conflicting duplicate points,
  an unsuccessful envelope, or malformed JSON: treat the batch as invalid and
  use individual fallback for all unresolved users.

Transport failure, non-2xx status, and decode failure do not poison the cache.
Already valid Redis hits remain usable. Batch failure is not returned if every
unresolved user subsequently succeeds through individual fallback.

## Cross-Pod Collapse

Before a batch origin, the adapter acquires a Redis lease whose identity
includes provider/version, normalized range, chosen limit, and a digest of the
sorted missing Relay IDs. This collapses identical cold work from the four Team
Usage lanes and from different Pods.

The Redis command timeout is 100 milliseconds, matching the existing read-cache
default. `MGET` retries at most once after a non-cancellation Redis command
error, matching the bounded GET retry contract; nil elements are ordinary
misses and are not retried. Pipelined writes, lease commands, and releases are
not retried.

The batch origin receives at most 12 seconds. Its lease lives for 15 seconds,
and waiters poll every 25 milliseconds. These bounds fit inside the existing
20-second Team Usage trend origin deadline and preserve time for individual
fallback after a failed batch. A shorter caller deadline always wins.

The lease holder double-checks `MGET`, calls the batch only for remaining
misses, writes valid per-user entries with a pipeline, and releases the
token-protected lease. A waiter polls `MGET` while the lease exists. If the
lease disappears without satisfying every miss, it competes for the next lease
only when its caller still has time; otherwise it proceeds directly to the
individual fallback. Waiting and lease acquisition remain bounded by the
caller context and the existing Team Usage origin deadline.

If Redis commands fail, the adapter records the error and fails open to the
batch or individual origin. Multiple Pods may then duplicate work, but no Pod
retains a completed result as a fallback. An unavailable Redis performance
layer cannot turn valid origin data into an HTTP failure.

Process-local `FlightGroup` may remain around ongoing work to avoid duplicate
goroutines inside one provider instance. It retains no completed trend value
and is not a Pod-level data cache. The Redis lease is the cross-Pod collapse
mechanism.

## Personal Usage Write-Through

`ReadUserUsageOrigin` continues to load stats, trend, and models under one Relay
login and request deadline. After the authenticated user check, a successful
trend branch is projected from `UserUsageTrendPoint` to the shared primitive
shape and written under the requested provider/version/user/range key. Each
integer `TotalTokens` value is copied into the primitive's optional token
pointer.

The write is permitted even if an independent stats or models branch later
fails, because the trend branch is itself a complete successful origin. A
canceled request does not write. Redis write failure is observed but does not
replace the personal origin result with an error.

The existing actor-and-binding-specific Personal Usage Redis dashboard cache
is unchanged. The primitive is a reduced write-through projection from a real
origin load, not a replacement for the personal dashboard cache. A personal
dashboard response-cache hit does not require a new Relay call merely to
backfill this primitive.

## Provider Changes And Expiry

Provider ID and persisted configuration version isolate cache generations.
Changing the provider URL, admin API key, or other persisted configuration
increments that version, so new providers cannot read old entries. Old keys
expire naturally within 60 seconds; explicit key scans or deletions are not
required.

The legacy mutable `SetAdminAPIKey` path uses the simple provider constructor
without this production Redis cache. Removing `teamTrendCache` also removes its
Pod-local credential generation and invalidation logic. `SetModel` remains
irrelevant to usage trend identity.

## Metrics And Logging

The primitive cache uses the existing cache metrics family with a distinct
`relay_user_trend` cache name. Events must distinguish at least:

```text
fresh
miss
malformed
write
error
lease_acquired
lease_wait
batch_origin
individual_fallback
possible_truncation
personal_write_through
```

Batch logs may contain provider ID, requested/hit/miss/resolved counts, limit,
duration, cache event, and error class. They must not contain Relay ID lists,
email, username, credentials, tokens, or raw response bodies.

## Verification

Focused tests must prove:

1. Keys isolate provider, configuration version, Relay user, range,
   granularity, and timezone while remaining actor-independent.
2. `MGET` handles all-hit, partial-hit, miss, malformed-value, and Redis-error
   cases without cross-user contamination.
3. One miss uses the individual origin; two misses prefer the batch.
4. The batch limit observes its lower bound, headroom, and hard maximum.
5. An untruncated batch caches requested results and proven empty users.
6. A possibly truncated batch caches returned requested users and individually
   loads only unresolved users.
7. Batch transport, status, envelope, and decode failures fall back without
   caching errors or synthesized empty values.
8. Cancellation while reading, waiting for a lease, running a batch, or waiting
   for an individual slot starts no later fallback and writes no canceled value.
9. Two independently constructed provider/cache instances sharing Redis and an
   identical request produce one batch origin through the distributed lease.
10. Redis failure may duplicate origins but never creates a Pod-local completed
    result cache or changes a successful origin into a failure.
11. Personal usage writes only after authenticated Relay identity matches and
    projects only date, actual cost, and total tokens.
12. A personal-origin write is reusable by a later Team Usage caller for the
    same provider/version/user/range.
13. Returned slices and optional token pointers are independent copies.
14. Provider version changes make old entries unreachable.
15. Existing Sub2API adapter, Personal Usage, Team Usage, handler, Redis-store,
    full backend, and race tests continue to pass.

## Local Verification

Locally verified on `perf/team-usage-batch-trend` on 2026-07-20 with these
exact commands:

```bash
cd backend
gofmt -w $(rg --files internal/readcache internal/relay internal/telemetry cmd/server | rg '\.go$')
cd ..
git diff --check
rg -n 'teamTrendCache|teamTrendCacheCapacity|teamTrendCacheTTL' backend/internal/relay
rg -n 'email|username|password|token' backend/internal/relay/sub2api_team_trend_redis.go backend/internal/relay/sub2api_team_trend_batch.go

cd backend
go test ./internal/readcache ./internal/relay ./internal/relayruntime ./internal/personalusage ./internal/teamusage ./cmd/server -count=1
go test ./... -count=1
go test -race ./internal/readcache ./internal/relay ./internal/personalusage ./internal/teamusage -count=1
```

Formatting produced no source diff and `git diff --check` was silent. The old
Pod-cache symbol scan returned no matches. The privacy scan matched only the
upstream batch DTO's numeric `tokens` field, internal `TotalTokens` projections,
and the random token used for token-protected lease ownership and release. It
found no email, username, or password field in either implementation file, and
none of the permitted token-count or lease-token values is serialized into a
log. All focused packages, the complete backend suite, and all four race-test
packages passed; no environment-sensitive local verification gap remains.

## Staging Acceptance

Use the same 251-member representative scope with 235 Relay-linked members and
start Summary, Trend, Members, and Organization concurrently. Delete only this
feature's Redis keys and the four Team Usage response keys before the cold run.

The change is accepted when:

- complete cold page data is ready within 9 seconds;
- all four warm requests complete within 1.5 seconds;
- an untruncated cold range produces at most one users-trend batch and zero
  individual user-trend GETs;
- total Relay calls fall from approximately 255 to at most 30;
- member counts remain 251 total and 235 Relay-linked;
- selected-user dates, actual costs, token totals, and aggregate responses match
  the individual-origin behavior;
- Relay 429, 5xx, transport-error, and timeout counts remain zero;
- cache, Redis, and lease error counters do not regress; and
- production remains on its existing image and configuration.

If cold readiness still exceeds nine seconds, record per-lane wall time, batch
wall time, Relay dependency counts, Redis events, and response correctness.
Stop before introducing a global batch snapshot, a longer primitive TTL, or an
additional cache layer; those require a separate design based on the evidence.

## Delivery Boundary

Implementation is one performance PR targeting
`feat/platform-loading-performance`. It includes the Redis bulk-store support,
Relay primitive cache, Sub2API batch adapter, personal write-through, tests,
metrics, and current architecture update. It does not modify Sub2API, frontend
behavior, Team Usage HTTP contracts, or unrelated Pod-local state.
