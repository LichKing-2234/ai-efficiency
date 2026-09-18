# Team Usage Performance Experiments

> Historical evidence. The rejected mechanisms and measurements in this file
> are not current runtime contracts. Current behavior is owned by
> [Team Usage prewarm](../contracts/team-usage-prewarm.md) and
> [platform loading](../contracts/platform-loading.md).

This record preserves the experiments that explained why Team Usage moved from
request-time per-user fan-out to a separate stateless prewarm worker. It omits
branch ledgers, implementation checklists, and repeated delivery narration.

## Decision Question

The split Summary, Trend, Members, and Organization endpoints had correct
authorization and independent response states, but a cold page repeated costly
Relay trend work. The decision problem was not simply "add more cache". Each
candidate had to reduce cold/warm latency while preserving:

- four independent typed response lanes and their HTTP contracts;
- complete authorized scope and stable member counts;
- authorization before any shared Relay projection;
- exact authoritative fallback when Redis/shared data was absent or invalid;
- bounded Relay/Redis work with no errors hidden as successful empty data;
- no `sub2api` source or direct-database change.

## Measurement Frame and Limits

The first experiments used one staging representative scope with 251 members,
235 of whom had Relay mappings. The common request window was
`2026-06-20..2026-07-19`, daily granularity, `Asia/Shanghai`, with Summary,
Trend, Members (`limit=50`), and Organization
(`department_limit=25`, `member_limit=50`) requested concurrently.

A cold round ran after relevant TTLs/keys could not be reused; an immediate
warm round followed it. Evidence recorded wall time, lane status/cache state,
member counts, Relay request count/status/duration, and cache/Redis errors. The
request-time experiments used these selection gates:

```text
cold complete <= 9.0s
each warm lane <= 1.5s
warm Relay calls = 0
Relay 429/5xx/transport/timeout = 0
cache/lease/Redis errors = 0
cold/warm business values and counts agree
```

The later three-candidate matrix observed 252 members and 236 Relay mappings
from its then-current restored snapshot. Candidate comparisons therefore use
the scope recorded for that matrix; the one-member drift is not a performance
effect. All results are staging observations from one workload and network
period, not universal service objectives or production capacity claims.

## Request-Time Experiments

### Per-User Shared Trend Origin

**Hypothesis:** A provider-local 60-second, 4096-entry per-user trend cache plus
singleflight would collapse the same user/range origins requested by all four
lanes while leaving their response caches independent.

**Observed:** The synthetic four-lane proof reduced 940 origins to 235. In the
two staging cold rounds, total Relay calls fell from the 607-629 baseline to 255
and cumulative Relay time fell from 113.6-124.4 seconds to 67.48/60.33 seconds.
Cold page completion remained 13.69/12.34 seconds. Immediate warm rounds were
1.14/1.05 seconds with zero Relay calls and no cache/Redis errors.

**Decision:** Keep the evidence that duplicate fan-out was real and collapsible,
but reject the completed Pod-local cache as a final architecture. It reduced
work without removing the 235-user cold critical path and did not provide a
cross-Pod source of truth.

Durable source:
[design](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/specs/2026-07-19-team-usage-shared-trend-cache-design.md),
[evidence plan](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md).

### Sixteen Provider-Wide Origins and Longer Response Freshness

**Hypothesis:** Raising useful provider-wide trend concurrency from eight to
sixteen and extending response-cache freshness to 144-162 seconds would shorten
cold fan-out and make normal revisits warm.

**Observed:** Cold rounds completed in 13.47/10.73 seconds with 255 successful
Relay calls and 80.727/65.074 seconds of aggregate dependency time. Immediate
warm rounds took 7.69/5.99 seconds instead of <=1.5 seconds. The first warm
round made 20 Relay calls; the second made five when Members missed. Cache-read
errors increased for all lanes (twice for Members), and Redis stale-connection
errors increased by two; Relay itself returned no 429, 5xx, transport error, or
timeout.

**Failed gates:** both cold rounds, both warm rounds, dependency-free warm, and
cache/Redis error stability.

**Decision:** More concurrency did not reduce the number of source calls.
Longer response freshness could not compensate for a transient Redis read that
fell back authoritatively without repairing the response cache.

Durable source:
[design](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/specs/2026-07-19-team-usage-cold-loading-design.md),
[evidence plan](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md).

### Twenty-Four Origins and One Redis GET Retry

**Hypothesis:** One immediate idempotent Redis GET retry inside the existing
context would preserve warm hits, while increasing the same provider-wide
origin limiter from sixteen to 24 would meet the cold target without exceeding
the 50-connection transport bound.

**Observed:** After a measured 300-second quiet window, the first cold round
completed in 12.363568 seconds. Lane times were 11.580609, 12.363568,
11.887330, and 11.563641 seconds. It made exactly 255 Relay calls, all 2xx, with
101.907 seconds of aggregate dependency time. Counts remained 251/235 and all
cache/Redis error, wait, timeout, stale-connection, and lease-failure deltas
were zero.

The immediate warm round completed in 1.222148 seconds; lane times were
0.751889, 1.222148, 0.951021, and 0.896057 seconds, all fresh with zero Relay
calls. The experiment stopped before round two because the cold <=9-second gate
had already failed.

**Decision:** Retain the bounded Redis GET retry as a generally useful
read-model behavior. Do not increase origin concurrency beyond 24: the cold
critical path remained source-work bound even when the warm/correctness gates
passed.

Durable source:
[design](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md),
[evidence plan](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md).

