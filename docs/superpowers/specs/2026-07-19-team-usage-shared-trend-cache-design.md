# Team Usage Shared Trend Origin Cache Design

**Status:** Approved for implementation on a branch based on `feat/platform-loading-performance@b03cf0a8`

**Refines:** `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`

## Problem

The split Team Usage endpoints correctly own independent response caches and
loading states, but their cold origins still repeat the same lower-level Relay
trend reads.

On staging, a representative scope with 251 members and 235 Relay-linked
members produced 607 to 629 Relay requests for one concurrent Team Overview
load. Summary, Members, and Organization request complete selected-window
totals. When the Relay batch stats response lacks either selected-window cost
or token totals, the `ai-efficiency` Sub2API adapter fills those fields from
per-user trend reads. Trend independently requests the same per-user points.
The four response cache lanes therefore remain correctly isolated while still
duplicating their most expensive origin primitive.

The normal warm response path is fast, but the response-cache fresh window is
only 48 to 54 seconds. Adjacent refreshes and navigations can therefore repeat
the same per-user reads even when the provider configuration and requested
range have not changed.

## Goals

1. Collapse concurrent reads for the same Relay user and normalized trend
   range inside one `ai-efficiency` Relay provider instance.
2. Reuse successful per-user trend results for 60 seconds.
3. Preserve the independent Summary, Trend, Members, and Organization response
   cache lanes and their existing HTTP contracts.
4. Bound memory and ensure provider credential or configuration changes cannot
   retain reusable entries unexpectedly.
5. Keep failures, cancellations, authorization, and provider errors visible to
   the current caller rather than caching them.

## Non-Goals

- Do not modify Sub2API or require a new upstream endpoint.
- Do not restore a shared Team Overview response or compatibility-origin cache.
- Do not change endpoint response-cache freshness, stale-if-error, Redis key,
  cursor, DTO, or HTTP status contracts.
- Do not add a cross-Pod Redis cache in this change.
- Do not cache Relay errors, values synthesized because of an error, or
  canceled operations.
- Do not change the current maximum of eight workers in each
  `GetUsageTrendForUsers` caller.
- Do not address Redis read retry behavior in this change; that remains a
  separate follow-up.

## Design

### Ownership

The cache belongs to one `sub2apiRelay` provider instance. This is the narrowest
boundary through which both direct `TeamMemberTrendProvider` reads and
`TeamUsageSummaryProvider` complete-range fallback reads already pass.

`relayruntime.Manager` creates provider instances per provider ID and
configuration version and retains them for at most five minutes. A new
provider instance therefore starts with an empty trend cache. The legacy
in-place `SetAdminAPIKey` path must also clear the cache when the normalized key
actually changes.

No endpoint service receives the cache directly. Summary, Trend, Members, and
Organization continue to construct and cache their own typed read models.

### Cache Identity

Each entry is identified by the comparable tuple:

```text
relay_user_id
normalized start_date
normalized end_date
normalized granularity
normalized timezone
```

All string dimensions use the same `strings.TrimSpace` normalization already
applied to the outbound trend query. Provider identity and configuration
version are implicit in the owning provider instance. `relay_user_id` must be
positive; invalid IDs continue through the existing request behavior and are
not stored.

### Lifetime And Capacity

- A successful result is fresh for exactly 60 seconds from origin completion.
- The cache holds at most 4096 entries per provider instance.
- Reads remove an addressed expired entry before treating it as a miss.
- Writes prune all expired entries first. If the cache is still at capacity,
  they evict the entry with the earliest expiration time before inserting the
  new value.
- Provider recreation, configuration invalidation, or a changed admin API key
  discards all entries.
- Each provider maintains a monotonically increasing credential generation.
  Flight identity includes that generation, and a flight may store its result
  only when its captured generation is still current.

This bound covers multiple large-team ranges without allowing unbounded growth.
It intentionally does not implement LRU bookkeeping because all entries share
the same short TTL and the provider itself has a five-minute maximum lifetime.

### Concurrent Read Flow

`GetUsageTrendForUsers` keeps its existing eight-worker outer contract. Each
worker calls a cached per-user reader:

1. Normalize and validate the cache key.
2. Return a cloned value when a fresh entry exists.
3. On a miss, enter `readcache.FlightGroup` using a deterministic key string
   that includes the current credential generation.
4. The flight leader calls the existing `getTeamMemberTrend` origin.
5. Store only a successful result, including a successful empty slice.
6. Return a clone to every waiter.

Each shared flight has a 20-second load timeout, matching the existing Team
Usage trend request budget. When one waiter is canceled, the shared load
continues for remaining waiters; when all waiters leave, the existing
`FlightGroup` cancellation behavior cancels the origin.

The cache and flight map are process-local. This removes duplicate work inside
the current single-Pod staging and production deployment while keeping the
change independent of Redis health. A later multi-replica requirement may add
a distributed primitive cache without changing endpoint contracts.

### Data Isolation

Cached `[]relay.UsageTrendPoint` values are never returned by reference. Store
and read paths copy the slice and clone each optional `TotalTokens` pointer.
Callers may therefore sort, aggregate, or otherwise transform their result
without mutating data observed by another endpoint.

Only successful origin results are reusable. Errors are returned unchanged and
are not written. A later successful request can retry immediately. The adapter
does not convert an error into an empty successful result.

### Credential Changes

`SetAdminAPIKey` compares normalized old and new keys under the existing
credential mutex. When the value changes, it releases that mutex and clears the
trend cache under the cache mutex while incrementing the credential generation.
An older in-flight request cannot populate the new generation, and a new
request cannot join an older generation's flight. Reapplying the same
normalized key does not clear fresh data or change the generation. `SetModel`
does not invalidate this cache because Team Usage trend reads are independent
of the inference model.

## Compatibility

This refinement does not change the Relay provider interfaces:

- `TeamUsageSummaryProvider.GetBatchUserUsageStats` retains its current
  complete-range fallback semantics.
- `TeamMemberTrendProvider.GetUsageTrendForUsers` retains its current response
  and error contract.
- Direct user usage dashboards and selected-member dashboards keep their
  existing paths and are not served by this cache.

The one-release `/api/v1/user/team-usage/overview` compatibility adapter remains
unchanged and continues to consume the four typed read lanes.

## Verification

Focused tests must prove:

1. Two concurrent callers for the same user and normalized range produce one
   upstream trend request and receive independent equal results.
2. A sequential read within 60 seconds reuses the successful result.
3. User ID, date range, granularity, and timezone differences do not collide.
4. An entry refreshes after 60 seconds using an injected deterministic clock.
5. Origin errors and canceled calls are not cached.
6. A successful empty trend is cached.
7. Mutating one returned slice or `TotalTokens` value cannot affect another
   caller.
8. A changed admin API key clears entries, separates new flights, and rejects
   old-generation writes, while reapplying the same normalized key does not.
9. Expired entries are pruned and the cache never exceeds 4096 entries.
10. Existing Sub2API relay, Team Usage service, handler, race, and full backend
    tests continue to pass.

Staging verification must repeat the same 251-member, 235-Relay-member
concurrent Summary, Trend, Members, and Organization audit. It must compare
cold and warm wall time, response/cache contracts, per-request dependency
counts, total dependency duration, and Redis/cache error counters against the
`b03cf0a8` staging baseline. Production remains unchanged until the integration
branch is reviewed and explicitly released.

## Delivery Boundary

This change is delivered as one small PR targeting
`feat/platform-loading-performance`. The Redis GET retry remediation is a
separate design and PR so either behavior can be reviewed, deployed, and rolled
back independently.
