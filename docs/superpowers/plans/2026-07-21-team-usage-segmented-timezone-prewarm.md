# Team Usage Segmented Timezone Prewarm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the Relay directory, current-stats, and aggregate-trend chain from eligible cold Team Usage requests by prewarming exact timezone-specific source segments into Redis while preserving PR #192 response and authorization behavior.

**Architecture:** A background `teamusage.Prewarmer` resolves the current primary Relay provider, fetches one provider-wide current-stats roster and three immutable trend segments per configured timezone, then publishes a manifest last. Request-time code first resolves the current representative scope and Relay mappings, reads only a matching provider/version/timezone/anchor generation, intersects it with the authorized Relay IDs, and otherwise executes PR #192's exact scope-origin fallback.

**Tech Stack:** Go 1.24, Gin, Ent, go-redis v9, miniredis, Prometheus client_golang, zap, Sub2API HTTP APIs, Docker Buildx, GHCR, Helm, Kubernetes.

**Status:** Tasks 1-3 are complete. Task 3 review corrections reject the
environment-dependent `Local` timezone and check every usage composition
addition; implementation `d9e0c4d2` passed focused, race, full-package, and vet
verification. Task 4 RED tests failed for the expected missing batch-store and
prewarm-cache symbols. The first implementation passed focused and race tests,
but review found four binding gaps: write-once values, per-reference partial
status, earliest-reference manifest expiry, and the two-second timeout cap.
Correction RED tests failed first for the missing partial-result API and then
for all four behaviors. A later formal review found three additional binding
gaps in trend-only generation accounting, fresh-clock publication margin, and
identical concurrent immutable claims. Their RED tests failed as expected. The
correction implementation passes focused, race, full-package, vet, and diff
verification and is committed in `3354ee41`; the final review ledger is
committed in `fd983263` and Task 4 review is clean. Task 5 implementation and
ledger commits `06338e20` and `014fecef`
passed their original verification. Formal review corrections for schedule
coordinators, worker lifetimes, source-slot release, lifecycle state, and
startup recovery now pass focused, race, full-package, vet, and diff checks;
the corrections are committed in `b1b84ef2`. The second formal Task 5 review
corrections for rollover bootstrap, atomic multi-lease publication, an
adjacent-tick active moving lease, and allowlist-scoped historical coordination
pass focused, race, full-package, readcache, backend compile, vet, and diff
checks. The implementation is committed in `6d2f15dd`. The third formal Task 5
review corrections for timezone-isolated moving preflight, an independent
provider-wide rollover recovery cycle, and strict multi-lease claim boundary
coverage pass focused, repeated, race, full-package, readcache, backend compile,
vet, and diff checks. The implementation is committed in `20677980`; the final
review ledger is committed in `4fed6345` and Task 5 review is clean. Task 6
authorized-reader integration is complete.
Task 7 disabled runtime, configuration, lifecycle, Redis transport, and bounded
telemetry wiring passed focused, full-backend, race, vet, build, compose-config,
and diff verification and is committed in `90943307`. Formal Task 7 review
corrections move all optional Redis/provider initialization behind HTTP startup,
atomically install the reader, join initialization before Redis close, complete
bounded validation/cache/quantity/generation metrics, classify partial-today
Redis failures separately from Relay failures, and report discarded lifecycle
failures without raw errors. The correction passed focused, full-backend, race,
vet, build, compose-config, and diff verification and is committed in `8f86642f`.
Formal Task 7 re-review corrections now record the validated provider-wide trend
union before authorization, preserve exact lifecycle lane/class outcomes, map
the locked six Relay rejection kinds without string parsing, and update the full
generation gauge only for one explicitly registered participating-lane batch.
Two follow-up RED/GREEN waves additionally bound global failures to selected
lanes, prevent post-lane duplicate cycle metrics, capture one preflight instant,
and reject late same-current-ID manifests whose anchors do not match the active
batch. The same reviewer approved the final diff with zero Critical, Important,
or Minor findings. Final focused, full-backend, race, vet, build, compose-config,
and diff checks passed; implementation is committed in `ab7cea64`.
Task 8 is complete: the exact Dashboard RED run failed for the
expected missing prewarm cycle-duration panel, then the bounded prewarm panels
and operations guidance made the same command pass. Final local verification
also passes after a controller-authorized test-only correction removed three
redundant parallel `gin.SetMode` writes exposed by the required race gate. The
final Standards and Spec reviewers approved the corrected immutable package
with zero findings; its behavior and documentation commits are `506bd403` and
`667f4032`.
Task 9 Step 1 is complete: the exact reviewed image is deployed only to staging
at revision 46 with the feature disabled, and production remains unchanged.
Task 9 Step 2 is complete after Task 13 replaced the blocked schema-v1 result
with schema-v2 zstd values and request-window reads. Exact budget-code commit
`759b1946` passed the final feature-disabled benchmark with 100 valid samples
and zero errors in every required class; read, write, lease, and release budgets
are locked at `250ms`. Step 3 was attempted and failed; Steps 3-6 are incomplete
and remain unchecked. Tasks 10 and 11 implemented schema-v2 zstd immutable
values and request-window-scoped Redis reads in commits `2761ecaf` and
`0f0ba03a`. Task 12 implementation and review are complete. The first review
corrections are
committed in `4bf72e9b`; the exhaustive selected-reference validation correction
is committed in `10d1e8ea`. The final independent review approved exact head
`10d1e8ea` with zero Critical, Important, or Minor findings after both
correction ladders passed. Task 13 is complete. Task 9 Step 3 was attempted on
2026-07-22 at staging revision 51 with two replicas, but failed the zero-Redis-
error and one-logical-cycle gates. The hard failure was established before
eight bounded Summary probes were run. Running those probes instead of stopping
was a deviation from the fail-fast contract; Step 4 did not start, and rollback
followed the probes. Staging is healthy and disabled at revision 52, production
is unchanged, Steps 3-6 are incomplete and remain unchecked, and
`docs/architecture.md` remains unchanged.
Task 14 root-cause analysis is complete. Retained evidence cannot identify the
Redis error subtype or attribute aggregate source observations to startup versus
the first scheduled tick. Task 14 therefore adds bounded diagnostic telemetry
and closes the confirmed startup coordinator contract gaps before any new
feature-enabled attempt. Task 14 Steps 1-3 are complete in commits `666e1e94`
and `16529b18`. Focused RED/GREEN tests, 50 repeated multi-instance runs,
server, Team Usage, and telemetry race tests, the full backend suite,
`go vet ./...`, and `go build ./...` passed. Independent task review found zero
Critical, Important, or Minor issues. Step 4's exact-code feature-disabled
benchmark passed. Step 5 then proved one startup owner and one lease-busy loser,
but the four-manifest cohort exceeded five seconds and two closed
`command_deadline` Redis failures occurred. The pre-ticker gate was not proven.
Staging was restored disabled at revision 56; the rollback duration was not
retained separately. Production is unchanged, and Task 9 Steps 3-6 remain
unchecked.

**Task 15 supersession note (2026-07-23):**
`docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`
and
`docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md`
supersede only Task 14's startup fetch ordering, cohort publication, and direct
scheduler-tick evidence contract. The Task 14 failure and rollback evidence
below remains historical evidence and is not rewritten. The exact locally
reviewed Task 15 code head is
`30279888db6dad6c0f5e433879ba9573642fc461`; no Task 15 image has been built or
deployed and no Task 15 staging replay has run. Staging remains
feature-disabled according to the retained Task 14 evidence. Task 15 Tasks 4-5
remain pending, and Task 9 Steps 3-6 remain unchanged and unchecked.

**Task 16 guarded replay decision (2026-07-23):** The one separately authorized
replacement replay completed one staging enable at revision 61 and one guardian
rollback at revision 62. Its feed/observer window overlapped restoration and the
controller transiently misclassified rollback convergence, so independent
review classified the run as an operational failure rather than a product pass
or product-gate failure. Final staging is exact-image disabled `1/1` and healthy,
production revision 69 is unchanged, and local/Pod Task 16 artifact counts are
zero. Task 9 Steps 3-6 remain unchecked, `docs/architecture.md` remains
unchanged, and no third replay is authorized.

**Task 9 HTTP acceptance preflight (2026-07-24):** One later, separately
user-authorized attempt enabled the Task 15 exact image only in staging at
revision 65 with two ready replicas and the four approved timezones. Before
login, cache deletion, or any Team Usage API request, its metrics contained
three Redis `command_deadline` errors and four cycle errors. The fail-fast gate
therefore stopped the attempt without producing cold/warm timings, `full_hit`,
payload hashes, or request-time Relay evidence. The guardian restored healthy
exact-image disabled staging revision 66 at `1/1`; production revision 69
remained unchanged. Task 9 Steps 3-6 remain unchecked and
`docs/architecture.md` remains unchanged.

**Task 9 latency-only observation (2026-07-24):** The user separately
authorized one current-image staging measurement after the failed preflight.
All four cold lanes returned HTTP 200 and `full_hit`, but Summary/Organization/
Trend/Members took `4.939/5.139/5.956/7.344s`; three failed the five-second
bound. All four immediate warm lanes passed at `0.512/0.519/0.785/0.863s`, with
matching business hashes. Background ticks made the Relay delta
non-attributable. The detailed sanitized ledger is in the superseding Task 15
plan. The guardian restored disabled staging revision 72 at `1/1`; production
revision 69 remained unchanged. Task 9 Steps 3-6 remain unchecked.

## Task 1 Gate Evidence

Staging matched PR #192 exact head `627a7123` at Helm revision 44 using image
`ghcr.io/lichking-2234/ai-efficiency:staging-627a7123d98aee37dd04fd5da2198234cfd003f0`.
Production remained unchanged on `v0.1.0-preview.73` at revision 69. The read-only probe
used completed split-safe anchor `2026-07-19`, made exactly 20 trend GETs with
maximum concurrency two. The final retained-ledger rerun completed in 51.576
seconds; its complete 20-call table is in the authoritative design spec's POC
Evidence section.

| Gate | Observed | Result |
| --- | ---: | --- |
| Slowest GET `<25s` | 6.657 s | Pass |
| All 20 GETs `<5m` | 51.576 s | Pass |
| Largest body `<32 MiB` | 1,022,906 bytes | Pass |
| Largest decoded result `<1,000,000` | 6,308 points | Pass |
| Largest source/composed user set `<5,000` | 359 / 359 | Pass |
| Largest stored segment `<8 MiB` | 470,926 bytes | Pass |
| Largest timezone generation `<16 MiB` | 696,323 bytes | Pass |
| All 12 trend segments `<64 MiB` | 2,621,040 bytes | Pass |
| Synthetic directory `<16 MiB` | 6,445,098 bytes | Pass |
| Synthetic stats chunk `<2 MiB` | 88,549 bytes | Pass |
| Synthetic current stats `<2 MiB` | 774,970 bytes | Pass |
| Synthetic manifest `<64 KiB` | 3,798 bytes | Pass |
| Peak RSS `<192 MiB` | 33,243,136 bytes | Pass |
| 7d/30d token and cost equality | exact in all four timezones | Pass |
| DST split guard fixtures | four rollover rejects and eight adjacent accepts | Pass |

## Global Constraints

- The authoritative design is `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`.
- Retain PR #192 exact head `627a7123` as the complete-range fallback behavior.
- Do not modify Sub2API source or require a coordinated Sub2API release.
- Do not add a Pod-local completed-result cache; process-local state may coordinate only an in-flight operation.
- Redis is optional and fail-open. It never authorizes a request and never becomes the source of truth for usage.
- Resolve the current actor, representative scope, Relay mappings, enabled primary provider row, provider configuration version, and final scope version on every request.
- Preserve existing Summary, Trend, Members, Organization, and Overview DTOs, cursor signatures, response-cache keys, freshness windows, and stale-if-error behavior.
- Prewarm only exact today/hour, 7d/day, and 30d/day windows anchored to the current local date of a configured timezone.
- Initial timezones are exactly `UTC`, `Asia/Shanghai`, `America/Los_Angeles`, and `Europe/Berlin`; normalize, validate, deduplicate, preserve first occurrence order, and reject more than four.
- Preserve Relay source labels. Never parse a returned label into a timezone-aware instant, reinterpret it as UTC, or convert it between timezones.
- Reject a source at exactly 5,000 unique users, 1,000,000 decoded points, or any strict byte bound; authorization filtering must occur after completeness and truncation validation.
- A missing row in a complete, non-truncated trend source is zero contribution. A currently authorized Relay ID missing from the complete directory/current-stats roster selects exact fallback.
- Directory requests use `page_size=1000`, `include_subscriptions=false`, `sort_by=id`, and `sort_order=asc`; current stats use chunks no larger than 500.
- Redis values contain no name, username, email, credential, API key, JWT, raw response, Directory record, representative assignment, scope, or authorization decision.
- Use only synthetic identities and example domains in tests and committed evidence. Keep credentials, raw bodies, user lists, temporary probes, and raw Redis values under `/tmp` and delete them before committing.
- Production image, configuration, Helm revision, and traffic remain unchanged throughout this plan. Staging enablement and production release require separate approvals.
- Update this plan's checkbox immediately after each completed step. Do not mark environment-sensitive checks complete without running them.

