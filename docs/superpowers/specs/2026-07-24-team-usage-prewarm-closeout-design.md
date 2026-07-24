# Team Usage Prewarm Closeout Design

**Status:** Accepted for merge with the feature disabled by default. User-visible
performance work is closed. Multi-replica enablement remains blocked by issue
[#194](https://github.com/LichKing-2234/ai-efficiency/issues/194).

**Date:** 2026-07-24

**Refines:**

- `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`
- `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`

## Decision

PR #193 is complete for merge. The representative user-visible merge gate is a
fresh Google Chrome navigation to `/usage/team` for the default 30-day
Asia/Shanghai window, with browser resources and the four typed response caches
cold while valid Redis prewarm values remain available.

The accepted gate is:

- HTTP 200 and matching business data;
- four typed response-cache misses and four prewarm `full_hit` outcomes;
- fully rendered page completion at or below eight seconds; and
- immediate warm API lanes below 1.5 seconds.

The eight-second value is a merge gate for the measured representative staging
scenario. It is not a production p95 SLO and must not be presented as one.
Historical five-second API-lane requirements remain part of the design record
but no longer block merging this default-disabled feature.

The five-second startup manifest-cohort spread is a separate lifecycle
correctness constraint and is not relaxed by this decision.

## Accepted Evidence

The exact PR image ran in staging with two replicas and four complete prewarm
manifests. A fresh temporary profile from installed Google Chrome
`150.0.7871.182` requested `2026-06-25` through `2026-07-24` with `day`
granularity and `Asia/Shanghai` timezone.

| Signal | Result |
| --- | ---: |
| TTFB | 0.157s |
| DOMContentLoaded | 0.745s |
| FCP | 1.228s |
| LCP | 6.336s |
| Summary complete | 5.516s |
| Members complete | 6.300s |
| Trend complete | 7.627s |
| Organization complete | 7.627s |
| Fully rendered | 7.796s |

Metrics recorded four response-cache misses, zero fresh hits, four prewarm
`full_hit` outcomes, and zero new Redis or Relay error deltas. The immediate
warm API round completed every lane below 1.5 seconds with matching business
hashes. Staging was restored to one replica with prewarm disabled, and
production remained unchanged.

## Deferred Boundary

Issue #194 owns the unresolved multi-Pod lifecycle work. Two-Pod observations
showed scheduled ticks and Relay traffic overlapping page measurements, and
some earlier guarded replays recorded Redis `command_deadline` errors. Request-
time Relay isolation is therefore not proven.

These gaps do not block merging because prewarm remains disabled by default.
They do block enabling prewarm on a multi-replica deployment. The follow-up must
remain Redis-coordinated, must not add a completed Pod cache, and must not modify
Sub2API or raise the accepted 250ms Redis budgets merely to hide contention.

## Integration And Documentation

- Convert PR #193 from draft to ready after this closeout is committed and CI
  passes.
- Keep its dependency base until the existing PR sequence is merged or
  retargeted deliberately.
- Do not enable staging or production prewarm as part of this closeout.
- Do not update `docs/architecture.md`; the optional runtime remains inactive.
- Leave historical failed checkboxes intact. Their work is explicitly closed
  incomplete and deferred to #194 rather than silently marked successful.
- Do not implement the proposed startup completion marker in PR #193.
