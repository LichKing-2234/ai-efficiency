# Team Usage Segmented Timezone Prewarm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the Relay directory, current-stats, and aggregate-trend chain from eligible cold Team Usage requests by prewarming exact timezone-specific source segments into Redis while preserving PR #192 response and authorization behavior.

**Architecture:** A background `teamusage.Prewarmer` resolves the current primary Relay provider, fetches one provider-wide current-stats roster and three immutable trend segments per configured timezone, then publishes a manifest last. Request-time code first resolves the current representative scope and Relay mappings, reads only a matching provider/version/timezone/anchor generation, intersects it with the authorized Relay IDs, and otherwise executes PR #192's exact scope-origin fallback.

**Tech Stack:** Go 1.24, Gin, Ent, go-redis v9, miniredis, Prometheus client_golang, zap, Sub2API HTTP APIs, Docker Buildx, GHCR, Helm, Kubernetes.

**Status:** Task 1 source and capacity gates passed. The corrected 20-call
ledger, exact staging image, and cleanup evidence are recorded; the correction
commit is pending. Tasks 2-9 remain blocked until Step 6 is checked.

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

- [ ] **Step 6: Commit the POC decision**

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

- [ ] **Step 1: Write RED interface and HTTP contract tests**

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

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/relay -run 'ProviderWide|DirectoryContract|CurrentStatsContract|TeamTrendBatch' -count=1 -v
```

Expected: FAIL because the provider-wide interfaces and bounded source methods do not exist and existing directory/stats reads are unbounded.

- [ ] **Step 3: Implement the minimal provider capabilities**

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

- [ ] **Step 4: Verify Relay behavior**

```bash
cd backend
gofmt -w internal/relay
go test ./internal/relay -count=1
go test -race ./internal/relay -count=1
go vet ./internal/relay
```

Expected: all pass; tests prove no real identity or raw body is logged or returned by the provider-wide interfaces.

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Write RED table tests for configuration, dates, labels, and composition**

Tests must cover the exact four defaults, trim/dedup/order, invalid zone, fifth zone, current local anchors, exact today/7d/30d shapes, custom-range rejection, `AddDate` semantics, LA/Berlin spring/fall rejection, opaque label validation, raw-hour retention, first-10-character coalescing, independent 6d/29d histories, sparse-user zero contribution, nil-token propagation, `1e-9` cost boundary, duplicate/order/coverage validation, and per-source/composed 5,000-user rejection.

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1 -v
```

Expected: FAIL with missing prewarm model symbols.

- [ ] **Step 3: Implement the pure domain model**

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

- [ ] **Step 4: Verify the pure model and race safety**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_model*.go
go test ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1
go test -race ./internal/teamusage -run 'Prewarm(Timezone|Window|Split|Segment|Compose)' -count=1
```

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Write RED Redis primitive and cache tests**

Tests must prove ordered MGET miss positions, token-checked Lua publication, token-checked release, namespace/schema/provider/version/timezone-digest/anchor/class/generation isolation, 512-byte key-reference rejection, immutable write-before-manifest order, reader use of one manifest only, malformed/oversized/hard-expired rejection, old-anchor in-flight readability, and Redis fail-open outcomes.

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1 -v
```

Expected: FAIL because batch reads, atomic publish, and prewarm cache do not exist.

- [ ] **Step 3: Implement bounded cache primitives and TTL relationships**

Introduce a narrow extension instead of enlarging every existing fake:

```go
type BatchStore interface {
	Store
	MGet(context.Context, ...string) ([][]byte, error)
	SetIfLeaseOwned(context.Context, string, string, string, []byte, time.Duration) (bool, error)
}
```

Use `movingFresh=75s`, `movingHard=4m`, `movingValueTTL=6m`, `historyFresh=25h`, `historyHard=49h`, `historyValueTTL=50h`, and `manifestTTL=3m`. Every reader validates logical hard expiry in addition to Redis TTL. The manifest expires before the earliest moving hard expiry; values outlive manifest discovery plus the 44-second maximum backend request, and prior-anchor moving values remain available long enough for a resolved request to finish.

- [ ] **Step 4: Verify Redis behavior**

```bash
cd backend
gofmt -w internal/readcache internal/teamusage/prewarm_cache*.go
go test ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1
go test -race ./internal/readcache ./internal/teamusage -run 'MGet|SetIfLeaseOwned|PrewarmCache|PublishLast' -count=1
```

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Write RED source and scheduler tests**

Tests must prove full deterministic directory exhaustion precedes 500-ID stats chunks, exact roster equality/digest, one shared current-stats generation, independent histories, deployment-wide source concurrency two across directory pages, stats chunks, trend segments, and partial-today calls, token-checked coordinator and two slot leases, process-local concurrency two, skipped overlapping 60-second ticks, deterministic `[0,30m)` jitter, split-unsafe no-source behavior, startup recovery of only missing/hard-expired values, stale-but-hard-valid retention, provider-version cancellation, lost-lease no-publish, and one timezone failure not invalidating another.

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1 -v
```

Expected: FAIL with missing source and lifecycle types.

- [ ] **Step 3: Implement bounded source orchestration**

`BuildCurrentStats` requires the new provider-wide usage capability, rejects 5,000 IDs, chunks at 500, validates exact cross-chunk coverage, sorts by ID, and hashes the length-delimited roster. `FetchSegment` calls the provider-wide trend capability with `limit=5000`, validates before writing, and never filters to a request scope.

- [ ] **Step 4: Implement the scheduler and distributed coordination**

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

- [ ] **Step 5: Verify lifecycle behavior**

```bash
cd backend
gofmt -w internal/teamusage/prewarm_source*.go internal/teamusage/prewarmer*.go
go test ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1
go test -race ./internal/teamusage -run 'Prewarm(Source|Moving|Historical|Startup|Lease|Concurrency)' -count=1
```

- [ ] **Step 6: Commit**

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

- [ ] **Step 1: Write RED request-path tests**

Tests must prove authorization and mapping resolution happen before Redis; full hits make no Relay call; eligible scopes above the existing `fullScopeCap=500` can use provider-wide prewarm facts; custom/unconfigured/DST/provider-version/anchor/schema/Redis failures use the exact existing lane fallback; authorized roster absence falls back; sparse complete trend omission yields zero; current scope-version change discards projection; partial missing today fetches only `D..D/hour` through the shared global source slots and never history; partial Relay failure falls back to the full exact range; Summary/Trend/Members/Organization/Overview outputs, cursors, freshness, and response-cache keys remain byte-equivalent.

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmReader|PrewarmFallback|PrewarmPartialToday|SharedOrigin|RangeCompletion' -count=1 -v
```