## File Map

- `backend/internal/relay/provider.go`: optional provider-wide directory, current-stats, and trend capability interfaces.
- `backend/internal/relay/types.go`: identity-free provider-wide result and metadata types.
- `backend/internal/relay/sub2api.go`: bounded deterministic directory pagination and exact current-stats decoding.
- `backend/internal/relay/sub2api_team_trend_batch.go`: raw provider-wide aggregate trend result before authorization filtering.
- `backend/internal/readcache/store.go`: ordered multi-get and token-checked manifest publication primitives implemented by `RedisStore`.
- `backend/internal/teamusage/prewarm_model.go`: timezone normalization, anchor recognition, DST guard, source validation, coalescing, composition, and size constants.
- `backend/internal/teamusage/prewarm_cache.go`: versioned keys, immutable envelopes, publish-last manifests, bounded reads, and leases.
- `backend/internal/teamusage/prewarm_source.go`: complete roster/current-stats generation and exact segment fetch orchestration.
- `backend/internal/teamusage/prewarmer.go`: startup recovery, moving cycle, daily historical correction, jitter, cancellation, and concurrency control.
- `backend/internal/teamusage/prewarm_reader.go`: authorized request-time projection, partial-today recovery, and fallback outcomes.
- `backend/internal/teamusage/origin.go`, `service.go`, `organization.go`: prewarm-first scope-origin loading with the retained PR #192 loader.
- `backend/internal/config/config.go`: disabled-by-default flag, timezone allowlist, and validated configuration.
- `backend/cmd/server/main.go`, `cache_metrics.go`: Redis pool bounds, dependencies, lifecycle, and metric recorders.
- `backend/internal/handler/router.go`, `team_usage.go`: pass the optional prewarm reader into the existing Team Usage service.
- `backend/internal/telemetry/team_usage_prewarm.go`: bounded-cardinality metrics.
- `deploy/config.example.yaml`, `deploy/docker-compose*.yml`, `deploy/README.md`: disabled defaults and operator configuration.
- `deploy/observability/grafana/ai-efficiency-performance.json`, `deploy/observability/README.md`: staging and production observability.
- `docs/architecture.md`: current runtime boundary, updated only after staging enablement makes the implementation active.

---

### Task 1: Run The Segmented-Source POC Hard Gate

**Files:**
- Temporary create and delete: `/tmp/ae-team-usage-segmented-poc.go`
- Temporary create and delete: `/tmp/ae-team-usage-segmented-poc`
- Temporary create and delete: `/tmp/ae-team-usage-segmented-poc-result.json`
- Modify after a sanitized decision exists: `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`
- Modify after a sanitized decision exists: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`

**Interfaces:**
- Consumes: staging Sub2API base URL and admin API key through process environment only.
- Produces: sanitized per-call duration/body/point/user counts, per-timezone composition results, trend-segment sizes, synthetic envelope sizes, peak RSS, and a pass/fail decision.
- Makes exactly 20 read-only trend GETs: four timezones times `history_29d`, `history_6d`, `today_hour`, `direct_30d`, and `direct_7d`, with global concurrency at most two.

- [x] **Step 1: Verify immutable audit targets before reading any credential**

Run:

```bash
gh pr view 192 --json headRefOid,state,mergedAt
kubectl -n la3-ai-efficiency-prod get deploy ai-efficiency-staging -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
helm -n la3-ai-efficiency-prod status ai-efficiency-staging
helm -n la3-ai-efficiency-prod status ai-efficiency-prod
```

Expected: PR #192 reports exact head `627a7123d98aee37dd04fd5da2198234cfd003f0`; record the exact staging image/revision; production has no pending change. Stop if the staging target cannot be tied to the retained baseline. Do not run `helm upgrade`.

- [x] **Step 2: Create the throwaway bounded probe**

The probe must define and use these request shapes and comparison keys:

```go
type segmentRequest struct {
	Timezone, Class, StartDate, EndDate, Granularity string
}
type pointKey struct {
	UserID int64
	Label  string
}
type value struct {
	Tokens *int64
	Cost   float64
}
```

For one completed split-safe anchor `D` per timezone, generate `D-29..D-1/day`, `D-6..D-1/day`, `D..D/hour`, `D-29..D/day`, and `D-6..D/day` with `time.Date` plus `AddDate`, never `N*24h`. Read at most `32<<20` bytes plus one sentinel byte, reject a body at or above the cap, reject invalid/duplicate/out-of-order points, reject non-finite or negative values, and reject unique users at or above 5,000. Use a semaphore of size two and an atomic request counter that must equal 20.

Coalesce only `today_hour` by `point.Date[:10]`, merge by `pointKey`, preserve nil-token propagation, and compare composed versus direct key sets, token presence/value, and `math.Abs(left.Cost-right.Cost) <= 1e-9`. The stored-size simulation serializes raw `today_hour`, not its coalesced projection.

- [x] **Step 3: Add deterministic no-HTTP structural fixtures to the same probe**

Create synthetic fixtures only:

```go
const (
	directoryBodyLimit   = 16 << 20
	statsBodyLimit       = 2 << 20
	currentStatsLimit    = 2 << 20
	manifestLimit        = 64 << 10
	trendSegmentLimit    = 8 << 20
	timezoneTrendLimit   = 16 << 20
	allTimezoneTrendLimit = 64 << 20
)
```

Serialize one 1,000-row maximum-width no-subscription directory page, one 500-row maximum-width stats response, one 4,999-row current-stats envelope, and four maximum-width manifests exactly as specified in the design. Assert every serialized size is strictly below its constant and that all 12 stored trend segments are strictly below 64 MiB in total. These fixtures must not make an HTTP request.

- [x] **Step 4: Run the measured POC and enforce every gate**

Run:

```bash
go build -o /tmp/ae-team-usage-segmented-poc /tmp/ae-team-usage-segmented-poc.go
/usr/bin/time -l env \
  AE_POC_BASE_URL="$AE_POC_BASE_URL" \
  AE_POC_ADMIN_API_KEY="$AE_POC_ADMIN_API_KEY" \
  /tmp/ae-team-usage-segmented-poc \
  > /tmp/ae-team-usage-segmented-poc-result.json \
  2> /tmp/ae-team-usage-segmented-poc-time.txt
jq -e '.request_count == 20 and .max_concurrency <= 2 and .decision == "pass"' \
  /tmp/ae-team-usage-segmented-poc-result.json
```

Expected: each GET `<25s`; total wall `<5m`; each body `<32 MiB`; each decoded result `<1,000,000` points; source and composed users `<5,000`; each segment `<8 MiB`; one timezone `<16 MiB`; all timezones `<64 MiB`; RSS `<192 MiB`; every per-key comparison exact under the token/cost rules; LA/Berlin spring and fall fixture anchors select fallback while adjacent 24-hour anchors pass. Any failure blocks the plan; do not weaken a limit or execute Task 2.

- [x] **Step 5: Record only sanitized evidence and delete authenticated artifacts**

Update the spec and this plan with dates, aggregate counts, sizes, durations, and pass/fail only. Then run:

```bash
rm -f /tmp/ae-team-usage-segmented-poc.go \
  /tmp/ae-team-usage-segmented-poc \
  /tmp/ae-team-usage-segmented-poc-result.json \
  /tmp/ae-team-usage-segmented-poc-time.txt
unset AE_POC_BASE_URL AE_POC_ADMIN_API_KEY
git diff --check
```

Expected: no credential, URL, user ID, row, body, or raw Redis value remains. On pass, change `Status` to `POC passed; implementation ready` and check Task 1. On failure, change it to `Blocked by segmented-source POC`, leave Tasks 2-9 unchecked, and stop.

- [x] **Step 6: Commit the POC decision**

```bash
git add docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): record segmented prewarm POC"
```

---

### Task 2: Add Bounded Provider-Wide Relay Sources

**Files:**
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/internal/relay/sub2api_team_trend_batch.go`
- Modify: `backend/internal/relay/sub2api_team_trend_batch_test.go`

**Interfaces:**
- Produces: `ProviderWideTeamUsageProvider.GetProviderUserIDs`, `GetProviderCurrentUsageStats`, and `ProviderWideTeamTrendProvider.GetProviderUsageTrend`.
- Preserves: `TeamUsageSummaryProvider.GetBatchUserUsageStats` and `TeamMemberTrendProvider.GetUsageTrendForUsers` as PR #192 fallback APIs.

- [x] **Step 1: Write RED interface and HTTP contract tests**

Define the intended signatures in test fakes:

```go
type ProviderWideTeamUsageProvider interface {
	GetProviderUserIDs(context.Context) (ProviderDirectoryResult, error)
	GetProviderCurrentUsageStats(context.Context, []int64) (ProviderCurrentStatsResult, error)
}
type ProviderWideTeamTrendProvider interface {
	GetProviderUsageTrend(context.Context, TeamMemberTrendParams, int) (ProviderWideTrendResult, error)
}
```

Tests must assert the exact directory query, strictly ascending cross-page IDs, stable authoritative pagination, `<5000` IDs, at-most-500 stats IDs, exact stats coverage, duplicate JSON-key rejection, and pre-decode rejection at exactly 16 MiB and 2 MiB. Trend tests must assert raw unfiltered rows, exact coverage metadata, bytes/points/users, negative-cost rejection, exact-5,000 rejection, and fallback API filtering remains unchanged.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/relay -run 'ProviderWide|DirectoryContract|CurrentStatsContract|TeamTrendBatch' -count=1 -v
```

Expected: FAIL because the provider-wide interfaces and bounded source methods do not exist and existing directory/stats reads are unbounded.

- [x] **Step 3: Implement the minimal provider capabilities**

Add identity-free result types:

```go
type ProviderDirectoryResult struct { UserIDs []int64; ResponseBytes int64; PageCount int }
type ProviderCurrentStatsResult struct { Stats map[int64]TeamUserUsageStats; ResponseBytes int64 }
type ProviderWideTrendResult struct {
	Points []ProviderWideTrendPoint
	Coverage TeamMemberTrendParams
	ResponseBytes int64
	PointCount, UniqueUserCount int
	Complete bool
}
type ProviderWideTrendPoint struct { UserID int64; Date string; ActualCost float64; TotalTokens *int64 }
```

Use limited reads with one sentinel byte before JSON decoding. Decode stats object tokens before map conversion so duplicate object keys cannot be hidden. Keep `GetUsageTrendForUsers` as a thin authorization-filtering adapter over the provider-wide result, without a completed in-memory cache.

- [x] **Step 4: Verify Relay behavior**

```bash
cd backend
gofmt -w internal/relay
go test ./internal/relay -count=1
go test -race ./internal/relay -count=1
go vet ./internal/relay
```

Expected: all pass; tests prove no real identity or raw body is logged or returned by the provider-wide interfaces.

- [x] **Step 5: Commit**

```bash
git add backend/internal/relay
git commit -m "perf(relay): add bounded provider-wide usage sources"
```

---

### Task 3: Implement Segment And Timezone Domain Logic

**Files:**
- Create: `backend/internal/teamusage/prewarm_model.go`
- Create: `backend/internal/teamusage/prewarm_model_test.go`

**Interfaces:**
- Produces: `NormalizePrewarmTimezones`, `RecognizePrewarmWindow`, `SplitSafe`, `ValidateTrendSegment`, `ComposePrewarmedOrigin`, and immutable envelope types used by Tasks 4-6.
- Consumes: provider-wide Relay types from Task 2.

- [x] **Step 1: Write RED table tests for configuration, dates, labels, and composition**

Tests must cover the exact four defaults, trim/dedup/order, invalid zone, fifth zone, current local anchors, exact today/7d/30d shapes, custom-range rejection, `AddDate` semantics, LA/Berlin spring/fall rejection, opaque label validation, raw-hour retention, first-10-character coalescing, independent 6d/29d histories, sparse-user zero contribution, nil-token propagation, `1e-9` cost boundary, duplicate/order/coverage validation, and per-source/composed 5,000-user rejection.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1 -v
```

Expected: FAIL with missing prewarm model symbols.

- [x] **Step 3: Implement the pure domain model**

Use explicit classes and coverage:

```go
type PrewarmSegmentClass string
const (
	SegmentHistory29d PrewarmSegmentClass = "history_29d"
	SegmentHistory6d  PrewarmSegmentClass = "history_6d"
	SegmentTodayHour  PrewarmSegmentClass = "today_hour"
)
type PrewarmCoverage struct { StartDate, EndDate, Granularity, Timezone string }
```