## Three-Candidate Matrix

The matrix removed the completed Pod-local trend cache and moved range
completion above stats chunks. Every candidate used the aggregate Relay
`users-trend` route over the complete authorized scope, retained typed response
caches, and ran once without staging tuning.

| Candidate | Hypothesis | Cold result | Immediate warm result | Relay/cache evidence | Verdict |
| --- | --- | ---: | ---: | --- | --- |
| A: upstream aggregate cache only | Rely on the upstream 30-second cache/singleflight; keep no AI Efficiency primitive cache. | slowest lane 18.521s | slowest lane 0.854s | cold 14 GET + 12 POST; four aggregate trend requests; warm Relay 0; no errors | Rejected: cold gate only. |
| B: Redis per-user primitives | One full-request lease and per-user Redis values enable cross-Pod/overlapping-scope reuse. | slowest lane 21.722s | slowest lane 7.995s | cold 11 GET + 12 POST; warm 7 GET + 9 POST; three lane cache errors | Rejected: cold, warm, warm-Relay, and error gates. |
| C: Redis scope-origin snapshot | One scope/version/range Redis origin lets four lanes project from one bounded shared generation. | slowest lane 14.214s | slowest lane 1.122s | cold 5 GET + 3 POST; one aggregate trend; warm Relay 0; one origin miss/lease/refresh; no errors | Best reference, but rejected by cold gate. |

All candidates preserved counts/range aggregates and used no individual trend
GET. Candidate B's independently recomputed floating-point summary differed
only at `1e-10`; this was not its rejection reason. Candidate C passed every
gate except cold completion and reduced Relay calls most, so it became the
measured baseline for the next design. No candidate was promoted, and staging
was restored to the 24-origin baseline.

Durable source:
[matrix design](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/specs/2026-07-20-team-usage-experiment-matrix-design.md),
[matrix evidence plan](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-07-20-team-usage-experiment-matrix.md).

## Why the Stateless Prewarmer Won

The matrix showed that even one shared scope origin was too slow when its
source work began on the browser's cold request. The selected design kept that
scope-origin/fallback composition but moved provider-wide source refresh out of
the request path:

- one optional worker publishes immutable, publish-last generations through
  Redis;
- backend Pods remain authorization-first, read-only, and exact-fallback
  capable;
- worker scheduling/restart is independent of backend replica count/readiness;
- Redis contains reconstructible usage facts, never authorization.

The acceptance question changed from the unattained request-time 9-second gate
to an end-to-end browser gate: three fresh-profile Chrome runs, fully-rendered
median <=8 seconds, each immediate warm API lane <1.5 seconds, exact typed-cache
miss/prewarm-hit counts, and business-value equality.

The measured Chrome runs were:

| Run | Fully rendered | Summary | Trend | Members | Organization | Warm max |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 7720ms | 4767ms | 5578ms | 5060ms | 5581ms | 470ms |
| 2 | 6295ms | 4385ms | 4110ms | 4931ms | 4542ms | 398ms |
| 3 | 7021ms | 5286ms | 4389ms | 5265ms | 4400ms | 461ms |

Median fully rendered was 7021ms. Every run returned HTTP 200, produced four
typed response-cache misses and four prewarm full hits, retained matching
same-generation business hashes, and kept immediate warm lanes below 500ms.

Operational evidence also showed:

- backend scaling `2 -> 1 -> 2` left the one worker and its refresh cadence
  unchanged;
- deleting the worker Pod produced an immediate stateless refresh that reused
  hard-valid Redis history and published new current/today lanes;
- scaling the worker to zero through manifest expiry left backend
  liveness/readiness healthy and selected exact request-time fallback;
- restored prewarm/fallback responses had identical structure/cardinality and
  stable fields. Four current/today-derived usage leaves changed across the
  source-sampling interval, so live comparison was sampling-aware while the
  same-source automated equivalence remained strict.

The first measured image did not expose the required backend prewarm-request
metric, so no performance run was claimed until that evidence gap was fixed.
This is an admission lesson: an unobservable required gate is a blocked test,
not a pass inferred from apparently correct responses.

The accepted implementation and exact staging evidence are preserved in the
[prewarmer evidence plan](https://github.com/LichKing-2234/ai-efficiency/blob/7355cab6efcbac8a69c5319bcc848e1bfe0d20f9/docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md).
Its integration is independently visible in
[PR #193](https://github.com/LichKing-2234/ai-efficiency/pull/193), the complete
loading stack in [PR #160](https://github.com/LichKing-2234/ai-efficiency/pull/160),
and platform release
[`v0.1.0-preview.74`](https://github.com/LichKing-2234/ai-efficiency/releases/tag/v0.1.0-preview.74).

## Resulting Decision

The retained decision is architectural, not a promise that one staging timing
will hold for every team:

1. Collapse duplicated domain work, but do not mistake higher concurrency for
   less work.
2. Keep request-time caches reconstructible and fail open to exact authoritative
   reads.
3. Move expensive provider-wide refresh off the browser cold path when its
   source latency cannot meet the product gate.
4. Keep worker state reconstructible from Redis/provider facts; backend replica
   count must not create schedulers.
5. Preserve authorization-first projection and exact fallback regardless of
   optimization availability.
6. Select performance mechanisms with measured user-visible behavior and
   explicit observability gates, not Relay-call count alone.

Current worker topology, schema, freshness, publication, and fallback rules are
intentionally absent here; they belong only to the current neutral contracts.