Expected: FAIL because `Service` has no prewarm reader and current cold loads always use the scope-origin source.

- [ ] **Step 3: Implement prewarm-first loading behind the existing origin cache**

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

- [ ] **Step 4: Implement partial-today recovery without a completed Pod cache**

When current stats and required history are hard-valid but today is not, coordinate only the in-flight today request, fetch exactly `D..D/hour`, validate and compose it, optionally publish a complete new generation while the lease is owned, then discard the completed local value. If any step fails, call the complete PR #192 loader rather than a per-segment approximation.

The local flight key contains provider ID/version, timezone digest, anchor, and `today_hour`; its callback returns the request result to current waiters only. It never writes the completed result into a process map after the callback returns.

- [ ] **Step 5: Verify all Team Usage behavior**

```bash
cd backend
gofmt -w internal/teamusage
go test ./internal/teamusage -count=1
go test -race ./internal/teamusage -count=1
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/teamusage
git commit -m "perf(teamusage): read authorized prewarmed origins"
```

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

- [ ] **Step 1: Write RED configuration, dependency, lifecycle, and metric tests**

Tests must prove disabled default, exact default list, env binding, invalid-list fail-open behavior, no prewarmer when disabled/empty/Redis unavailable/provider unsupported, startup after dependencies without blocking HTTP, graceful stop before Redis close, `MinIdleConns>=4`, one-second dial/pool bounds, two-second Redis transport read/write ceilings for later measured large-value contexts, existing cache 100ms contexts, and preinitialized closed-enum metric labels with no identity or raw-key label.

- [ ] **Step 2: Run RED tests**

```bash
cd backend
go test ./internal/config ./internal/telemetry ./internal/handler ./cmd/server \
  -run 'TeamUsagePrewarm|RedisClientOptions|CacheMetrics|RouterDependencies' -count=1 -v
```

- [ ] **Step 3: Implement disabled-by-default wiring**

Bind `AE_TEAM_USAGE_PREWARM_ENABLED` and comma-separated `AE_TEAM_USAGE_PREWARM_TIMEZONES`. Validate through `NormalizePrewarmTimezones`; invalid optimization config logs a bounded error and constructs no reader/prewarmer instead of terminating the core service. Configure the shared go-redis transport with one-second dial/pool timeouts, `MinIdleConns=4`, and two-second read/write ceilings; existing caches keep their 100ms command contexts, while prewarm read/write contexts remain separately configurable for Task 9 measurement. Pass the reader through `RouterOptions` and `ServiceOptions`. Start the prewarmer after provider runtime/cache construction, retain its cancel function, and stop it before Redis shutdown.

```go
type TeamUsagePrewarmConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Timezones []string `mapstructure:"timezones"`
}
```

Defaults are `false` and the four design timezones. Compose examples expose the same values without enabling the feature.

- [ ] **Step 4: Add bounded-cardinality telemetry**

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

- [ ] **Step 5: Verify configuration and full backend**

```bash
cd backend
gofmt -w internal/config internal/telemetry internal/handler cmd/server
go test ./internal/config ./internal/telemetry ./internal/handler ./cmd/server -count=1
go test ./... -count=1
go test -race ./internal/relay ./internal/readcache ./internal/teamusage ./internal/config ./internal/telemetry ./internal/handler ./cmd/server -count=1
go vet ./...
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add backend deploy
git commit -m "perf(backend): wire segmented Team Usage prewarm"
```

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

- [ ] **Step 1: Add dashboard contract tests before editing JSON**

Extend `backend/internal/telemetry/dashboard_contract_test.go` to require prewarm cycle duration/outcome, last-success, Redis/source duration, generation bytes, request outcome/fallback, and skipped tick/lease panels with bounded label groupings.

- [ ] **Step 2: Run RED, update dashboard, then run GREEN**

```bash
cd backend
go test ./internal/telemetry -run 'Dashboard' -count=1 -v
```

Expected RED: required prewarm expressions are absent. Add panels and operator queries, then rerun and expect PASS.

- [ ] **Step 3: Run final local verification**

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

- [ ] **Step 4: Run broad branch review and resolve all Critical/Important findings**

Generate the review package from `63f29f64` to `HEAD`, dispatch an independent code reviewer, apply accepted findings one at a time, rerun affected tests, and repeat until Critical and Important are empty.

- [ ] **Step 5: Commit documentation and review corrections**

```bash
git add deploy/observability docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md
git commit -m "docs(teamusage): add prewarm operations guidance"
```

Do not update `docs/architecture.md` yet; the feature is still disabled and is not current runtime.

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

- [ ] **Step 1: Publish and deploy an immutable staging image with the feature disabled**

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

- [ ] **Step 2: Benchmark staging Redis with synthetic maximum-safe values**

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