`SplitSafe` may parse only local `D-1` and `D` midnights and compare `previous.Add(24*time.Hour).Equal(current)`. Returned Relay labels remain strings. Composition creates a dense `teamUsageScopeOrigin` only after intersecting with authorized IDs; absent complete trend rows become empty point slices, while missing roster stats return an ineligible outcome.

- [x] **Step 4: Verify the pure model and race safety**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_model*.go
go test ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1
go test -race ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1
```

- [x] **Step 5: Commit**

```bash
git add backend/internal/teamusage/prewarm_model.go backend/internal/teamusage/prewarm_model_test.go
git commit -m "perf(teamusage): add segmented prewarm domain model"
```

---

### Task 4: Add Immutable Redis Generations And Atomic Publication

**Files:**
- Modify: `backend/internal/readcache/store.go`
- Modify: `backend/internal/readcache/store_test.go`
- Create: `backend/internal/teamusage/prewarm_cache.go`
- Create: `backend/internal/teamusage/prewarm_cache_test.go`

**Interfaces:**
- Produces: `readcache.BatchStore.MGet` and `SetIfLeaseOwned`; `PrewarmCache.Read`, `WriteCurrentStats`, `WriteSegment`, `PublishManifest`, and lease methods.
- Consumes: immutable model/envelope types from Task 3.

- [x] **Step 1: Write RED Redis primitive and cache tests**

Tests must prove ordered MGET miss positions, token-checked Lua publication, token-checked release, namespace/schema/provider/version/timezone-digest/anchor/class/generation isolation, 512-byte key-reference rejection, immutable write-before-manifest order, reader use of one manifest only, malformed/oversized/hard-expired rejection, old-anchor in-flight readability, and Redis fail-open outcomes.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1 -v
```

Expected: FAIL because batch reads, atomic publish, and prewarm cache do not exist.

- [x] **Step 3: Implement bounded cache primitives and TTL relationships**

Introduce a narrow extension instead of enlarging every existing fake:

```go
type BatchStore interface {
	Store
	MGet(context.Context, ...string) ([][]byte, error)
	SetIfLeaseOwned(context.Context, string, string, string, []byte, time.Duration) (bool, error)
}
```

Use `movingFresh=75s`, `movingHard=4m`, `movingValueTTL=6m`, `historyFresh=25h`, `historyHard=49h`, `historyValueTTL=50h`, and `manifestTTL=3m`. Every reader validates logical hard expiry in addition to Redis TTL. The manifest expires before the earliest moving hard expiry; values outlive manifest discovery plus the 44-second maximum backend request, and prior-anchor moving values remain available long enough for a resolved request to finish.

- [x] **Step 4: Verify Redis behavior**

```bash
cd backend
gofmt -w internal/readcache internal/teamusage/prewarm_cache*.go
go test ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1
go test -race ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1
```

- [x] **Step 5: Commit**

```bash
git add backend/internal/readcache backend/internal/teamusage/prewarm_cache.go backend/internal/teamusage/prewarm_cache_test.go
git commit -m "perf(teamusage): add publish-last Redis generations"
```

---

### Task 5: Implement Provider Generation And Prewarm Lifecycle

**Files:**
- Create: `backend/internal/teamusage/prewarm_source.go`
- Create: `backend/internal/teamusage/prewarm_source_test.go`
- Create: `backend/internal/teamusage/prewarmer.go`
- Create: `backend/internal/teamusage/prewarmer_test.go`

**Interfaces:**
- Produces: `ProviderBinding`, `PrimaryProviderBindingResolver`, `PrewarmSource.BuildCurrentStats`, `FetchSegment`, and `Prewarmer.Start/Stop/RunMoving/RunHistorical`.
- Consumes: Tasks 2-4 provider capabilities, domain model, and cache.

- [x] **Step 1: Write RED source and scheduler tests**

Tests must prove full deterministic directory exhaustion precedes 500-ID stats chunks, exact roster equality/digest, one shared current-stats generation, independent histories, deployment-wide source concurrency two across directory pages, stats chunks, trend segments, and partial-today calls, token-checked coordinator and two slot leases, process-local concurrency two, skipped overlapping 60-second ticks, deterministic `[0,30m)` jitter, split-unsafe no-source behavior, startup recovery of only missing/hard-expired values, stale-but-hard-valid retention, provider-version cancellation, lost-lease no-publish, and one timezone failure not invalidating another.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1 -v
```

Expected: FAIL with missing source and lifecycle types.

- [x] **Step 3: Implement bounded source orchestration**

`BuildCurrentStats` requires the new provider-wide usage capability, rejects 5,000 IDs, chunks at 500, validates exact cross-chunk coverage, sorts by ID, and hashes the length-delimited roster. `FetchSegment` calls the provider-wide trend capability with `limit=5000`, validates before writing, and never filters to a request scope.

- [x] **Step 4: Implement the scheduler and distributed coordination**

Use a 60-second ticker, non-blocking process cycle guard, source semaphore size two, `3m` moving coordinator TTL, `6m` historical coordinator TTL, and `90s` segment/source-slot lease TTL. Expose one `SourceCallLimiter` used by every background and request-driven Relay source call; each call must own one of the same two distributed slot leases and one process-local semaphore position. Generate random lease tokens, re-check token ownership through `SetIfLeaseOwned` before manifest publication, and stop all workers on context cancellation. No goroutine may retain a completed provider generation after publication.

The lifecycle surface is:

```go
type SourceCallLimiter interface {
	Do(context.Context, func(context.Context) error) error
}
func (p *Prewarmer) Start(ctx context.Context)
func (p *Prewarmer) Stop()
func (p *Prewarmer) RunMoving(ctx context.Context) error
func (p *Prewarmer) RunHistorical(ctx context.Context, timezone string, anchor time.Time) error
```

- [x] **Step 5: Verify lifecycle behavior**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_source*.go internal/teamusage/prewarmer*.go
go test ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1
go test -race ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1
```

- [x] **Step 6: Commit**

```bash
git add backend/internal/teamusage/prewarm_source* backend/internal/teamusage/prewarmer*
git commit -m "perf(teamusage): add segmented prewarm lifecycle"
```

---

### Task 6: Integrate Authorized Request Reads And Exact Fallback

**Files:**
- Create: `backend/internal/teamusage/prewarm_reader.go`
- Create: `backend/internal/teamusage/prewarm_reader_test.go`
- Modify: `backend/internal/teamusage/origin.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/organization.go`
- Modify: `backend/internal/teamusage/shared_origin_test.go`
- Modify: `backend/internal/teamusage/range_completion_test.go`

**Interfaces:**
- Produces: `PrewarmReader.ReadAuthorizedOrigin(ctx, PrewarmReadRequest) (*teamUsageScopeOrigin, PrewarmReadOutcome, error)`.
- Preserves: `loadTeamUsageScopeOrigin` as the exact PR #192 source path and `OriginCache` as the outer request-origin cache.

- [x] **Step 1: Write RED request-path tests**

Tests must prove authorization and mapping resolution happen before Redis; full hits make no Relay call; eligible scopes above the existing `fullScopeCap=500` can use provider-wide prewarm facts; custom/unconfigured/DST/provider-version/anchor/schema/Redis failures use the exact existing lane fallback; authorized roster absence falls back; sparse complete trend omission yields zero; current scope-version change discards projection; partial missing today fetches only `D..D/hour` through the shared global source slots and never history; partial Relay failure falls back to the full exact range; Summary/Trend/Members/Organization/Overview outputs, cursors, freshness, and response-cache keys remain byte-equivalent.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmReader|PrewarmFallback|PrewarmPartialToday|SharedOrigin|RangeCompletion' -count=1 -v
```

Expected: FAIL because `Service` has no prewarm reader and current cold loads always use the scope-origin source.

- [x] **Step 3: Implement prewarm-first loading behind the existing origin cache**

Add `PrewarmReader *PrewarmReader` to `ServiceOptions`. In each existing lane loader, resolve current subjects and Relay IDs first and attempt the reader for a recognized eligible window before applying `fullScopeCap`; a valid provider-wide generation may therefore serve an authorized scope larger than 500 but smaller than the provider `<5000` bound. Hydrate a dense origin from authorized IDs, then resolve scope again and compare the opaque version before returning. On miss/ineligibility, preserve the exact PR #192 branch: scopes at or below 500 use `OriginCache` plus `loadTeamUsageScopeOrigin`, while larger scopes use their existing bounded Summary/Trend/Members/Organization generator. Do not change HTTP DTOs or outer caches.

Use this request boundary:

```go
type PrewarmReadRequest struct {
	ProviderID, ActorUserID int
	ProviderVersion         int64
	ScopeVersion            string
	Params                  OverviewParams
	AuthorizedRelayUserIDs  []int64
	Provider                relay.Provider
}
type PrewarmReadOutcome string
func (r *PrewarmReader) ReadAuthorizedOrigin(context.Context, PrewarmReadRequest) (*teamUsageScopeOrigin, PrewarmReadOutcome, error)
```

- [x] **Step 4: Implement partial-today recovery without a completed Pod cache**

When current stats and required history are hard-valid but today is not, coordinate only the in-flight today request, fetch exactly `D..D/hour`, validate and compose it, optionally publish a complete new generation while the lease is owned, then discard the completed local value. If any step fails, call the complete PR #192 loader rather than a per-segment approximation.

The local flight key contains provider ID/version, timezone digest, anchor, and `today_hour`; its callback returns the request result to current waiters only. It never writes the completed result into a process map after the callback returns.

- [x] **Step 5: Verify all Team Usage behavior**

```bash
cd backend
gofmt -w internal/teamusage
go test ./internal/teamusage -count=1
go test -race ./internal/teamusage -count=1
```

- [x] **Step 6: Commit**

```bash
git add backend/internal/teamusage
git commit -m "perf(teamusage): read authorized prewarmed origins"
```

**Formal review correction evidence (2026-07-21):** Final representative-scope
and provider-config re-resolution errors now abort the prewarmed projection and
force a fresh request resolution without executing the stale request's exact
fallback. Request partial-today recovery and lifecycle moving work now share one
`moving` segment-lease dimension and helper, so an owned lifecycle lease sends the
request directly to the complete exact fallback without a concurrent source call.
Explicit exact-versus-prewarm coverage now compares serialized Summary, Trend,
Members, Organization, and Overview responses, Members and Organization cursor
signatures, miss/fresh status, and all four response-cache keys. The required
focused command passed in `3.648s`, full Team Usage passed in `19.077s`, race
passed in `23.387s`, and `go vet ./...` plus `git diff --check` exited zero.

---

### Task 7: Wire Disabled Configuration, Runtime, And Observability

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Create: `backend/internal/telemetry/team_usage_prewarm.go`
- Create: `backend/internal/telemetry/team_usage_prewarm_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go`
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/README.md`

**Interfaces:**
- Produces: `TeamUsagePrewarmConfig{Enabled bool, Timezones []string}` and a lifecycle wired to server shutdown.
- Preserves: existing 100ms cache command contexts while the shared Redis transport uses one-second dial/pool bounds and at least four idle connections.

- [x] **Step 1: Write RED configuration, dependency, lifecycle, and metric tests**

Tests must prove disabled default, exact default list, env binding, invalid-list fail-open behavior, no prewarmer when disabled/empty/Redis unavailable/provider unsupported, startup after dependencies without blocking HTTP, graceful stop before Redis close, `MinIdleConns>=4`, one-second dial/pool bounds, two-second Redis transport read/write ceilings for later measured large-value contexts, existing cache 100ms contexts, and preinitialized closed-enum metric labels with no identity or raw-key label.

  RED tests added on 2026-07-21 cover configuration defaults and environment
  binding, fail-open runtime construction, exact limiter sharing, asynchronous
  lifecycle startup and ordered shutdown, shared Redis transport bounds, optional
  router injection, and preinitialized privacy-safe closed-enum metrics. Step 2
  remains unchecked until the exact focused command is run and its failure is
  captured.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/config ./internal/telemetry ./internal/handler ./cmd/server \
  -run 'TeamUsagePrewarm|RedisClientOptions|CacheMetrics|RouterDependencies' -count=1 -v
