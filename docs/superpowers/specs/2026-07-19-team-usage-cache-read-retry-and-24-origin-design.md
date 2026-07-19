# Team Usage Cache Read Retry And 24-Origin Follow-Up Design

**Status:** Approved; implementation pending on `perf/team-usage-cache-read-retry-24`

**Refines:**

- `docs/superpowers/specs/2026-07-19-team-usage-cold-loading-design.md`
- `docs/superpowers/specs/2026-07-19-team-usage-shared-trend-cache-design.md`

## Problem

The sixteen-origin staging release preserved every Team Usage response
contract and completed all 255 Relay requests without a 429, 5xx, transport
error, or timeout. It did not meet the latency gate. The two 251-member,
235-Relay-member cold rounds completed in 13.47 and 10.73 seconds against a
nine-second target. The same rounds accumulated 80.727 and 65.074 seconds of
Relay dependency time.

The cold path still contains 235 per-user trend GET requests, eight paginated
user-directory GET requests, and twelve batch usage-stat POST requests. The
sixteen-slot change increased useful trend parallelism but intentionally did
not reduce that request count. The successful upstream status distribution
provides evidence to test a separately reviewed provider-wide limit of 24,
still below the HTTP transport's 50-connection per-host limit.

Immediate warm reads also failed their separate contract. The first warm
round completed in 7.69 seconds with all four response lanes reporting `miss`
and 20 Relay calls. The second completed in 5.99 seconds with Members reporting
`miss` and five Relay calls. Cache error counters increased once for Summary,
Trend, and Organization and twice for Members while Redis pool stale
connections increased by two. Redis timeout, pool-wait, and cache-lease failure
counters did not increase.

`readcache.RedisStore.Get` currently returns the first Redis command error.
The Team Usage cache then follows its fail-open path: it loads authoritative
data with cache writing disabled and returns `miss`. A transient failed read
therefore turns an otherwise warm request into a Relay-backed refresh and does
not repair the response cache during that request.

## Goals

1. Let one transient Redis GET failure recover inside the existing caller
   deadline before a reconstructible read model falls back authoritatively.
2. Keep Redis miss, cancellation, command-budget, lease, write, and mutation
   behavior unchanged.
3. Raise the useful provider-wide Team Usage trend origin limit from sixteen
   to 24 without creating per-caller or credential-generation capacity pools.
4. Re-run the same two cold/warm staging comparisons and retain the existing
   cold and warm acceptance thresholds.
5. Preserve Team Usage HTTP DTOs, cache keys, cache lifetimes, provider
   interfaces, and production isolation.

## Non-Goals

- Do not modify Sub2API or require a new upstream endpoint.
- Do not enable go-redis command retries globally.
- Do not retry Redis SET, SETNX, PTTL, Lua, lease, publish, or mutation
  commands.
- Do not add backoff, configuration, a new retry package, or unbounded retry.
- Do not add Relay user-directory or batch usage-stat primitive caches.
- Do not change the 60-second per-user trend cache TTL or 4096-entry bound.
- Do not change Team Usage response-cache keys, schemas, 144-162 second fresh
  window, 240-270 second stale deadline, or stale-if-error behavior.
- Do not change frontend code or Team Usage endpoint contracts.
- Do not increase the trend limit beyond 24 in this change.
- Do not deploy production as part of this change.

## Bounded Redis GET Retry

`backend/internal/readcache.RedisStore.Get` performs at most two Redis GET
attempts for one logical read. Both attempts use the exact context supplied by
the caller. Team Usage and the other current read-model callers already wrap
commands in their bounded command contexts; the retry neither resets nor
extends that budget.

The first attempt follows these rules:

1. A successful value returns immediately.
2. `redis.Nil` maps to `readcache.ErrMiss` immediately and is not retried.
3. An error accompanied by a canceled or expired context returns immediately
   and is not retried.
4. Any other command error receives one immediate second GET attempt with no
   sleep or backoff.

The second attempt is terminal. A successful value or `redis.Nil` uses the
normal value/miss contract; any error is returned to the caller. The store
does not classify transport error strings because go-redis may surface stale
connection replacement through several wrapped network errors. GET is
idempotent, and the shared caller budget bounds the broader single-retry rule.

This behavior intentionally applies to every reconstructible read model that
uses `readcache.RedisStore.Get`, including Team Usage, personal usage,
representative scope, provider metadata, work-item counts, and repository
inventory. It does not change the other `readcache.Store` methods. The runtime
go-redis client retains `MaxRetries = -1`, so no write or lease command gains
an implicit library retry.

