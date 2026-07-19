# Team Usage Cold Loading Design

**Status:** Implemented through `perf/team-usage-cold-loading@2147142a`; independent re-review and full local verification are complete, while exact-head PR CI and staging A/B remain pending

**Refines:**

- `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- `docs/superpowers/specs/2026-07-19-team-usage-shared-trend-cache-design.md`

## Problem

Staging revision 28 on integration commit
`b1e8ed566dd6837acc0aac32d7b29cda8a40c8f3` correctly collapsed duplicate
per-user trend origins across the four split Team Usage lanes. Two comparable
251-member, 235-Relay-member cold audits reduced Relay dependency calls from
607-629 to 255 and cumulative dependency time by 41-52 percent. All responses
remained available and warm page completion stayed near one second.

Cold page completion nevertheless remained 12.34-13.69 seconds. Each cold
round contained 243 GET requests and 12 POST requests. Current code and the
request shape account for those calls as:

- 235 per-user trend GET requests;
- eight paginated Relay user-directory GET requests, two for each split lane;
- twelve batch usage-stat POST requests, three 100-user chunks for each lane.

The critical path is still the 235-user trend fan-out. Its caller uses eight
workers. The four lanes request the same ordered user/range set and share
per-user flights, so they normally advance through the same eight origin slots
rather than multiplying useful concurrency. Separately, the Team Usage
response caches become soft-expired after only 48-54 seconds, which makes a
normal revisit pay the cold path again roughly once per minute.

## Goals

1. Raise useful per-user trend origin concurrency from eight to sixteen.
2. Bound that concurrency across the entire Relay provider instance, including
   concurrent callers, ranges, actors, and credential generations.
3. Increase all four Team Usage response-cache fresh windows from a
   one-minute pre-jitter maximum to a three-minute pre-jitter maximum.
4. Preserve the existing response contracts, stale-if-error behavior, cache
   isolation, credential invalidation, per-user trend cache, and upstream call
   count.
5. Validate the change on the same staging scope before considering a higher
   concurrency limit.

## Non-Goals

- Do not modify Sub2API or add a new upstream endpoint.
- Do not add Relay user-directory or batch-stat primitive caches.
- Do not increase concurrency beyond sixteen in this change.
- Do not add an environment setting or administrator control for concurrency.
- Do not change frontend loading states, endpoint DTOs, Redis key identity,
  stale-if-error eligibility, or cursor contracts.
- Do not change the per-user trend cache's 60-second TTL or 4096-entry bound.
- Do not change the Team Usage response-cache maximum stale age.
- Do not address Redis GET retry behavior.
- Do not deploy production as part of this change.

## Provider-Wide Trend Origin Limit

`sub2apiRelay` owns one zero-value-safe trend origin limiter with a fixed
capacity of sixteen. `GetUsageTrendForUsers` also starts at most sixteen
workers per caller so one large caller can fill the provider capacity.

Every cache-miss loader passed to `teamTrendCache.GetOrLoad` acquires the
provider limiter immediately before `getTeamMemberTrend` and releases it after
the HTTP origin finishes. The limiter therefore has these semantics:

1. A fresh per-user cache hit does not acquire a slot.
2. Waiters sharing an existing per-user flight do not acquire additional
   slots; only the flight leader reaches the origin loader.
3. Disjoint users, ranges, actors, and credential generations share the same
   sixteen slots within one provider instance.
4. A caller canceled while waiting for a slot returns its context error and
   never starts an origin. The limiter checks cancellation before waiting and
   again after slot acquisition so a simultaneous cancellation and slot
   handoff cannot start the HTTP loader.
5. Credential invalidation clears cached trend values and separates flights,
   but it does not replace the limiter or temporarily double origin capacity.
6. Invalid non-positive Relay user IDs keep their existing cache/flight bypass,
   but their direct origin loader remains subject to the provider-wide limit.

The limiter is internal to the Sub2API adapter. Relay provider interfaces and
Team Usage callers do not receive new configuration or methods.

Sixteen is deliberately below the runtime HTTP client's 50-connection
per-host maximum. This leaves headroom for health checks, batch stats, login,
and unrelated Relay operations. A future increase to 24 requires separate
staging evidence and is not an automatic fallback if this design misses its
latency target.

## Three-Minute Fresh Window

The four Team Usage response caches continue to use the existing 10-20 percent
jitter. Their fresh lifetime changes from:

```text
one minute minus 10-20 percent = 48-54 seconds
```

to:

```text
three minutes minus 10-20 percent = 144-162 seconds
```

The maximum stale lifetime remains five minutes before jitter, or 240-270
seconds after generation. `generated_at`, `fresh_until`, and `stale_until`
continue to describe the actual response snapshot generation. The Redis hard
TTL still ends at `stale_until`.

At or before `fresh_until`, the cache returns `cache_status=fresh`. After
`fresh_until`, the current synchronous collapsed refresh and eligible
stale-if-error behavior remain unchanged. This design does not serve stale
data while a successful refresh is still running, does not extend hidden
per-user trend freshness, and does not claim a newly generated `as_of` time for
old origin data.

The writer emits only the new 144-162 second fresh window. To preserve an
eligible stale fallback across an application upgrade, the reader also accepts
same-schema historical envelopes whose actual fresh window is 48-54 seconds
and whose stale window remains 240-270 seconds. Their stored `fresh_until` and
`stale_until` remain authoritative, so compatibility neither refreshes nor
extends old values. All other fresh-window ranges remain invalid, and Redis
keys and payload schema versions do not change.

The three-minute window is scoped only to Summary, Trend, Members, and
Organization read models in `backend/internal/teamusage`. Personal usage,
representative scope, work-item, provider metadata, and other caches retain
their current lifetimes.

## Error And Cancellation Behavior

- The existing 20-second shared per-user trend load timeout remains in force.
- Slot wait time is included in that timeout.
- Origin errors, cancellations, and values synthesized because of an error are
  not cached.
- A canceled waiter does not cancel an origin that still has another waiter.
- When the last waiter leaves, the existing flight cancellation path cancels a
  loader waiting for a limiter slot or an active HTTP request.
- No retry, backoff, or 429-specific behavior is added.

## Verification

Focused tests must prove:

1. One 32-user caller reaches sixteen concurrent origins before release and
   never exceeds sixteen.
2. Two concurrent callers with disjoint user/range keys collectively never
   exceed sixteen active origins, proving the bound is provider-wide rather
   than per-caller.
3. Two callers sharing user/range keys still collapse to one origin per user
   while using no more than sixteen slots.
4. Cancellation while waiting for a limiter slot, including cancellation at
   slot handoff, returns promptly and does not start an extra origin.
5. Credential invalidation does not create a second limiter capacity pool.
6. The minimum-jitter Team Usage fresh window is 162 seconds and the
   maximum-jitter window is 144 seconds.
7. Same-schema historical 48-54 second envelopes remain readable through
   their original stale deadline, while new writes use only 144-162 seconds.
8. Existing 240-270 second stale bounds, Redis hard TTL, cache statuses,
   response contracts, and race tests remain unchanged.

The complete backend test, vet, build, and race gates must pass. No frontend
test expectation should change because the HTTP contract is unchanged.

## Staging Acceptance

After review and merge into `feat/platform-loading-performance`, publish an
immutable multi-architecture staging image and repeat the same two cold/warm
audits for 251 members and 235 Relay-linked members over
`2026-06-20..2026-07-19`, `day`, and `Asia/Shanghai`.

Acceptance requires:

- Summary, Trend, Members limit 50, and Organization limits 25/50 all return
  HTTP 200 with their current availability and count contracts;
- each of two cold rounds completes in at most nine seconds;
- each cold round emits no more than 255 Relay dependency calls;
- Relay 429, 5xx, transport-error, and timeout counts remain zero;
- immediate warm page completion remains at most 1.5 seconds with zero Relay
  calls;
- cache and Redis error counters do not regress;
- production image, readiness, and Helm revision remain unchanged.

If sixteen concurrent origins do not meet the nine-second target, this ticket
records the evidence and stops. It does not silently increase the limit to 24
or weaken correctness/freshness contracts.

## Documentation And Delivery

Implementation updates `docs/architecture.md` to replace the current
eight-worker and 48-54-second descriptions with the provider-wide sixteen-slot
limit and 144-162-second fresh window. The historical 2026-07-19 shared-trend
cache spec remains unchanged; this spec records the new active refinement.

Delivery is one small PR from `perf/team-usage-cold-loading` to
`feat/platform-loading-performance`. Production remains unchanged until a
separate explicit release decision.