```

  RED evidence (2026-07-21): the exact command failed as expected. Config tests
  could not find `Config.TeamUsagePrewarm`; telemetry tests could not find
  `TeamUsagePrewarmRecorder`; server tests could not find the on-demand metrics
  factory, runtime initializer, or config type; and handler tests rejected the
  intended optional reader argument. These failures establish the missing Task
  7 wiring rather than a fixture or syntax failure.

- [x] **Step 3: Implement disabled-by-default wiring**

Bind `AE_TEAM_USAGE_PREWARM_ENABLED` and comma-separated `AE_TEAM_USAGE_PREWARM_TIMEZONES`. Validate through `NormalizePrewarmTimezones`; invalid optimization config logs a bounded error and constructs no reader/prewarmer instead of terminating the core service. Configure the shared go-redis transport with one-second dial/pool timeouts, `MinIdleConns=4`, and two-second read/write ceilings; existing caches keep their 100ms command contexts, while prewarm read/write contexts remain separately configurable for Task 9 measurement. Pass the reader through `RouterOptions` and `ServiceOptions`. Start the prewarmer after provider runtime/cache construction, retain its cancel function, and stop it before Redis shutdown.

```go
type TeamUsagePrewarmConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Timezones []string `mapstructure:"timezones"`
}
```

Defaults are `false` and the four design timezones. Compose examples expose the same values without enabling the feature.

  Implemented on 2026-07-21. Runtime construction returns before creating the
  recorder, cache, reader, or prewarmer when disabled, empty, Redis-unavailable,
  provider-unavailable, provider-unsupported, or invalid. The enabled path uses
  the configured Redis namespace, passes the exact prewarmer limiter into the
  reader, starts after the HTTP server goroutine, and stops before Redis close.
  All compose examples retain `false` as the default.

- [x] **Step 4: Add bounded-cardinality telemetry**

Record cycle/source/Redis durations, sizes, lease/tick outcomes, last-success timestamps, cache outcomes, and request fallback reasons using closed enums. Timezone labels may contain only the validated maximum-four configured values. Background logs contain only operation ID, provider/version, bounded timezone/class/outcome, duration, and counts; user fallbacks retain the request ID.

```go
type PrewarmMetrics interface {
	RecordCycle(class, timezone, outcome string, duration time.Duration)
	RecordSource(class, timezone, outcome string, duration time.Duration, bytes, points, users int)
	RecordRedis(operation, outcome string, duration time.Duration, bytes int)
	RecordRequest(timezone, outcome, fallbackReason string)
	SetLastSuccess(class, timezone string, at time.Time)
}
```

  Implemented on 2026-07-21 with preinitialized closed enums for cycle, source,
  Redis, request, fallback, lease/tick, and last-success evidence. Invalid
  runtime labels are dropped, and timezone series are limited to the normalized
  maximum-four allowlist. The focused Task 7 command and complete config,
  telemetry, handler, and server package tests pass; full backend verification
  remains Step 5.

- [x] **Step 5: Verify configuration and full backend**

```bash
cd backend
gofmt -w internal/config internal/telemetry internal/handler cmd/server
go test ./internal/config ./internal/telemetry ./internal/handler ./cmd/server -count=1
go test ./... -count=1
go test -race ./internal/relay ./internal/readcache ./internal/teamusage ./internal/config ./internal/telemetry ./internal/handler ./cmd/server -count=1
go vet ./...
go build ./...
```

  GREEN evidence (2026-07-21): the exact focused Task 7 command passed; the
  complete config, telemetry, handler, and server packages passed; `go test
  ./... -count=1` passed; the required race set passed with no race reports;
  and `go vet ./...` plus `go build ./...` exited zero. The macOS race linker
  emitted non-fatal `LC_DYSYMTAB` warnings for three test binaries, all of which
  subsequently passed.

- [x] **Step 6: Commit**

```bash
git add backend deploy
git commit -m "perf(backend): wire segmented Team Usage prewarm"
```

  Committed as `90943307f9a323b57a65a4dc92ed3903af9e198f` on 2026-07-21.

**Formal review correction evidence (2026-07-21):** RED tests first failed for
the missing lazy runtime/atomic reader source, bounded background reporter,
quantity/validation/cache metrics, and distinct partial-today failure classes.
The production path now prepares only normalized configuration before router
construction, starts a bounded cancelable initializer after both HTTP goroutines,
and performs Redis ping, provider resolution/capability checks, metric/cache/
prewarmer/reader construction, atomic installation, and lifecycle startup in
that order. Shutdown cancels and joins the initializer and prewarmer before
Redis close. Successful publications record segment, timezone, and full
generation bytes with current stats counted once; source validation and
manifest/current-stats/segment cache outcomes use closed enums. Background
failures emit generated bounded operation IDs and fixed fields without raw
errors. The exact focused Task 7 command, focused Team Usage review tests,
`go test ./... -count=1`, the required race set, `go vet ./...`, `go build ./...`,
all five compose config checks, and `git diff --check` passed. The first race
run exposed a test-recorder slice append race; the fake was synchronized and
both focused and complete race reruns passed. Committed as `8f86642f`.

**Formal re-review correction evidence (2026-07-21):** The first RED wave
proved that `union_users` exposed authorized roster size, one published timezone
updated the full-generation gauge, real Sub2API limit/coverage rejections were
not mapped through a closed typed contract, and aggregate lifecycle reporting
duplicated failures or marked healthy lanes/classes as errors. Follow-up RED
tests proved that a global validation wrapper was downgraded to `error`, startup
did not emit `rejected`, an unknown Relay rejection kind was accepted, and the
old sparse fixture expected an authorization-derived union.

The second RED wave proved that post-preflight shared failures were broadcast to
non-participating lanes, post-lane ownership failures could add a second cycle
outcome after lane defers had already recorded success, preflight read the clock
once per timezone across rollover, and a late old-anchor manifest could satisfy
the same current generation. The correction now uses exact lifecycle failure
targets, keeps the highest non-lifecycle wrapper for bounded global reporting,
marks post-lane failures as already cycle-recorded, and maintains one bounded
active metrics batch containing only provider/version/current ID, at most four
timezone anchors, and byte counts. Publication contributes to the full gauge
only when provider, version, current ID, timezone, and anchor all match that
registered participating-lane batch. It retains no request or usage result.

The exact Relay and Team Usage focused suites, complete Relay/Team Usage
packages, exact Task 7 command, `go test ./... -count=1`, the required
cross-package race set, `go vet ./...`, `go build ./...`, all five Compose config
checks, and `git diff --check` passed. The race run emitted only the existing
non-fatal macOS `LC_DYSYMTAB` linker warnings and reported no race. The same
reviewer approved the final correction with zero Critical, Important, or Minor
findings. Committed as `ab7cea64cef10638282a77b9e36dc4a08b390bee`.

---

### Task 8: Complete Documentation And Branch-Wide Review

**Files:**
- Modify: `deploy/observability/grafana/ai-efficiency-performance.json`
- Modify: `deploy/observability/README.md`
- Modify only after staging enablement in Task 9: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`

**Interfaces:**
- Produces: operator-visible dashboards and a current execution ledger.
- Consumes: metric names and final runtime behavior from Tasks 2-7.

- [x] **Step 1: Add dashboard contract tests before editing JSON**

Extend `backend/internal/telemetry/dashboard_contract_test.go` to require prewarm cycle duration/outcome, last-success, Redis/source duration, generation bytes, request outcome/fallback, and skipped tick/lease panels with bounded label groupings.

  Contract evidence (2026-07-22): the dashboard test now requires separate
  cycle-duration, cycle-outcome, last-success, source-duration, Redis-duration,
  generation-bytes, request-outcome/fallback, skipped-tick, and lease-outcome
  panels. Every aggregation is limited to the implemented closed labels; the
  Grafana JSON is intentionally unchanged before the required RED run.

- [x] **Step 2: Run RED, update dashboard, then run GREEN**

```bash
cd backend
go test ./internal/telemetry -run 'Dashboard' -count=1 -v
```

Expected RED: required prewarm expressions are absent. Add panels and operator queries, then rerun and expect PASS.

  RED evidence (2026-07-22): `go test ./internal/telemetry -run 'Dashboard'
  -count=1 -v` exited 1 after compiling and running the contract. It failed at
  the expected first absent panel, `Team Usage Prewarm Cycle Duration`; there
  were no JSON parse, fixture path, or test compilation errors.

  GREEN evidence (2026-07-22): the dashboard now exposes separate bounded
  cycle-duration/outcome, last-success, source-duration, Redis-duration,
  generation-bytes, request-outcome/fallback, skipped-tick, and lease-outcome
  views. The operations README keeps the feature disabled, documents bounded
  labels and fail-open diagnosis, and requires flag-only rollback without a
  Redis flush. `jq empty` and the exact Dashboard command both exited 0; the
  Dashboard test reported PASS.

- [x] **Step 3: Run final local verification**

```bash
cd backend
go test ./... -count=1
go test -race ./internal/relay ./internal/readcache ./internal/teamusage ./internal/config ./internal/telemetry ./internal/handler ./cmd/server -count=1
go vet ./...
go build ./...
cd ..
git diff --check
git status --short
```

Expected: all commands pass. Report environment-sensitive staging checks separately; do not imply they ran locally.

  Local verification evidence (2026-07-22): the first exact cross-package race
  run exposed the three parallel checkpoint tests concurrently calling global
  `gin.SetMode`; the focused five-count race reproduction exited 1 with the same
  writes at the then-current `checkpoint_test.go:30`, `:82`, and `:135`. Package
  `init()` already establishes Gin test mode, so the controller authorized the
  minimal test-only correction: remove only those redundant writes while
  retaining `t.Parallel()` and every production path. The same focused
  five-count race command then exited 0, and the exact required cross-package
  race command exited 0 with no race report.

  On the final working diff, `go test ./... -count=1`, `go vet ./...`, `go build
  ./...`, and `git diff --check` exited 0. The touched Go files have no `gofmt
  -d` output. The race runs emitted only the existing non-fatal macOS
  `LC_DYSYMTAB` linker warnings. Environment-sensitive staging checks were not
  run locally and remain Task 9 work.

- [x] **Step 4: Run broad branch review and resolve all Critical/Important findings**

Generate the review package from `63f29f64` to `HEAD`, dispatch an independent code reviewer, apply accepted findings one at a time, rerun affected tests, and repeat until Critical and Important are empty.

  Initial branch-wide review evidence (2026-07-22): the Spec and Standards
  reviewers reported `0 Critical / 4 Important / 3 Minor` findings. The four
  Important findings were PR #192 fallback compatibility, recovery publication
  without all three generated segment leases, missing roster-digest
  recomputation, and the authoritative spec's stale implementation status. The
  three Minor findings were raw-string timezone digesting, four unused
  status-derived `*Fresh` fields, and the ambiguous Task 8 ledger wording.

  Correction RED/GREEN evidence (2026-07-22): fallback RED rejected a
  32-64 MiB compatibility response at 32 MiB and rejected unordered source rows;
  GREEN restores the strict-less 64 MiB requested-user parser and per-user sort
  while the provider-wide source retains its 32 MiB/order/coverage checks.
  Recovery RED showed that five valid publication claims exceeded the old
  four-claim cap and that a competitor could replace `history_29d` ownership
  before a three-claim recovery publish; GREEN bounds each lane below the
  existing 90-second leases, fetches through the existing two-call limiter,
  drains every result, retains all three segment tokens, and atomically checks
  tick, active, and three segment claims. The lost-history lane now fails while
  another timezone publishes, and no worker/source lease remains. Roster RED
  accepted an envelope and reference carrying the same tampered digest as a
  full hit; GREEN recomputes the existing length-delimited digest over ordered
  Stats IDs and selects exact fallback. Timezone RED locked the old raw SHA
  mismatch; GREEN uses the length-delimited vector before any runtime enablement.
  The unused `*Fresh` fields are removed, and the current spec and this ledger
  now reflect the default-disabled implemented state.

  Correction verification (2026-07-22): the exact fallback and provider-wide
  Relay tests, repeated recovery lease/lane/concurrency tests, roster and
  timezone digest tests, full Relay, Team Usage, readcache, config, telemetry,
  handler, and Dashboard packages, `go test ./... -count=1`, and the required
  cross-package race command all pass. `go vet ./...`, `go build ./...`, `jq
  empty` on the dashboard, touched-file `gofmt -d`, and `git diff --check` exit
  zero. The race link emitted only the existing non-fatal macOS `LC_DYSYMTAB`
  warnings and no race report. One focused rerun initially exposed the old
  synthetic cache helper's intentionally unrelated digest; changing only that
  helper to the exact digest for user 101 made the same command and the full
  suite pass.

  Final review evidence (2026-07-22): the same Standards and Spec reviewers
  independently reviewed immutable package `63f29f64..875aa371`. Both reported
  zero Critical, Important, or Minor findings. The package retained the locked
  Task 9 boundary: `docs/architecture.md`, staging, Helm, and production remain
  unchanged. Step 5 remains unchecked; no commit has been created.

- [x] **Step 5: Commit documentation and review corrections**

```bash
git add deploy/observability docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): add prewarm operations guidance"
```

Do not update `docs/architecture.md` yet; the feature is still disabled and is not current runtime.

  Completed on 2026-07-22 after both final reviewers reported zero findings.
  Branch-review behavior corrections are committed in `506bd403`; this step
  commits the dashboard contract, Grafana panels, operator guidance,
  authoritative default-disabled status, and execution ledger. Task 9 staging
  acceptance and `docs/architecture.md` remain intentionally untouched.