If the retry succeeds, callers observe a normal cache value and do not record
a cache error or authoritative refresh. If it fails, existing fail-open,
stale-if-error, and metrics behavior remains unchanged. No new metric interface
or logging surface is added in this change.

## Provider-Wide 24-Origin Limit

`maxConcurrentTeamTrendOrigins` changes from sixteen to 24.
`GetUsageTrendForUsers` continues to size its caller worker count from that
constant, while the provider-owned `teamTrendOriginLimiter` remains the final
authority immediately before each actual HTTP trend origin.

All existing safety properties remain required:

1. One large caller can fill all 24 slots.
2. Concurrent disjoint callers share one 24-slot provider maximum.
3. Cache hits and same-key flight waiters consume no origin slot.
4. Cancellation before waiting or during slot handoff cannot start an origin.
5. Credential invalidation separates cache generations but never replaces the
   limiter or doubles capacity.
6. Invalid-ID bypass origins remain subject to the same provider limit.

The limit stays below the current 50-connection per-host transport ceiling,
leaving 26 connections of nominal headroom for directory, batch-stat, health,
login, and unrelated Relay traffic. This is a fixed internal limit, not a
runtime or administrator setting.

## Testing

Implementation follows RED/GREEN TDD with synthetic data only.

Redis store tests must prove:

- a first command error followed by a value performs exactly two GET attempts
  and returns the value;
- a first command error followed by `redis.Nil` returns `readcache.ErrMiss`;
- two command errors return the second error after exactly two attempts;
- a canceled or expired context never starts a second attempt;
- an ordinary miss and an ordinary success still perform one attempt;
- SET and all lease methods retain their current one-command behavior.

Relay tests must prove:

- one caller starts 24 actual origins before any is released;
- two disjoint callers never exceed 24 active origins in total;
- cancellation, credential-generation, cache-hit, and shared-flight behavior
  retain their existing contracts with the new bound;
- the relevant tests no longer encode sixteen in their names or assertions.

The complete `internal/readcache`, `internal/relay`, and `internal/teamusage`
suites, focused race checks, full backend tests, vet, and build must pass. No
frontend test expectation changes because neither source nor HTTP contracts
change; repository CI remains the complete merge gate.

## Staging Acceptance

After review and merge into `feat/platform-loading-performance`, publish the
exact merge commit as an immutable multi-architecture staging image and use
the existing two-phase production-snapshot restore rollout.

Repeat two cold/warm audits with the same 251-member and 235-Relay-member
scope, date range, granularity, timezone, four concurrent endpoints, request
limits, request IDs, and dependency aggregation used for revisions 28 and 30.
Wait long enough between cold rounds for the unchanged 60-second trend cache
and the 144-162 second response-cache fresh windows to expire.

Acceptance requires:

- Summary, Trend, Members, and Organization retain HTTP 200, member counts,
  Relay-member counts, and existing availability contracts;
- each cold round completes in at most nine seconds;
- each cold round emits no more than 255 Relay dependency calls;
- Relay 429, 5xx, transport-error, and timeout counts remain zero;
- each immediate warm round completes in at most 1.5 seconds with zero Relay
  calls;
- Team Usage cache error, Redis timeout/wait, and lease-failure counters do not
  increase;
- production image, readiness, and Helm revision remain unchanged.

If the target still fails, record the exact evidence and stop. Do not increase
the limit beyond 24, weaken cache correctness, or relax the acceptance gate.

## Rollback

The two runtime changes are independently revertible. Reverting the Redis GET
commit restores first-error fallback behavior without changing cache data.
Reverting the Relay commit restores the sixteen-slot bound without changing
keys, values, provider interfaces, or response contracts. Staging can also
roll back to the prior immutable revision-30 image. No data migration or cache
flush is required.

## Alternatives Rejected

### Enable Global go-redis Retries

Changing `redis.Options.MaxRetries` is smaller code but affects writes, leases,
Lua scripts, and unrelated Redis operations. That scope is unnecessary for the
observed read failure and weakens the existing bounded-command contract.

### Add Directory And Batch-Stat Primitive Caches

Those caches could remove a small minority of the 255 cold calls, but the 235
per-user trend origins remain the critical path. They require new identities,
lifetimes, invalidation, memory bounds, and correctness tests without strong
evidence that they can meet the nine-second target. They remain a separate
evidence-driven follow-up rather than part of this fix.