---

### Task 9: Benchmark Redis And Run Staging Acceptance

**Files:**
- Modify with sanitized evidence: `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`
- Modify with live checkbox/evidence updates: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`
- Modify after successful staging enablement: `docs/architecture.md`
- Modify in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Staging-only release inputs in the existing deployment workflow; never production values.

**Interfaces:**
- Consumes: exact reviewed branch head and synthetic maximum-safe Redis fixtures.
- Produces: locked large-value read/write command budgets and sanitized three-round staging evidence.

- [x] **Step 1: Publish and deploy an immutable staging image with the feature disabled**

  **Execution evidence (2026-07-22, complete):** Before the Task 9 rollout,
  production was healthy at Helm revision 69 on
  `ghcr.io/lichking-2234/ai-efficiency:v0.1.0-preview.73`, with the prewarm flag
  absent and one of one Deployment replicas ready. Staging was healthy at Helm
  revision 44 on the retained PR #192 image
  `ghcr.io/lichking-2234/ai-efficiency:staging-627a7123d98aee37dd04fd5da2198234cfd003f0`,
  also with the flag absent and one of one replicas ready. Staging live/readiness
  and production readiness each returned HTTP 200. No secret values or response
  bodies were retained. The required `static-spaces-release-builder` advertised
  both `linux/amd64` and `linux/arm64`, so it passed the local builder gate before
  any image was published. Exact reviewed HEAD
  `667f4032e0ec9e60b8dd240fe55c0db2833e9509` was then published as
  `ghcr.io/lichking-2234/ai-efficiency:staging-667f4032e0ec9e60b8dd240fe55c0db2833e9509`
  with remote manifest digest
  `sha256:5a25d8feadd213ccf64706281d05411628d549d675788c14bdf92b0bf32dfb6b`.
  The inspected remote index contains both required target manifests:
  `linux/amd64` at `sha256:0bdc1850475bea5495c7b2732b7b76041f46314297a11118a822d3cd715e9be8`
  and `linux/arm64` at `sha256:bf5ca2d1b13f096201856af0e97e3e5aecfab39eeef65bc716fba099b7f07c24`.
  The staging refresh script left the static override at mode `0600` with the
  exact image tag and snapshot ID `667f4032e0ec`; its parsed values still omit
  `AE_TEAM_USAGE_PREWARM_ENABLED`. A deployment-only render likewise omitted
  the flag, and the paused `--dry-run=server --hide-secret` completed
  successfully. The final tracked static override diff is limited to the
  non-secret image tag and snapshot ID: canonical JSON hashes are identical
  after deleting those two selectors. No secret value changed in the retained
  diff, and no secret values were printed or retained outside the `0600` values
  file. The
  required paused upgrade completed at Helm revision 45 with zero desired and
  zero ready application replicas; the rendered flag remained absent. The
  explicit application-Pod deletion gate then observed zero matching Pods both
  before and after its wait, proving the old staging application was gone before
  the restore-enabled phase. The restore-enabled
  `--dry-run=server --hide-secret` also completed successfully and its
  deployment-only render continued to omit the feature flag. The actual atomic
  restore-enabled upgrade, including the waited restore Job, completed at Helm
  revision 46. The PostgreSQL StatefulSet and application Deployment both
  completed rollout; the Deployment was desired/ready/available `1/1/1`. The
  sole application Pod used the exact immutable image and had the feature flag
  absent. Staging live and readiness endpoints each returned HTTP 200. The final
  production gate exactly matched the baseline: Helm revision 69, image
  `ghcr.io/lichking-2234/ai-efficiency:v0.1.0-preview.73`, absent feature flag,
  one of one replicas ready, and HTTP 200 readiness.

Build the exact reviewed HEAD through the repository's current staging workflow, verify the GHCR manifest digest, deploy only `ai-efficiency-staging`, and record image digest and Helm revision. Confirm `AE_TEAM_USAGE_PREWARM_ENABLED=false`. Verify production image, configuration, and Helm revision before and after.

Run the immutable build from `/Users/admin/helm`:

```bash
APP_WORKTREE=/Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
COMMIT="$(git -C "${APP_WORKTREE}" rev-parse HEAD)"
IMAGE="ghcr.io/lichking-2234/ai-efficiency:staging-${COMMIT}"
docker buildx build --builder static-spaces-release-builder \
  --platform linux/amd64,linux/arm64 \
  --file "${APP_WORKTREE}/deploy/Dockerfile" \
  --tag "${IMAGE}" \
  --build-arg APP_VERSION="staging-${COMMIT:0:7}" \
  --build-arg APP_COMMIT="${COMMIT}" \
  --build-arg APP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --push "${APP_WORKTREE}"
docker buildx imagetools inspect "${IMAGE}"
```

Use `/Users/admin/helm/ai-efficiency/scripts/refresh-staging-upgrade-values.sh` for the exact image tag and snapshot ID, then follow `/Users/admin/helm/docs/staging-playbook.md` paused and restore-enabled phases. Before the disabled rollout, assert the rendered Deployment either omits `AE_TEAM_USAGE_PREWARM_ENABLED` or sets it to `false`; verify both architectures in the remote manifest.

- [x] **Step 2: Benchmark staging Redis with synthetic maximum-safe values**

  **Initial excluded evidence (2026-07-22):** The reviewed runtime
  configures the shared Redis client with `MinIdleConns=4`, one-second dial and
  pool timeouts, disabled command retries, and context timeouts enabled. Its
  distributed source limiter has exactly two process and Redis slots. The live
  staging application remained feature-disabled before the synthetic benchmark.
  A first local execution proved that a single maximum-safe lane
  (`27,262,972` bytes across current plus three segment values) exceeded the
  two-second read timeout over the non-runtime local network path. The identical
  static probe was therefore moved into the staging application Pod without
  changing its payloads, concurrency, timeouts, or Redis commands. Its transport
  smoke passed config, authentication, PING, four-connection pool warmup, and
  `MinIdleConns>=4`. Its ordered one-sample smoke passed maximum-safe current and
  segment immutable writes, five-lease setup, the six-key/eight-argument
  token-checked manifest publication, one-lane MGET, four-lane concurrent MGET,
  and cleanup. The full in-Pod run then failed after approximately 53 seconds at
  fixed diagnostic stage `mget` with class `redis_command`. Read-only root-cause
  review found that the probe created 200 unique large write-sample keys and
  deferred cleanup until the end. Those current and segment values alone totalled
  `1,048,575,800` raw bytes; warmup, manifests, and four-lane seeds raised the
  raw total to approximately `1.168` GB before Redis overhead. Redis reports
  `maxmemory=1,073,741,824`, `maxmemory_policy=volatile-lru`, current post-cleanup
  used memory `6,280,576`, and lifetime peak used memory `1,100,870,752` bytes.
  The application Pod remained ready with zero restarts and no OOM/kill event;
  its post-cleanup cgroup usage was `21,676,032` of `536,870,912` bytes. No
  before-run Redis counters or Pod working-set sample exists, and the probe
  intentionally collapsed the command error without retaining its raw string,
  so no timeout/EOF/reset/OOM subtype can be claimed. The accumulated sample-key
  shape does not represent one runtime generation, making the full run invalid
  rather than an accepted capacity result. The probe retained no partial latency
  report, so the budget formula cannot be applied. Cleanup completed (the
  terminal stage was `mget`, not `cleanup`), and
  the Pod binary, local probe source/binary, and all temporary reports were then
  deleted and verified absent. A controller-authorized corrected harness then
  passed a static live-set assertion of `102,760,435 < 268,435,456` raw bytes
  with at most 13 live value keys, but its only formal execution stopped at
  diagnostic stage `baseline` before generating a probe ID or any synthetic key.
  It issued an unsupported combined `INFO memory stats` command where this Redis
  endpoint requires separate section commands. That corrected source, binary,
  and empty diagnostic report were also deleted and verified absent. No valid
  p50/p95/p99/max distribution or before/after counter pair exists. Step 2
  remains unchecked by design. The final safety check found Redis used memory at
  `6,309,464` bytes, staging still disabled and healthy at revision 46, and
  production unchanged and healthy at revision 69.

  **Accepted package-native benchmark and blocking result (2026-07-22):** A
  temporary `teamusage` test used the existing RedisStore primitives rather than
  duplicating their protocol. Its miniredis RED/GREEN cycle proved 13 live value
  keys, `102,760,435` raw bytes below a 256-MiB guard, immediate deletion of each
  unique write sample, fixed MGET seeds, registry-only cleanup, and zero remaining
  namespace keys. The same statically linked `linux/amd64` test binary ran in the
  feature-disabled staging application Pod. Its smoke passed before the only
  full run. Current SET NX, segment SET NX, five-lease manifest publication,
  lease acquire, and lease release each completed 100 samples with zero command
  errors. Their p50/p95/p99/max values in milliseconds were respectively
  `21.126/21.582/23.142/98.109`,
  `142.001/239.545/242.412/245.360`,
  `11.998/12.096/12.134/12.196`,
  `11.705/11.751/11.871/15.047`, and
  `11.716/11.760/11.834/12.274`.

  The four-lane MGET workload completed 15 valid lane samples at
  `p50=1099.047 ms` and `p95/p99/max=1900.419 ms`, then one command hit the
  configured two-second read timeout in the fourth concurrent round. The
  partial distribution is retained only for diagnosis and is not used to select
  a budget; the real command error independently blocks Step 2. Full cleanup had
  zero errors; Redis used memory returned to `6,410,536` bytes with zero eviction
  and rejected-connection delta. Staging remained revision 46 with the flag
  absent, one ready Pod, zero restarts, and healthy live/readiness checks.
  Production remained revision 69 on `v0.1.0-preview.73`, with the flag absent
  and healthy. All temporary test sources, binaries, and JSON reports were
  deleted; only sanitized evidence was retained. Therefore Step 2 remains
  unchecked, no budget constants or commit exist, and Steps 3-6 must not start.

  **Accepted schema-v2 replacement (2026-07-22):** Tasks 10-13 preserved the
  failed schema-v1 run as diagnostic evidence, then replaced its values with the
  production schema-v2 zstd codec and narrowed 30-day request reads to current,
  history-29d, and today-hour. Exact budget commit `759b1946` ran in the
  feature-disabled staging Pod at revision 50. Its final full repeat completed
  100 samples with zero errors for current write, segment write, five-lease
  manifest publication, full-generation MGET, four-concurrent-lane 30-day MGET,
  lease acquire, and lease release. Their p99 values were `14.963`, `20.989`,
  `13.548`, `75.757`, `47.576`, `12.664`, and `12.718` milliseconds. The
  unchanged formula selected and the code locks `250ms` for read, write, lease,
  and release. Cleanup errors and final registered key count were zero; eviction
  and rejected-connection deltas were zero. The staging image remained exact,
  ready, and feature-disabled after the benchmark. Production remained revision
  69 on `v0.1.0-preview.73`, feature-disabled and healthy. Step 2 is complete;
  Steps 3-6 remain unchecked pending separate feature-enabled acceptance.

Under `MinIdleConns>=4` and background concurrency two, measure separate immutable writes, manifest publication, and four-lane MGET reads. Select each command budget as `max(250ms, 2*p99)` capped at `2s`. Any Redis error or required budget above two seconds blocks enablement. Write RED boundary tests for the selected budgets, implement the exact constants, and run:

```bash
cd backend
gofmt -w internal/teamusage cmd/server
go test ./internal/teamusage ./cmd/server -run 'Prewarm.*CommandBudget|RedisClientOptions' -count=1
go test -race ./internal/teamusage ./cmd/server -run 'Prewarm.*CommandBudget|RedisClientOptions' -count=1
cd ..
git add backend/internal/teamusage backend/cmd/server
git commit -m "perf(teamusage): lock staging Redis command budgets"
```

Generate a focused review package for this commit and resolve every Critical/Important finding. Rebuild a new immutable staging image and repeat the feature-disabled benchmark because the exact code changed.

- [ ] **Step 3: Enable staging and wait for four valid lanes**

Enable the feature only in staging with the four approved timezones. Within five seconds of the recorded readiness point, require valid current stats, both histories, today, and a publish-last manifest for every safe lane. Confirm global source concurrency never exceeds two, one logical cycle across Pods, and zero Relay/Redis error counters.

  **Failed staging attempt (2026-07-22):** The exact accepted image from budget
  commit `759b1946` was atomically enabled only in staging at Helm revision 51
  with exactly two ready replicas and the four approved timezones. All four safe
  schema-v2 manifests referenced current stats, history-29d, history-6d, and
  today values that existed in Redis. Manifest creation timestamps spanned
  `1.375s` from the first complete lane to the last.

  Before any Summary validation probe, the snapshot already established the
  hard failure: the two Pods each recorded four successful startup labels and
  16 successful source observations, showing two source-producing Pod cycles
  rather than one collapsed logical startup cycle. The same pre-validation
  snapshot contained two Redis errors, including one lease-acquire error, and
  six cycle errors (two historical and four recovery); the other Redis error
  was a manifest-read error. It also contained one manifest-cache error. Relay
  non-2xx/source-error, validation-rejection, and Redis-pool-timeout counts were
  zero.

  Despite the already-conclusive Step 3 failure, eight bounded 7-day and 30-day
  Summary probes were then executed and recorded `full_hit`. This was a process
  deviation from the required fail-fast stop and must not be treated as Step 3
  compliance or acceptance-round evidence. Across that interval, total Redis
  errors increased by one, lease-acquire Redis errors increased by one, and
  cycle errors increased by four. The post-interval totals were therefore three
  Redis errors, including two lease-acquire errors, and ten cycle errors. Step 4
  did not start: no Team Usage Redis family was deleted and no three-round API
  acceptance evidence was collected.

  Only after the eight probes did Helm rollback restore the exact revision-50
  disabled selector as revision 52. Staging finished at one of one replicas
  ready on the same immutable image, with the prewarm environment entries
  absent and live/readiness HTTP 200. The tracked staging override was restored
  byte-for-byte with mode `0600` and has no diff. Production remained healthy
  and unchanged at revision 69 on `v0.1.0-preview.73`, with one ready replica and
  the flag absent. Steps 3-6 remain incomplete and unchecked, and
  `docs/architecture.md` was not modified.

  **Failed HTTP acceptance preflight (2026-07-24):** One separately
  user-authorized retry started from exact-image disabled staging revision 64
  and unchanged production revision 69. An independent rollback guardian was
  armed before one staging-only atomic upgrade enabled revision 65 with two
  ready replicas, zero restarts, and the four approved timezones. External
  live/readiness returned HTTP 200.

  Before login, response-cache deletion, or any Team Usage request, anonymous
  per-replica metrics recorded four `startup/success` labels and four successful
  startup source observations on the owner, and four `startup/lease_busy`
  labels with zero startup source observations on the loser. Redis contained
  four schema-v2 manifest keys. The same snapshot already contained three
  `command_deadline` errors: two `generation_read` and one `manifest_read`.
  Four background cycles ended in error. Relay recorded 150 requests with zero
  errors.

  The zero-Redis-error gate failed before lane completeness or timing could be
  accepted. No account was authenticated, no Redis key was deleted, no
  application API was called, and no response body was retained. Cold/warm
  timings, `full_hit`, payload hashes, and request-time Relay deltas are not
  available for this attempt. The guardian performed the only rollback and
  produced healthy exact-image disabled staging revision 66 with one ready
  replica, zero restarts, no prewarm environment, and HTTP 200 live/readiness.
  Production remained healthy and unchanged at revision 69. Steps 3-6 remain
  incomplete and unchecked, and `docs/architecture.md` remains unchanged.

- [ ] **Step 4: Run the three sanitized acceptance rounds**

Using the same authorized account and deterministic standard window:

```text
Round 1: delete only new prewarm, existing Team Usage origin/response, and lease keys; request all four lanes concurrently.
Round 2: retain valid prewarm generations, delete only four response-cache keys; request all four lanes concurrently.
Round 3: delete nothing; immediately repeat all four lanes.
```

Require HTTP 200 and business-payload equivalence; prewarmed cold lanes `<5s`; immediate warm lanes `<1.5s`; fully prewarmed requests make zero Relay calls; zero Relay 429/5xx/transport/timeout and Redis read/write/pool/lease/decode/timeout errors; empty Redis no worse than PR #192 by more than one second; one cycle across Pods; no completed Pod result.

- [ ] **Step 5: Record evidence, update current architecture, and keep production unchanged**

Record only sanitized request/operation IDs, durations, counts, outcomes, digest, and staging revision. Update `docs/architecture.md` to describe the now-active optional staging runtime and retained exact fallback. Recheck production image/config/revision/traffic and record unchanged status.

- [ ] **Step 6: Commit the acceptance ledger**

```bash
git add docs/architecture.md \
  docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): record segmented prewarm staging acceptance"
```

If any gate fails, disable the staging flag, record the failure with remaining checkboxes unchecked, and stop. Do not flush unrelated Redis keys, deploy Sub2API, or alter production.

---

### Task 10: Store Prewarm Immutable Values As Schema-v2 Zstd Frames

**Files:**
- Create: `backend/internal/teamusage/prewarm_codec.go`
- Create: `backend/internal/teamusage/prewarm_codec_test.go`
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_cache_test.go`
- Modify: `backend/internal/teamusage/prewarmer_test.go` (schema-v2 opaque-value test interceptor only)
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

**Interfaces:**
- Consumes: existing `encodePrewarmJSON`, `decodePrewarmJSON`, 2 MiB current-stats limit, 8 MiB trend limit, and opaque `readcache.BatchStore` bytes.
- Produces: schema-v2 keys plus package-private `encodePrewarmStoredJSON(value any, decodedLimit, storedLimit int) ([]byte, error)` and `decodePrewarmStoredJSON(encoded []byte, decodedLimit int, destination any) error`.

- [x] **Step 1: Write RED codec and cache-storage tests**

Add table tests that require the zstd magic prefix, checksum-protected round
trip, stored-byte metadata, and hard rejection boundaries. Use only synthetic
values:

```go
func TestPrewarmStoredJSONRoundTripAndCorruption(t *testing.T) {
	value := testPrewarmSegmentValue(t, SegmentHistory29d)
	encoded, err := encodePrewarmStoredJSON(value, prewarmSegmentMaxBytes, prewarmSegmentMaxBytes)
	if err != nil {
		t.Fatalf("encodePrewarmStoredJSON() error = %v", err)
	}
	if !bytes.HasPrefix(encoded, []byte{0x28, 0xb5, 0x2f, 0xfd}) || bytes.Contains(encoded, []byte(`"points"`)) {
		t.Fatal("stored value is not an opaque zstd frame")
	}
	var decoded PrewarmTrendSegment
	if err := decodePrewarmStoredJSON(encoded, prewarmSegmentMaxBytes, &decoded); err != nil {
		t.Fatalf("decodePrewarmStoredJSON() error = %v", err)
	}
	if diff := cmp.Diff(value, decoded); diff != "" {
		t.Fatalf("round trip mismatch (-want +got):\n%s", diff)
	}
	for _, corrupted := range [][]byte{encoded[:len(encoded)-1], append(append([]byte(nil), encoded...), 0xff)} {
		if err := decodePrewarmStoredJSON(corrupted, prewarmSegmentMaxBytes, &decoded); err == nil {
			t.Fatal("corrupt frame decoded without error")
		}
	}
}
```

Also require `prewarmCacheSchemaVersion == 2`, raw JSON absent from Redis,
`SerializedBytes == len(stored)`, raw JSON exactly at each decoded limit,
compressed output exactly at its stored limit, expanded output at the decoded
limit, checksum corruption, truncation, appended bytes, and a v1 manifest miss.

- [x] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmStoredJSON|StoresCompressedBytesAndUsesSchemaV2|SchemaV2' -count=1
```

Expected: compile failure for missing stored-codec functions and schema-version
assertion failure until the production implementation exists.

- [x] **Step 3: Add the direct dependency and minimal bounded codec**

```bash
cd backend
go get github.com/klauspost/compress@v1.18.0
```

Create `prewarm_codec.go` with reusable stateless encoder and decoder instances.
Keep compression inside `teamusage`; do not change `readcache.BatchStore`:

```go
type prewarmStoredCodecPair struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

var prewarmStoredCodec = sync.OnceValues(func() (*prewarmStoredCodecPair, error) {
	encoder, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderCRC(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create prewarm zstd encoder: %w", err)
	}
	decoder, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(uint64(prewarmSegmentMaxBytes)),
		zstd.WithDecoderMaxWindow(uint64(prewarmSegmentMaxBytes)),
		zstd.WithDecodeAllCapLimit(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create prewarm zstd decoder: %w", err)
	}
	return &prewarmStoredCodecPair{encoder: encoder, decoder: decoder}, nil
})
```

`encodePrewarmStoredJSON` calls `encodePrewarmJSON`, compresses with `EncodeAll`,
and rejects `len(encoded) >= storedLimit`. `decodePrewarmStoredJSON` rejects
empty or over-width input, decodes into capacity `decodedLimit`, rejects
`len(decoded) >= decodedLimit`, then calls `decodePrewarmJSON` so concatenated
or trailing JSON and metadata tampering still fail.

- [x] **Step 4: Wire compressed values and schema v2 into `PrewarmCache`**

Change only current-stats and trend immutable values:

```go
const prewarmCacheSchemaVersion = 2

encoded, err := encodePrewarmStoredJSON(
	value, prewarmCurrentStatsMaxBytes, prewarmCurrentStatsMaxBytes,
)

if err := decodePrewarmStoredJSON(values[0], prewarmCurrentStatsMaxBytes, &decoded); err != nil {
	return nil, PrewarmSegmentSet{}, statuses, false,
		fmt.Errorf("decode team usage prewarm current stats: %w", err)
}
```

Apply the trend path with `prewarmSegmentMaxBytes`. Manifests remain plain JSON.
Keep reference validation against stored frame length and generation-byte metrics
based on `SerializedBytes`.

- [x] **Step 5: Run GREEN and race tests**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_codec.go \
  internal/teamusage/prewarm_codec_test.go \
  internal/teamusage/prewarm_cache.go \
  internal/teamusage/prewarm_cache_test.go
go test ./internal/teamusage -run 'PrewarmStoredJSON|PrewarmCache|PrewarmManifest' -count=1
go test -race ./internal/teamusage -run 'PrewarmStoredJSON|PrewarmCache|PrewarmManifest' -count=1
```

Expected: all selected tests pass with zero race reports.

- [x] **Step 6: Commit the codec unit**

```bash
git add backend/go.mod backend/go.sum \
  backend/internal/teamusage/prewarm_codec.go \
  backend/internal/teamusage/prewarm_codec_test.go \
  backend/internal/teamusage/prewarm_cache.go \
  backend/internal/teamusage/prewarm_cache_test.go \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "perf(teamusage): compress prewarm Redis values"
```

---

### Task 11: Read Only The Requested Standard Window

**Files:**
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_cache_test.go`
- Modify: `backend/internal/teamusage/prewarm_reader.go`
- Modify: `backend/internal/teamusage/prewarm_reader_test.go`

**Interfaces:**
- Consumes: schema-v2 compressed immutable values from Task 10 and existing `PrewarmWindowClass` recognition.
- Produces: `(*PrewarmCache).ReadWindow(context.Context, PrewarmCacheIdentity, PrewarmWindowClass) (*PrewarmCacheResult, bool, error)` while preserving full `Read` for lifecycle and publication validation.

- [x] **Step 1: Write RED key-selection and relative-completeness tests**

Use `recordingPrewarmStore.LastMGet()` to require the exact key order:

```go
tests := []struct {
	name  string
	class PrewarmWindowClass
	want  func(PrewarmManifest) []string
}{
	{"today", PrewarmWindowToday, func(m PrewarmManifest) []string {
		return []string{m.CurrentStats.Key, m.TodayHour.Key}
	}},
	{"7d", PrewarmWindow7d, func(m PrewarmManifest) []string {
		return []string{m.CurrentStats.Key, m.History6d.Key, m.TodayHour.Key}
	}},
	{"30d", PrewarmWindow30d, func(m PrewarmManifest) []string {
		return []string{m.CurrentStats.Key, m.History29d.Key, m.TodayHour.Key}
	}},
}
```

For every class, delete or corrupt the unselected history value and require a
complete request-relative result with no unselected cache-outcome metric. Delete
the selected history value and require an incomplete result. Prove ordinary
`Read` still MGETs all four references and detects that missing/corrupt value.

- [x] **Step 2: Run RED cache tests**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmCacheReadWindow|PrewarmReaderWindowSelection' -count=1
```

Expected: compile failure because `ReadWindow` does not exist and the reader
still calls full `Read`.

- [x] **Step 3: Implement an explicit internal selection model**

Keep one manifest GET and one selected-value MGET:

```go
type prewarmReadSelection struct {
	currentStats bool
	history29d   bool
	history6d    bool
	todayHour    bool
}

func prewarmSelectionForWindow(class PrewarmWindowClass) (prewarmReadSelection, error) {
	selection := prewarmReadSelection{currentStats: true, todayHour: true}
	switch class {
	case PrewarmWindowToday:
	case PrewarmWindow7d:
		selection.history6d = true
	case PrewarmWindow30d:
		selection.history29d = true
	default:
		return prewarmReadSelection{}, fmt.Errorf("invalid prewarm window class %q", class)
	}
	return selection, nil
}
```

Refactor full `Read` through an all-reference selection and add `ReadWindow`.
`readReferencedValues` must build keys, statuses, decode work, completeness, and
cache metrics only from selected references while preserving manifest order.

- [x] **Step 4: Make request-time recovery window-relative**

Change the reader call and recovery predicate:

```go
result, found, err := r.cache.ReadWindow(ctx, identity, window.Class)

func prewarmResultCanRecoverToday(result *PrewarmCacheResult, class PrewarmWindowClass) bool {
	if result == nil || result.CurrentStats == nil {
		return false
	}
	switch class {
	case PrewarmWindowToday:
	case PrewarmWindow7d:
		if result.Segments.History6d == nil { return false }
	case PrewarmWindow30d:
		if result.Segments.History29d == nil { return false }
	default:
		return false
	}
	return result.TodayHourStatus == PrewarmValueMissing ||
		result.TodayHourStatus == PrewarmValueInvalid ||
		result.TodayHourStatus == PrewarmValueHardExpired
}
```

Retain fresh/stale eligibility for current stats and the selected history.
Do not weaken authorization, point validation, or composition validation.

- [x] **Step 5: Run GREEN, equivalence, and race tests**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_cache.go \
  internal/teamusage/prewarm_cache_test.go \
  internal/teamusage/prewarm_reader.go \
  internal/teamusage/prewarm_reader_test.go
go test ./internal/teamusage -run 'PrewarmCacheReadWindow|PrewarmReader|PrewarmEquivalence' -count=1
go test -race ./internal/teamusage -run 'PrewarmCacheReadWindow|PrewarmReader|PrewarmEquivalence' -count=1
```

Expected: selected tests pass, public Team Usage DTO equivalence stays exact,
and race reports remain empty.

- [x] **Step 6: Commit the request-read unit**

```bash
git add backend/internal/teamusage/prewarm_cache.go \
  backend/internal/teamusage/prewarm_cache_test.go \
  backend/internal/teamusage/prewarm_reader.go \
  backend/internal/teamusage/prewarm_reader_test.go \
  docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "perf(teamusage): scope prewarm reads by window"
```

---

### Task 12: Verify And Review The Compressed Read Branch

**Files:**
- Modify as evidence is completed: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`
- Modify only for contract corrections: `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`

**Interfaces:**
- Consumes: Task 10 schema-v2 codec and Task 11 window-scoped reader.
- Produces: one reviewed exact branch head eligible for a new feature-disabled staging image.

- [x] **Step 1: Run the full local verification ladder**

```bash
cd backend
gofmt -w internal/teamusage
go test ./internal/teamusage -count=1
go test ./... -count=1
go test -race ./internal/teamusage ./cmd/server -count=1
go vet ./...
go build ./...
cd ..
git diff --check
```

Expected: every command exits zero with no test failure, race report, vet error,
build error, or whitespace error.

Verification evidence (2026-07-22, head `0f0ba03a`):
`gofmt -w internal/teamusage` produced no diff;
`go test ./internal/teamusage -count=1` passed (`22.527s`);
`go test ./... -count=1` passed (including `internal/teamusage` at `44.032s`);
`go test -race ./internal/teamusage ./cmd/server -count=1` passed both packages
with no race report; and `go vet ./...`, `go build ./...`, and
`git diff --check` all exited zero without errors.

- [x] **Step 2: Review Standards and Spec compliance**

Use the repository code-review workflow against merge-base through current HEAD.
Review zstd decompression bounds, shared-codec concurrency, stored-vs-decoded
byte semantics, v1/v2 isolation, request-relative cache metrics, missing-today
recovery, and unchanged full-generation publication. Resolve every Critical or
Important finding with a fresh RED/GREEN cycle, then rerun Step 1.

Review evidence (2026-07-22): the initial branch-wide review from base
`1d3a8c6c` through head `0f0ba03a` reported two Important and two Minor
findings. They covered request-scoped missing-today recovery being coupled to
strict full-generation publication, stale plan/spec status, acceptance of an
appended legal zstd frame, and schema-v2 tamper tests stopping at codec decode
instead of their named validators. Commit `4bf72e9b` closed all four with fresh
RED/GREEN coverage and the full Step 1 ladder. Re-review then found one
Important first-error masking case where an unavailable unselected reference
could hide an unavailable selected reference. Commit `10d1e8ea` made selected
reference validation exhaustive, passed fresh RED/GREEN tests, and reran the
full Step 1 ladder. Final independent review of exact head `10d1e8ea` reported
zero Critical, zero Important, and zero Minor findings.

- [x] **Step 3: Record the reviewed head and commit the local ledger**

Update the plan status and completed checkboxes with exact commands and commit
IDs. Keep Task 9 Steps 2-6 unchecked and do not modify `docs/architecture.md`.

Ledger evidence (2026-07-22): exact reviewed implementation head
`10d1e8eabb0221acbee027ed599c2bae05d8b7b3` includes correction commits
`4bf72e9b7532c045de7c63d34b55e346a2d5c29a` and
`10d1e8eabb0221acbee027ed599c2bae05d8b7b3`. Pre-commit `git diff --check`
exited zero, and the only tracked ledger changes were this plan and the current
design spec. Task 9 Steps 2-6 remained unchecked, command budgets remained
unselected, the feature remained disabled, production remained unchanged, and
`docs/architecture.md` had no diff.

```bash
git add docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md \
  docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md
git commit -m "docs(teamusage): complete compressed read review"
```

---

### Task 13: Rebenchmark Schema-v2 Redis Before Enablement

**Files:**
- Modify with sanitized evidence: `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`
- Modify with sanitized evidence: `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`
- Modify after measured budgets pass: `backend/internal/teamusage/prewarm_cache.go`
- Modify after measured budgets pass: `backend/internal/teamusage/prewarm_cache_test.go`
- Modify exact staging selectors in `/Users/admin/helm`: `ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`

**Interfaces:**
- Consumes: exact reviewed Task 12 head, accepted package-native benchmark shape, staging Redis, and the disabled staging workflow.
- Produces: selected schema-v2 command budgets and a passing repeat benchmark, or a documented blocker with the feature still disabled.

- [x] **Step 1: Build and deploy the exact reviewed head disabled**

  **Execution evidence (2026-07-22, complete):** Before this rollout, staging
  was healthy at revision 46 on the previous reviewed image and production was
  healthy at revision 69 on `v0.1.0-preview.73`; both Deployments omitted
  `AE_TEAM_USAGE_PREWARM_ENABLED`, had one ready replica with zero restarts,
  and returned HTTP 200 for live and readiness. The required
  `static-spaces-release-builder` advertised both `linux/amd64` and
  `linux/arm64`. Exact reviewed head `e1452ca834d37769036df0f2a1630c9b95161aed`
  was published as a multi-architecture image with index digest
  `sha256:8fd47a1eea943892843448abb546669e980cba9dd600c4a2049f6e4c4fd65cf8`;
  the amd64 and arm64 manifest digests are respectively
  `sha256:c0506a543afa4f08920ff5ef08946e23f6bd3b76c622fc14157e0cc2e945dc62`
  and
  `sha256:44ab128bf053082de61ddf03e5ac38ba389b03861f613045f221913deabbc701`.
  The staging override remained mode `0600`; canonical JSON excluding only
  image tag and restore snapshot ID was unchanged, and both the parsed override
  and rendered Deployment omitted the feature flag. The paused server dry-run
  and atomic upgrade completed at revision 47 with zero application Pods. The
  restore-enabled server dry-run and waited atomic upgrade completed at
  revision 48. The final staging Deployment ran only the exact image with one
  ready replica, zero restarts, no feature-flag entry, and HTTP 200 live and
  readiness. Production remained revision 69 on `v0.1.0-preview.73`, with the
  flag absent, one ready replica, and HTTP 200 live and readiness.

Reuse Task 9 Step 1's immutable Buildx and paused/restore-enabled Helm procedure.
Verify both GHCR architectures, exact staging image, absent
`AE_TEAM_USAGE_PREWARM_ENABLED`, staging live/readiness HTTP 200, and unchanged
production revision/image/flag/health before and after.

- [x] **Step 2: Recreate the package-native benchmark outside the repository**

  **Excluded invalid smoke (2026-07-22):** The first staged schema-v2 binary
  stopped at `stage=request_lane_mget class=shape` after two valid lane samples
  and two byte-count mismatches. All four MGET commands returned without a Redis
  command or timeout error. The harness incorrectly compared every lane against
  lane 0's compressed byte total even though each lane uses distinct generation
  IDs and therefore may have a different zstd length. This run is excluded from
  benchmark evidence and budget selection. It had zero transport, INFO, and
  cleanup errors, final registered synthetic key count zero, and zero eviction
  and rejected-connection delta. The controller authorized one corrected formal
  smoke limited strictly to per-lane expected-byte accounting; payloads, codec,
  Redis commands, timeouts, concurrency, sample counts, and gates remain
  unchanged.

  **Corrected formal smoke (2026-07-22, pass):** The local RED failed for the
  missing per-lane byte-accounting helper. GREEN then proved that distinct
  generation IDs produce distinct compressed lane totals and that all four
  miniredis lane MGETs validate against their own current, history-29d, and
  today-hour byte sums. The corrected static `linux/amd64` binary ran in the
  same feature-disabled staging Pod. Decoded current JSON was `765,163` bytes;
  decoded history-29d, history-6d, and today-hour JSON were `8,388,489`,
  `8,388,488`, and `8,388,496` bytes, all strictly below their limits. The
  compressed full generation was `652,833` bytes and the recorded lane-0 30-day
  request window was `436,744` bytes. Current write, segment write, manifest
  write, full-generation MGET, lease acquire, and lease release each completed
  one valid sample; the four concurrent request lanes completed four valid
  samples. Their p99 values were respectively `11.724`, `17.145`, `12.437`,
  `69.882`, `11.320`, `11.282`, and `67.598` milliseconds. Every command class
  had zero errors. Cleanup, transport, and INFO errors were zero, the final
  registered synthetic key count was zero, and eviction and rejected-connection
  deltas were zero. This smoke selects no budget; the single full run is next.

  **Initial exact-head full run (2026-07-22, pass):** The unchanged corrected
  binary completed 100 valid samples with zero errors for current compressed
  write, segment compressed write, five-lease manifest publication,
  full-generation MGET, four-concurrent-lane 30-day request MGET, lease acquire,
  and lease release. Its p50/p95/p99/max values in milliseconds were
  `12.200/13.502/13.557/13.616`,
  `17.062/17.218/19.343/48.952`,
  `12.269/12.423/12.462/12.503`,
  `13.161/21.318/71.539/74.687`,
  `12.416/20.433/52.040/67.882`,
  `11.909/12.633/12.644/12.680`, and
  `11.788/12.619/12.635/12.777`. Transport, INFO, and cleanup errors were zero;
  final registered synthetic key count was zero. Redis used memory changed by
  `-8,016` bytes, peak used memory did not increase, eviction and
  rejected-connection deltas were zero, and the lifetime error-reply counter
  increased by 18 without any returned command error. The command-class formula
  selects `250 ms` for every class and therefore provisional `250 ms` read,
  write, lease, and release defaults. These values are not accepted until the
  RED/GREEN budget commit and its exact-code repeat benchmark pass.

Use a temporary `_test.go` copied into `backend/internal/teamusage` only for
compilation, then delete it before any commit. It must call the production
schema-v2 codec and `RedisStore`, use a unique synthetic namespace, print no
Redis URL or value, and guarantee registry-only cleanup. Its static gate is:

```text
decoded current JSON < 2 MiB
decoded segment JSON < 8 MiB each
one full generation = current + history_29d + history_6d + today_hour
one 30d request lane = current + history_29d + today_hour
four concurrent request lanes
final synthetic namespace key count = 0
```

Compile a static `linux/amd64` test binary, copy it into the feature-disabled
staging application Pod, run one smoke and then one full sample run. Delete the
source, local binary, Pod binary, and raw report after retaining only sanitized
counts, durations, byte sizes, error classes, and counter deltas.

- [x] **Step 3: Apply the unchanged budget gate**

  The initial exact-head full run satisfied the unchanged gate: every required
  class had 100 valid samples and zero errors, every calculated budget was
  `250 ms`, cleanup was complete, and eviction/rejected-connection deltas were
  zero. Step 4 still requires the same gate to pass again on the exact budget
  code before any value is accepted.

Measure at least 100 successful samples for compressed immutable writes,
manifest publication, full-generation MGET, 30d request-window MGET, lease
acquire, and lease release. Run four request lanes concurrently with background
concurrency two. For each command class calculate:

```text
budget = max(250 ms, 2 * p99)
```

Any Redis error, fewer than 100 valid samples, cleanup error, eviction,
rejected-connection increase, or required budget above 2 seconds blocks the
feature. Partial distributions never select a budget.

- [x] **Step 4: Lock passing budgets with RED/GREEN tests**

  **Budget TDD and local verification (2026-07-22, code ready for commit):**
  The focused RED run failed only because all four PrewarmCache defaults were
  still `2s` instead of the measured `250ms`. GREEN added distinct exact
  read, write, lease, and release default constants at `250ms` while retaining
  the existing explicit two-second cap and shared Redis transport ceilings.
  The first full-package run exposed one old test's hard-coded `59s` publication
  boundary, which encoded the previous `2s` write budget. That failure was
  reproduced three times; the test now derives the same equality boundary from
  the cache's actual write budget, without changing production publication
  logic. Its focused three-count rerun passed. The final focused, focused race,
  full-backend, vet, and build commands all passed. The race command emitted
  only the existing macOS linker `LC_DYSYMTAB` warning. Independent narrow
  review reported zero Critical, Important, or Minor findings. The budget-only
  implementation and tests are committed as `759b1946`; the exact-code image
  and repeat benchmark remain before this step can be checked.

  The exact-code repeat smoke on staging revision 50 passed with zero errors in
  every command class. Full-generation MGET completed one sample at
  `p99=71.196 ms`; four concurrent 30-day request lanes completed four samples
  at `p99=62.990 ms`. Cleanup errors and final registered key count were zero,
  and eviction/rejected-connection deltas were zero. The single full repeat
  completed all seven required classes with 100 valid samples and zero errors.
  Their p50/p95/p99/max values in milliseconds were
  `14.365/14.910/14.963/15.017`,
  `16.543/18.413/20.989/53.967`,
  `11.921/13.467/13.548/13.638`,
  `13.415/19.680/75.757/77.355`,
  `11.704/31.456/47.576/59.045`,
  `11.581/12.637/12.664/12.760`, and
  `11.367/12.657/12.718/12.819`. Each command-class and selected cache budget
  remained `250ms`. Cleanup, transport, and INFO errors were zero; final
  registered key count was zero. Used memory changed by `-143,720` bytes, peak
  used memory did not increase, eviction and rejected-connection deltas were
  zero, and the lifetime error-reply counter increased by 19 without a returned
  command error. The repeat package source, local binary, Pod binary, and raw
  reports were deleted and verified absent after this sanitized evidence was
  retained.

Only after Step 3 passes, write exact boundary tests for selected read, write,
lease, and release values. Run RED, change only the corresponding defaults or
constants, then run:

```bash
cd backend
go test ./internal/teamusage ./cmd/server -run 'Prewarm.*CommandBudget|RedisClientOptions' -count=1
go test -race ./internal/teamusage ./cmd/server -run 'Prewarm.*CommandBudget|RedisClientOptions' -count=1
go test ./... -count=1
go vet ./...
go build ./...
```

Commit with `perf(teamusage): lock compressed Redis command budgets`, rebuild an
exact immutable staging image, and repeat Steps 1-3 against that exact code. A
budget is not accepted until this second benchmark also passes.

- [x] **Step 5: Record the decision and resume Task 9 only on success**

  The final exact-code benchmark passed, so Task 9 Step 2 is checked and the
  selected `250ms` read, write, lease, and release budgets are accepted. The
  feature remains disabled at staging revision 50 and in production; Task 9
  Steps 3-6 remain unchecked. The final staging selector is committed and
  pushed on Helm main as `215b7d0`. Task 9 Step 3 is the next separately
  controlled action.

If the final exact-code benchmark passes, check Task 9 Step 2 and record image
digest, staging revision, selected budgets, sanitized distributions,
compressed/decoded byte totals, Redis counters, cleanup, and unchanged
production state. Then proceed to Task 9 Step 3. If it fails, keep Task 9 Steps
2-6 unchecked, keep the feature disabled, record the blocker, and stop.

---

### Task 14: Make Startup Collapse And Redis Failures Diagnosable

**Files:**
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_cache_test.go`
- Modify: `backend/internal/teamusage/prewarm_metrics.go`
- Modify: `backend/internal/teamusage/prewarmer.go`
- Modify: `backend/internal/teamusage/prewarmer_test.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm_test.go`
- Modify: current plan and current design spec with sanitized evidence only.

**Confirmed root causes and evidence limits:**

- A Pod that loses the startup `SET NX` race returns `nil`, and lifecycle
  reporting therefore records four `startup/success` observations rather than
  a closed `lease_busy` outcome.
- A successful startup compare-deletes its coordinator lease, so a staggered
  Pod can become a second startup owner inside the intended six-minute
  deployment-wide collapse window.
- The retained `16` source observations per Pod have no lifecycle/timestamp
  breakdown and may include the first 60-second moving/recovery/historical
  tick. They do not prove two complete startup source cycles.
- The retained Redis metrics collapse command deadline, caller cancellation,
  network, Redis command, validation, and decode/reference failures into
  `error`. The old Pods and raw metrics are gone, so the `250ms` budget remains
  the strongest hypothesis, not a proven cause.

- [x] **Step 1: Add RED multi-instance and error-class tests**

Use two independent `Prewarmer` instances with separate providers and metrics
but one shared Redis store, namespace, provider binding, and four-timezone
allowlist. Block the first source while it owns startup coordination. Require
the concurrent loser to make zero source calls and report `lease_busy`, not
success. After the owner completes, start a staggered third instance within the
six-minute TTL and require it to remain `lease_busy`; exactly one startup owner
may be observed.

Add closed Redis error classes without raw error strings or unbounded labels:
`validation`, `caller_canceled`, `command_deadline`, `network_timeout`,
`network_error`, `redis_command`, and `decode_or_reference`. Tests must
distinguish a pre-canceled parent from a child command deadline, normal manifest
miss from an error, normal lease contention from an error, and validation
rejection without calling the store. The existing Redis pool pending, wait, and
timeout metrics remain the source for pool-phase correlation because go-redis
does not expose its internal pool-timeout sentinel as a public error contract.

- [x] **Step 2: Implement the minimal coordinator and telemetry correction**

Represent startup lease contention as an explicit closed lifecycle result.
Retain the successful startup coordinator marker until its TTL expires; release
it only on failure or cancellation so recovery can retry. Keep all token checks,
TTL values, provider/version/allowlist dimensions, source concurrency, segment
leases, and fallback behavior unchanged.

Record one additional bounded Redis error-class counter at the live error site
before canceling the child command context. Do not expose raw errors, Redis
keys, tokens, provider data, or user data. Do not change the accepted command
budgets in this task.

- [x] **Step 3: Verify and review exact code**

Run focused RED/GREEN tests, repeated multi-instance tests, race tests for
`teamusage` and `telemetry`, the full backend suite, `go vet ./...`, and
`go build ./...`. Commit with:

```bash
git commit -m "fix(teamusage): make prewarm startup gates deterministic"
```

Generate a task-scoped review package and resolve every Critical/Important
finding before publishing an image.

  **Completed local verification and review (2026-07-22):** The multi-instance
  startup and Redis error-class RED tests failed only on the missing contracts.
  GREEN passed, including 50 repeated startup-collapse runs, focused server,
  Team Usage, and telemetry tests, race tests for all three packages, the full
  backend suite, `go vet ./...`, `go build ./...`, and diff checks. Initial
  review found three Important telemetry gaps: the real reporter rejected
  `lease_busy`, publication double-classified generation failures, and an
  immutable claim miss lacked a closed class. Commit `16529b18` fixed all three
  with RED/GREEN coverage. Re-review reported zero Critical, Important, or Minor
  findings. No staging image was published and no runtime configuration changed.

- [x] **Step 4: Rebuild and repeat the feature-disabled Redis benchmark**

Publish an immutable staging image for the reviewed Task 14 head, deploy it
with the feature absent/disabled, and repeat Task 13's exact 100-sample seven-
class benchmark. Any command, transport, INFO, cleanup, eviction, or rejected-
connection error blocks enablement. Do not change a budget from this run unless
a later feature-enabled replay proves the current value insufficient.

  **Completed exact-code replay (2026-07-22):** Exact reviewed head
  `3d533710` was published only as immutable staging image
  `staging-3d533710b8c48a76be1a92389482e1e80ca66317`. The remote index digest is
  `sha256:9865d3e2039999ac0f6b5ae3dcd2b9bc26bdda89a521994e1388641dd3bc1038`;
  its amd64 and arm64 manifests are respectively
  `sha256:0e2773ce083bafec17e5bcd8c9a50c7002de3f9dc951ded734c194832311700e`
  and
  `sha256:34d6836640ac6dedce81715f6aed453823e52aabdd42f3a4035f09e9b6d85ed0`.
  The paused and restore-enabled disabled rollouts completed at staging
  revisions 53 and 54. Revision 54 ran one ready Pod on the exact image with
  prewarm environment entries absent and HTTP 200 live/readiness; production
  remained revision 69 on `v0.1.0-preview.73`, ready and unchanged.

  The exact Task 13 package-native harness was restored outside the repository,
  passed its local overlay check, and ran as a static amd64 binary in the
  feature-disabled Pod. The smoke passed. The full replay completed 100 samples
  with zero errors in each ordered class: current write, segment write,
  five-lease manifest publication, full-generation MGET, four-lane request MGET,
  lease acquire, and lease release. Their p99 values were `13.442`, `19.416`,
  `12.600`, `73.590`, `50.473`, `11.848`, and `11.847` milliseconds. Every
  selected budget remained `250ms`. Transport, INFO, and cleanup errors were
  zero; the final synthetic key count and eviction/rejected-connection deltas
  were zero. The Pod binary was deleted before enablement. Step 5 is the next
  controlled action.

- [ ] **Step 5: Run one pre-ticker two-Pod diagnostic replay**

Enable only staging with two replicas and the approved four timezones. Capture
per-Pod metric baselines before enablement and evaluate startup before the first
60-second scheduled tick. Require exactly one startup owner, one busy replica,
four complete manifests within five seconds of the first complete manifest,
zero Relay errors, zero Redis errors, and zero Redis error-class deltas. Capture
Redis pending, wait, and timeout deltas alongside any command deadline.

  **Failed deterministic replay (2026-07-22):** The exact revision-54 image was
  atomically enabled only in staging at revision 55 with two fresh Pods. The
  earliest successful scrapes came `14.716s` and `5.600s` after their respective
  container starts. At those scrapes, Redis operation/error-class and pool
  pending/wait/timeout counters were zero on both Pods. One Pod became the exact
  owner and later recorded four `startup/success` labels and 16 source
  observations. The other recorded four `startup/lease_busy` labels and zero
  source observations during startup, satisfying the corrected ownership
  contract.

  The timing gate failed. The first complete manifest was created at
  `12:15:42.532Z` and the last at `12:16:33.468Z`, a `50.936s` cohort rather
  than at most five seconds. The lease-busy Pod's first successful scrape at
  `12:15:27.600Z` already contained all four `startup/lease_busy` labels, but no
  ticker-construction or first-tick timestamp was retained. Because lifecycle
  reporting still runs after those counters are written, the pre-ticker gate is
  unproven. The final scrape recorded moving/recovery/historical cycles and
  eight source observations after the loser's zero-source startup. It recorded
  exactly two
  Redis operation errors, both closed `command_deadline` classes: one
  `lease_acquire` and one `manifest_read`. Relay non-2xx and source-error counts
  were zero. Bounded scrapes recorded zero pool pending values and zero pool
  wait-count, wait-duration, and timeout deltas; they cannot exclude pressure
  between scrapes.

  No API acceptance round ran and no Team Usage business key was deleted.
  Atomic upgrade restored the exact image with the feature absent, one replica,
  and HTTP 200 live/readiness at staging revision 56. The failure-to-rollback
  duration was not retained separately, so this ledger does not claim a timed
  fail-fast proof. Production remained revision 69 on `v0.1.0-preview.73`, ready
  and unchanged. Step 5 and Task 9 Steps 3-6 remain unchecked.

  **Guarded replacement replay (2026-07-23):** One later authorized replay
  completed enabled revision 61, but its feed closed before the controller
  stopped marker/observer processing. The resulting observer failure overlapped
  rollback and cannot establish a product result. The guardian performed the
  only rollback and produced healthy disabled revision 62. Independent review
  classified the attempt as an operational failure. Step 5 and Task 9 Steps
  3-6 remain unchecked; no third replay is authorized.

On any failure, disable staging immediately, keep Task 9 Steps 3-6 unchecked,
record only sanitized operation IDs, closed outcomes, counts, and durations,
and stop. If a Redis error recurs, its closed class and pool deltas define the
next root-cause task; do not guess or alter the budget in this task. If the
replay passes, record that Task 9 Step 3 may restart from a fresh empty-key
round, but do not start Step 4 in the same replay.
