# Team Usage Redis Daily Prewarm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evaluate whether provider-wide UTC-hour facts can safely move standard Team Usage aggregation off the request path. The evaluated approach failed its mandatory semantic-equivalence gate.

**Architecture:** Rejected UTC-hour candidate: a primary-provider background prewarmer would fetch one current-stats snapshot and one bounded UTC-hour history, then reconstruct browser-timezone days. The staging POC proved that the current Relay hourly labels cannot support that reconstruction contract.

**Tech Stack:** Go 1.24, Gin, Ent, go-redis v9, miniredis, Prometheus client_golang, zap, Sub2API HTTP APIs, Kubernetes, Helm, Docker Buildx, GHCR.

**Status:** **Rejected historical plan.** Replaced by `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`; the replacement is approved but blocked on its segmented-source POC. Every checkbox and the remaining body below are retained as historical evidence and must not be executed.

### Task 1 Gate Evidence

| Gate | Measured | Result |
| --- | ---: | --- |
| Relay duration | 10.081 s | Pass (`< 25 s`) |
| Hourly source body | 7,464,074 bytes | Pass (`< 33,554,432`) |
| Decoded hourly points | 45,331 | Pass (`< 1,000,000`) |
| Unique users | 364 | Pass (`< 5,000`) |
| Probe peak RSS | 59,375,616 bytes | Pass (`< 201,326,592`) |
| Largest serialized UTC-day shard | 157,531 bytes | Pass (`< 1,048,576`) |
| Total serialized 32-day generation | 3,165,743 bytes | Pass (`< 16,777,216`) |
| `UTC` reconstructed vs direct daily | Mismatch | **Fail** |
| `America/Los_Angeles` reconstructed vs direct daily | Mismatch | **Fail** |
| `Europe/Berlin` reconstructed vs direct daily | Mismatch | **Fail** |

The failure is semantic, not capacity-related. Sub2API applies the request
timezone while deriving range boundaries, but `GetUserUsageTrend` groups with
`TO_CHAR(u.created_at, format)` without applying that timezone in SQL. Hourly
labels therefore cannot be treated as UTC facts for cross-timezone
reconstruction. No production limit constants are locked by this failed gate.

## Global Constraints

- Do not modify Sub2API source or require a coordinated Sub2API release.
- Do not add a Pod-local completed-result cache; process-local state may only coordinate in-flight work.
- Prewarm only the current enabled primary Relay provider.
- Authorization, representative scope, and Relay identity reconciliation remain authoritative in the local database on every request.
- Never cache names, emails, authorization decisions, credentials, tokens, raw usage logs, or raw Relay responses in prewarm values.
- Preserve existing Summary, Trend, Members, Organization, and Overview DTOs, cursors, response-cache keys, freshness fields, and stale-if-error behavior.
- Preserve the existing 5,000-user aggregate-trend truncation guard.
- Whole-hour timezone offsets may use UTC-hour reconstruction; any effective non-hour offset must use the exact timezone-specific scope-origin fallback.
- The canonical history is at most 32 UTC day shards and includes the UTC-12 through UTC+14 boundary margin around a 30-local-day request.
- Refresh current stats and the newest 48 UTC hours every 60 seconds; run one jittered full 32-day historical correction per UTC day.
- Publish the manifest only after every referenced value is written and validated; generation values must outlive their manifest.
- Redis failure remains fail-open through the current authoritative scoped path.
- Keep authenticated POC and staging artifacts under `/tmp`; remove credentials, bodies, user lists, tokens, and unredacted Redis values after each run.
- Use only synthetic identities such as `alice@example.com` in tests, fixtures, specs, plans, and logs.
- Production remains unchanged until a separately approved production release.

The UTC-hour-specific constraints and file map below are retained only as the
rejected candidate's execution record. They are not authorization to continue.

## File Map

- `backend/internal/relay/provider.go`: exported provider-wide hourly-history capability and result metadata.
- `backend/internal/relay/sub2api.go`: Relay directory page size 1,000.
- `backend/internal/relay/sub2api_team_trend_batch.go`: validated provider-wide hourly aggregate adapter and source-size metadata.
- `backend/internal/readcache/store.go`: optional Redis multi-get interface and implementation.
- `backend/internal/teamusage/prewarm_model.go`: coverage, shard, manifest, reconstruction, and validation domain logic.
- `backend/internal/teamusage/prewarm_cache.go`: Redis key contract, publish-last generation writes, bounded reads, and lease operations.
- `backend/internal/teamusage/prewarm_source.go`: primary-provider directory, stats, and hourly-history source builder.
- `backend/internal/teamusage/prewarmer.go`: startup, moving-edge, and historical scheduler lifecycle.
- `backend/internal/teamusage/origin.go`: authorized prewarm read path and retained exact fallback.
- `backend/internal/teamusage/service.go`: service dependencies, primary-provider lookup reuse, and stats chunk size 500.
- `backend/internal/config/config.go`: deployment feature flag.
- `backend/internal/handler/router.go`, `backend/internal/handler/team_usage.go`: pass the prewarm reader into the Team Usage service.
- `backend/cmd/server/main.go`: Redis pool options, prewarm construction, startup, and shutdown.
- `backend/internal/telemetry/team_usage_prewarm.go`: bounded-cardinality prewarm metrics.
- `backend/cmd/server/cache_metrics.go`: manifest/current/history cache recorders.
- `deploy/observability/grafana/ai-efficiency-performance.json`, `deploy/observability/README.md`: dashboard and operator queries.
- `docs/architecture.md`: current runtime boundary, updated only after implementation is active.

---

### Task 1: Run The Staging POC Hard Gate

**Files:**
- Temporary create and delete: `/tmp/ae-team-usage-prewarm-poc.go`
- Temporary create and delete: `/tmp/ae-team-usage-prewarm-poc`
- Modify after a sanitized result exists: `docs/superpowers/specs/2026-07-21-team-usage-redis-daily-prewarm-design.md`
- Modify after a sanitized result exists: `docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md`

**Interfaces:**
- Consumes: staging Sub2API base URL and admin API key supplied only through process environment.
- Produces: a sanitized `POCDecision` record containing `relay_duration`, `source_bytes`, `decoded_points`, `unique_users`, `peak_rss`, `max_day_shard_bytes`, `total_shard_bytes`, and comparison results for `UTC`, `America/Los_Angeles`, and `Europe/Berlin`.
- Gate constants: Relay duration `< 25s`; source body `< 32 MiB`; decoded points `< 1,000,000`; unique users `< 5,000`; probe peak RSS `< 192 MiB`; each serialized day shard `< 1 MiB`; all 32 day shards together `< 16 MiB`; token totals exact; actual-cost absolute error `<= 1e-9`.

- [x] **Step 1: Verify the audit target and isolation before reading credentials**

Run:

```bash
gh pr view 192 --json headRefOid,headRefName,baseRefName,state
kubectl -n la3-ai-efficiency-prod get deploy ai-efficiency-staging -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
helm -n la3-ai-efficiency-prod status ai-efficiency-staging
helm -n la3-ai-efficiency-prod status ai-efficiency-prod
```

Expected: PR #192 head is `627a7123d98aee37dd04fd5da2198234cfd003f0`, staging uses its immutable image at revision 44 or a later explicitly recorded equivalent baseline, and production has no pending rollout. If staging has moved, record the exact new image/revision before continuing; do not alter production.

- [x] **Step 2: Create a throwaway probe with no embedded identity or secret**

The `/tmp` probe must:

```go
type point struct {
	Date       string   `json:"date"`
	UserID     int64    `json:"user_id"`
	Tokens     *int64   `json:"tokens"`
	ActualCost *float64 `json:"actual_cost"`
}

type result struct {
	RelayDurationMS int64            `json:"relay_duration_ms"`
	SourceBytes     int              `json:"source_bytes"`
	DecodedPoints   int              `json:"decoded_points"`
	UniqueUsers     int              `json:"unique_users"`
	DayShardBytes   map[string]int   `json:"day_shard_bytes"`
	TotalShardBytes int              `json:"total_shard_bytes"`
	Comparisons     map[string]string `json:"comparisons"`
}
```

Use `http.NewRequestWithContext`, set only `X-API-Key` from `AE_POC_ADMIN_API_KEY`, call `/api/v1/admin/dashboard/users-trend`, limit reads to `32 << 20`, reject an exactly 5,000-user result, parse hourly dates with layout `2006-01-02 15:00` in UTC, and group each point after `hour.In(location)` by local `2006-01-02`. Fetch direct `granularity=day` results for the three comparison timezones, filter both sides to the same 30 local dates, compare tokens exactly and costs with `math.Abs(left-right) <= 1e-9`, and print only the `result` JSON. Do not print URLs, headers, IDs, row contents, or response bodies.

- [x] **Step 3: Build and run the probe under a measured process**

Run with the canonical 30-day comparison window plus one UTC date on each side:

```bash
go build -o /tmp/ae-team-usage-prewarm-poc /tmp/ae-team-usage-prewarm-poc.go
/usr/bin/time -l env \
  AE_POC_BASE_URL="$AE_POC_BASE_URL" \
  AE_POC_ADMIN_API_KEY="$AE_POC_ADMIN_API_KEY" \
  AE_POC_START_DATE="2026-06-21" \
  AE_POC_END_DATE="2026-07-20" \
  /tmp/ae-team-usage-prewarm-poc \
  > /tmp/ae-team-usage-prewarm-poc-result.json \
  2> /tmp/ae-team-usage-prewarm-poc-time.txt
jq -e '
  .relay_duration_ms < 25000 and
  .source_bytes < 33554432 and
  .decoded_points < 1000000 and
  .unique_users < 5000 and
  .total_shard_bytes < 16777216 and
  ([.day_shard_bytes[]] | max) < 1048576 and
  ([.comparisons[]] | all(. == "exact"))
' /tmp/ae-team-usage-prewarm-poc-result.json
```

Expected: `jq` exits 0; `/usr/bin/time -l` reports maximum resident set size below 192 MiB. A response at 5,000 users, a body reaching 32 MiB, or a comparison mismatch is a hard failure, not a warning.

Actual: build and measured execution completed, and the probe's internal request-count guard confirmed exactly four read-only GETs. The capacity gates passed, but all three comparison values were `mismatch`; `jq` therefore failed as required.

- [x] **Step 4: Record the decision and enforce the stop condition**

For a pass, add a sanitized table to the design spec and this plan with only aggregate measurements and lock these production constants:

```go
const (
	prewarmSourceMaxBytes       = 32 << 20
	prewarmDecodedPointMaxCount = 1_000_000
	prewarmDayShardMaxBytes     = 1 << 20
	prewarmCurrentStatsMaxBytes = 2 << 20
	prewarmGenerationMaxBytes   = 16 << 20
	prewarmUniqueUserLimit      = 5_000
)
```

If any gate fails, mark this plan `Blocked by UTC-hour POC`, leave every later checkbox unchecked, and write a replacement design using daily buckets for an explicit configured common-timezone list. Do not implement Tasks 2-10 on a failed UTC-hour POC.

Decision: blocked. The design spec records the failed semantic gate and proposes direct `granularity=day` buckets for the explicitly configured initial set `UTC`, `Asia/Shanghai`, `America/Los_Angeles`, and `Europe/Berlin`. Other timezones remain request-driven. That replacement awaits user approval and a new plan.

- [x] **Step 5: Delete all authenticated artifacts before committing evidence**

Run:

```bash
rm -f /tmp/ae-team-usage-prewarm-poc.go \
  /tmp/ae-team-usage-prewarm-poc \
  /tmp/ae-team-usage-prewarm-poc-result.json \
  /tmp/ae-team-usage-prewarm-poc-time.txt
unset AE_POC_BASE_URL AE_POC_ADMIN_API_KEY
git status --short
```

Expected: no temporary probe remains; only the spec and plan contain sanitized aggregate evidence.

- [x] **Step 6: Commit the failed gate evidence**

```bash
git add docs/superpowers/specs/2026-07-21-team-usage-redis-daily-prewarm-design.md \
  docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md
git commit -m "docs(teamusage): record failed Redis prewarm POC"
```

---

### Task 2: Reduce Existing Relay Directory And Stats Calls

**Files:**
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/range_completion_test.go`
- Modify: `backend/internal/teamusage/shared_origin_test.go`

**Interfaces:**
- Produces: `const teamUsageStatsChunkSize = 500` in package `teamusage`.
- Preserves: `TeamUsageSummaryProvider.GetBatchUserUsageStats(ctx, userIDs, params)` and all response contracts.

- [ ] **Step 1: Write failing Relay pagination tests**

Add a server test that returns two pages and records request queries:

```go
if got := requests[0].URL.Query().Get("page_size"); got != "1000" {
	t.Fatalf("page_size = %q, want 1000", got)
}
if got := requests[1].URL.Query().Get("page"); got != "2" {
	t.Fatalf("second page = %q, want 2", got)
}
```

Cover both `ListUsers` and the paginated fallback in `findUserInAdminList` so neither path silently stays at 200.

- [ ] **Step 2: Write failing 1,001-user stats chunk tests**

Replace the 205-user chunk expectation with a direct boundary regression:

```go
stats, err := service.loadTeamUsageStats(ctx, provider, relayIDs(1001), nil, relay.TeamUsageSummaryParams{})
if err != nil {
	t.Fatal(err)
}
if got, want := provider.batchSizes(), []int{500, 500, 1}; !reflect.DeepEqual(got, want) {
	t.Fatalf("batch sizes = %v, want %v", got, want)
}
if len(stats) != 1001 {
	t.Fatalf("stats = %d, want 1001", len(stats))
}
```

- [ ] **Step 3: Run the RED tests**

Run:

```bash
cd backend
go test ./internal/relay -run 'ListUsers.*PageSize|FindUser.*PageSize' -count=1
go test ./internal/teamusage -run 'LoadTeamUsageStats.*Chunk' -count=1
```

Expected: Relay tests observe `200`; Team Usage observes 100-sized chunks.

- [ ] **Step 4: Implement the minimal constants and substitutions**

```go
const sub2APIAdminUserPageSize = 1000

path := fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=%d", page, sub2APIAdminUserPageSize)
```

```go
const teamUsageStatsChunkSize = 500

for _, chunk := range chunkInt64s(uniqueRelayUserIDs, teamUsageStatsChunkSize) {
	// Existing validation remains unchanged.
}
```

- [ ] **Step 5: Run focused and package tests**

```bash
cd backend
go test ./internal/relay ./internal/teamusage -run 'PageSize|LoadTeamUsageStats|SharedOriginFourConcurrentLanes' -count=1
go test ./internal/relay ./internal/teamusage -count=1
```

Expected: PASS; the 205-user shared-origin test now expects one stats POST, while 1,001 users produce exactly 500/500/1.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go \
  backend/internal/teamusage/service.go backend/internal/teamusage/range_completion_test.go \
  backend/internal/teamusage/shared_origin_test.go
git commit -m "perf(teamusage): enlarge Relay usage batches"
```

---

### Task 3: Make The Shared Redis Pool Reliable

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`
- Test: `backend/internal/readcache/store_test.go`

**Interfaces:**
- Produces: shared go-redis transport options with `MinIdleConns=4`, `DialTimeout=1s`, `ReadTimeout=2s`, `WriteTimeout=2s`, `PoolTimeout=1s`, `MaxRetries=-1`, `DialerRetries=1`, and `ContextTimeoutEnabled=true`.
- Preserves: each existing cache's own 100 ms `context.WithTimeout` command budget.

- [ ] **Step 1: Change the runtime-options test first**

```go
if options.MinIdleConns != 4 || options.DialTimeout != time.Second ||
	options.ReadTimeout != 2*time.Second || options.WriteTimeout != 2*time.Second ||
	options.PoolTimeout != time.Second {
	t.Fatalf("Redis pool options = %+v", options)
}
```

Retain the existing assertions for disabled command retries and context timeouts.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./cmd/server -run TestRedisClientOptions -count=1
```

Expected: FAIL because the client still uses 100 ms and one initial connection.

- [ ] **Step 3: Implement the transport budgets**

```go
return &redis.Options{
	Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB,
	MaxRetries: -1, DialerRetries: 1, MinIdleConns: 4,
	DialTimeout: time.Second, ReadTimeout: 2 * time.Second,
	WriteTimeout: 2 * time.Second, PoolTimeout: time.Second,
	ContextTimeoutEnabled: true,
}
```

- [ ] **Step 4: Prove small-cache contexts still win**

Add a delayed Redis test around an existing `OriginCache` or `readcache.RedisStore` caller and assert a 100 ms caller context returns before 300 ms even though the transport read timeout is 2 seconds.

- [ ] **Step 5: Run focused tests**

```bash
cd backend
go test ./cmd/server ./internal/readcache ./internal/teamusage -run 'RedisClientOptions|CommandTimeout' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/server/main.go backend/cmd/server/redis_runtime_test.go backend/internal/readcache/store_test.go
git commit -m "fix(backend): stabilize Redis pool expansion"
```

---

### Task 4: Add UTC-Hour Coverage And Reconstruction Domain Logic

**Files:**
- Create: `backend/internal/teamusage/prewarm_model.go`
- Create: `backend/internal/teamusage/prewarm_model_test.go`

**Interfaces:**
- Produces: `type PrewarmCoverage struct { StartUTCDate, EndUTCDate string }`.
- Produces: `func canonicalPrewarmCoverage(now time.Time) PrewarmCoverage`.
- Produces: `func requiredPrewarmDates(params OverviewParams) ([]string, error)`.
- Produces: `func supportsUTCWholeHourReconstruction(params OverviewParams) (bool, error)`.
- Produces: `func reconstructPrewarmOrigin(params OverviewParams, relayUserIDs []int64, stats map[int64]relay.TeamUserUsageStats, shards []prewarmDayShard) (*teamUsageScopeOrigin, error)`.
- Produces immutable JSON types `prewarmManifest`, `prewarmCurrentStatsEnvelope`, `prewarmDayShard`, and `prewarmHourFact` used by Task 5.
- Produces: `type PrewarmRefreshClass string` with `startup`, `moving`, and `historical` constants.
- Produces: `type PrewarmFallbackReason string` with the closed reasons consumed by Task 8.
- Produces: `type PrewarmGeneration struct { ProviderID int; ProviderVersion int64; RefreshClass PrewarmRefreshClass; CurrentStats prewarmCurrentStatsEnvelope; DayShards map[string]prewarmDayShard; Coverage PrewarmCoverage; CreatedAt time.Time; SourceStatus string }`.
- Produces a transport-neutral `PrewarmObserver` interface whose methods accept only fixed-enum strings, durations, byte/point counts, and timestamps; telemetry implements it in Task 9 without entering the domain model.

Use these exact cross-task contracts:

```go
const (
	PrewarmRefreshStartup    PrewarmRefreshClass = "startup"
	PrewarmRefreshMoving     PrewarmRefreshClass = "moving"
	PrewarmRefreshHistorical PrewarmRefreshClass = "historical"
)

const (
	PrewarmFallbackDisabled               PrewarmFallbackReason = "disabled"
	PrewarmFallbackCustomRange            PrewarmFallbackReason = "custom_range"
	PrewarmFallbackUnsupportedGranularity PrewarmFallbackReason = "unsupported_granularity"
	PrewarmFallbackNonHourOffset          PrewarmFallbackReason = "non_hour_offset"
	PrewarmFallbackManifestMiss           PrewarmFallbackReason = "manifest_miss"
	PrewarmFallbackCurrentStatsMiss       PrewarmFallbackReason = "current_stats_miss"
	PrewarmFallbackHistoryMiss            PrewarmFallbackReason = "history_miss"
	PrewarmFallbackInvalidGeneration      PrewarmFallbackReason = "invalid_generation"
	PrewarmFallbackRedisError             PrewarmFallbackReason = "redis_error"
	PrewarmFallbackMovingEdgeMiss         PrewarmFallbackReason = "moving_edge_miss"
)

type PrewarmObserver interface {
	ObserveCycle(refreshClass, outcome string, duration time.Duration)
	ObserveSource(responseBytes, decodedPoints, uniqueUsers int)
	ObserveGeneration(totalBytes int, dayShardBytes []int)
	RecordCache(component, outcome string)
	SetLastSuccess(refreshClass string, at time.Time)
	RecordFallback(reason string)
}
```

- [ ] **Step 1: Write RED coverage and standard-window tests**

```go
func TestCanonicalPrewarmCoverageSpansThirtyLocalDaysAndGlobalMargins(t *testing.T) {
	got := canonicalPrewarmCoverage(time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC))
	if got.StartUTCDate != "2026-06-20" || got.EndUTCDate != "2026-07-21" || got.DayCount() != 32 {
		t.Fatalf("coverage = %#v", got)
	}
}
```

Table-test today, seven-day, and thirty-day requests in UTC-12, UTC, and UTC+14 and assert `requiredPrewarmDates` never returns more than 32 ordered dates.

- [ ] **Step 2: Write RED timezone exactness tests**

Use `America/New_York` windows containing the 2026 spring-forward and fall-back transitions. Assert reconstructed daily totals equal the sum of their actual 23-hour and 25-hour local days. Assert `Asia/Kathmandu` and `Australia/Lord_Howe` return `supported=false` whenever the requested window observes a non-hour offset.

- [ ] **Step 3: Write RED sparse and nil-token tests**

```go
shard := prewarmDayShard{UTCDate: "2026-07-20", Users: map[int64][]prewarmHourFact{
	101: {
		{UTCStart: "2026-07-20T00:00:00Z", ActualCost: 1.25, TotalTokens: int64Ptr(10)},
		{UTCStart: "2026-07-20T03:00:00Z", ActualCost: 2.75, TotalTokens: nil},
	},
}}
```

Assert absent hours/users contribute zero only after shard validation, costs sum to 4, and a single nil token makes the reconstructed range token total nil.

- [ ] **Step 4: Run RED**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmCoverage|UTCWholeHour|ReconstructPrewarm' -count=1
```

Expected: compile failure because the domain types do not exist.

- [ ] **Step 5: Implement strict model validation and reconstruction**

Use `time.Parse("2006-01-02", date)`, `time.Parse(time.RFC3339, fact.UTCStart)`, and `time.LoadLocation(params.Timezone)`. Inspect the effective zone offset at every minute boundary in the bounded request interval and reject any offset where `offsetSeconds%3600 != 0`. Reject duplicate user/hour facts, non-increasing points, wrong UTC date, future hours, NaN/Inf/negative cost, negative tokens, coverage gaps, and more than 32 dates. Preserve nil tokens during aggregation and sort Relay IDs plus points before returning.

- [ ] **Step 6: Run GREEN plus repeated DST checks**

```bash
cd backend
go test ./internal/teamusage -run 'PrewarmCoverage|UTCWholeHour|ReconstructPrewarm' -count=20
```

Expected: PASS with stable ordering.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/teamusage/prewarm_model.go backend/internal/teamusage/prewarm_model_test.go
git commit -m "feat(teamusage): model UTC prewarm facts"
```

---

### Task 5: Add Redis MGET And The Publish-Last Cache Contract

**Files:**
- Modify: `backend/internal/readcache/store.go`
- Modify: `backend/internal/readcache/store_test.go`
- Create: `backend/internal/teamusage/prewarm_cache.go`
- Create: `backend/internal/teamusage/prewarm_cache_test.go`

**Interfaces:**
- Produces: `type MultiStore interface { Store; MGet(context.Context, ...string) (map[string][]byte, error) }`.
- Produces: `func (s *RedisStore) MGet(ctx context.Context, keys ...string) (map[string][]byte, error)`; missing keys are omitted from the result.
- Produces: `type PrewarmCacheOptions` with namespace, `ReadTimeout=1s`, write/release budgets, TTLs, clock, token source, and metrics.
- Produces: `type PrewarmLookupKey struct { ProviderID int; ProviderVersion int64; CoverageEndUTCDate string }`.
- Produces: `type PrewarmReadResult struct { Manifest prewarmManifest; CurrentStats prewarmCurrentStatsEnvelope; DayShards []prewarmDayShard; MissingMovingDates []string }`.
- Produces: `func NewPrewarmCache(store readcache.MultiStore, options PrewarmCacheOptions) (*PrewarmCache, error)`.
- Produces: `func (c *PrewarmCache) Read(ctx context.Context, key PrewarmLookupKey, dates []string) (*PrewarmReadResult, error)`.
- Produces: `func (c *PrewarmCache) Publish(ctx context.Context, candidate *PrewarmGeneration) error`.
- Produces: `func (c *PrewarmCache) TryAcquireCycleLease(...)` and token-checked release.

- [ ] **Step 1: Write RED MGET semantics tests**

```go
got, err := store.MGet(ctx, "present-a", "missing", "present-b")
if err != nil {
	t.Fatal(err)
}
if string(got["present-a"]) != "a" || string(got["present-b"]) != "b" {
	t.Fatalf("MGet = %#v", got)
}
if _, found := got["missing"]; found {
	t.Fatal("missing key must be omitted")
}
```

- [ ] **Step 2: Write RED manifest atomicity and generation-isolation tests**

Use miniredis plus a recording/failing store. Assert:

```go
// Values are written before the discoverable manifest.
if got[len(got)-1] != manifestKey {
	t.Fatalf("last SET = %q, want manifest %q", got[len(got)-1], manifestKey)
}
// A failed day-shard SET leaves the prior manifest byte-for-byte unchanged.
// One Read starts from one manifest and never mixes a newer generation.
```

Also assert key text contains only namespace plus SHA-256 digests, not provider IDs, user IDs, emails, or raw date lists.

- [ ] **Step 3: Write RED size, schema, stale, and MGET validation tests**

Cover the exact POC-locked bounds, missing current stats, missing historical shard, one missing moving shard, provider-version mismatch, expired manifest, stale-but-usable prior manifest, duplicate/out-of-day points, partial generation invisibility, and MGET cancellation at the one-second reader budget.

- [ ] **Step 4: Run RED**

```bash
cd backend
go test ./internal/readcache ./internal/teamusage -run 'MGet|PrewarmCache|Manifest' -count=1
```

Expected: compile failure for `MultiStore`, `PrewarmCache`, and the new types.

- [ ] **Step 5: Implement MGET and immutable generation keys**

`RedisStore.MGet` must convert each non-nil `interface{}` result to bytes, copy returned slices, omit Redis nils, and return an error for any unexpected value type. `PrewarmCache.Publish` must JSON-encode and size-check current stats plus every day shard, SET generation values with TTL, then SET the manifest as the sole commit point. Use generation IDs from `uuid.NewString` and digest all key dimensions with SHA-256.

- [ ] **Step 6: Implement bounded read and lease operations**

Read the computed manifest key once, validate it, then issue exactly one MGET for its current-stats key and required day keys. Return a typed miss/fallback reason instead of logging payload data. Use `readcache.Store.TryAcquireLease` and `ReleaseLease`; never poll full generation values while another cycle lease is live.

- [ ] **Step 7: Run GREEN and race tests**

```bash
cd backend
go test ./internal/readcache ./internal/teamusage -run 'MGet|PrewarmCache|Manifest' -count=20
go test -race ./internal/readcache ./internal/teamusage -run 'PrewarmCache|Manifest' -count=1
```

Expected: PASS; race output may contain only the known macOS linker warning.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/readcache/store.go backend/internal/readcache/store_test.go \
  backend/internal/teamusage/prewarm_cache.go backend/internal/teamusage/prewarm_cache_test.go
git commit -m "feat(teamusage): add Redis prewarm generations"
```

---

### Task 6: Build The Primary-Provider Prewarm Source

**Files:**
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api_team_trend_batch.go`
- Modify: `backend/internal/relay/sub2api_team_trend_batch_test.go`
- Create: `backend/internal/teamusage/prewarm_source.go`
- Create: `backend/internal/teamusage/prewarm_source_test.go`
- Modify: `backend/internal/teamusage/service.go`

**Interfaces:**
- Produces: `type ProviderUsageHourlyHistory struct { PointsByUser map[int64][]UsageTrendPoint; ResponseBytes int; DecodedPointCount int; UniqueUserCount int }`.
- Produces: `type ProviderUsageHourlyHistoryProvider interface { GetProviderUsageHourlyHistory(context.Context, TeamMemberTrendParams) (*ProviderUsageHourlyHistory, error) }`.
- Produces: `type PrewarmSourceEvidence struct { ResponseBytes, DecodedPointCount, UniqueUserCount, GenerationBytes int }`.
- Produces: `type prewarmSource struct` and `func newPrewarmSource(client *ent.Client, resolver ProviderResolver, observer PrewarmObserver) *prewarmSource`.
- Produces: `func buildPrewarmGeneration(ctx context.Context, binding primaryProviderBinding, previous *prewarmManifest, class PrewarmRefreshClass, now time.Time) (*PrewarmGeneration, PrewarmSourceEvidence, error)` on a `prewarmSource`.
- Preserves: `TeamMemberTrendProvider.GetUsageTrendForUsers` behavior and the 5,000-user truncation rejection.

- [ ] **Step 1: Write RED provider-wide adapter tests**

Serve a synthetic aggregate envelope with two users and out-of-request ordering. Assert `GetProviderUsageHourlyHistory` returns both users, response byte count, decoded row count, unique-user count, sorted points, and nil token values. Add rejection tests for exactly 5,000 unique users, 32 MiB body, duplicate user/hour, negative/NaN cost, negative tokens, invalid hour format, and trailing JSON.

- [ ] **Step 2: Write RED source-builder tests**

Use a provider implementing `UserDirectoryProvider`, `TeamUsageSummaryProvider`, and `ProviderUsageHourlyHistoryProvider`. Assert moving refresh performs one directory read, stats chunks no larger than 500, and one 48-hour history read; historical refresh requests the full 32-day UTC coverage. Assert current stats contain only validated Relay IDs and history remains provider-wide without directory identity fields.

- [ ] **Step 3: Write RED moving-edge reuse and cancellation tests**

Provide a valid prior manifest with historical day refs. Assert a moving cycle reuses every shard outside the latest 48-hour interval and replaces every UTC-day shard intersecting that interval, which is two dates at UTC midnight and otherwise may be three, plus current stats. Cancel after source fetch and assert no candidate is returned. Change the primary provider version between source resolution and publish validation and assert the candidate is rejected.

- [ ] **Step 4: Run RED**

```bash
cd backend
go test ./internal/relay ./internal/teamusage -run 'ProviderUsageHourly|PrewarmSource' -count=1
```

Expected: compile failure for the new provider capability and source builder.

- [ ] **Step 5: Implement the adapter without duplicating HTTP parsing**

Extend the existing validated `getTeamTrendBatch` result with response bytes and decoded point count. Have `GetUsageTrendForUsers` continue filtering to its requested IDs; have `GetProviderUsageHourlyHistory` return the validated complete map. Both methods must reject `UniqueUserCount == 5000` as potentially truncated.

- [ ] **Step 6: Implement the source builder**

Resolve the primary provider row through a package-level helper shared with `Service.resolvePrimaryProviderConfig`. Require all three capabilities, list users only for current-stats IDs, fetch stats in 500-ID chunks, fetch UTC hourly history at the moving or historical range, validate against the Task 1 constants, and build deterministic sorted envelopes. Do not place `relay.User` values in the generation.

- [ ] **Step 7: Run GREEN and packages**

```bash
cd backend
go test ./internal/relay ./internal/teamusage -run 'ProviderUsageHourly|PrewarmSource' -count=20
go test ./internal/relay ./internal/teamusage -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/relay/provider.go \
  backend/internal/relay/sub2api_team_trend_batch.go \
  backend/internal/relay/sub2api_team_trend_batch_test.go \
  backend/internal/teamusage/prewarm_source.go \
  backend/internal/teamusage/prewarm_source_test.go \
  backend/internal/teamusage/service.go
git commit -m "feat(teamusage): build provider prewarm source"
```

---

### Task 7: Add The Redis-Leased Background Prewarmer

**Files:**
- Create: `backend/internal/teamusage/prewarmer.go`
- Create: `backend/internal/teamusage/prewarmer_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go`

**Interfaces:**
- Consumes: `PrewarmRefreshClass` and `PrewarmObserver` from Task 4.
- Produces: `func NewPrewarmer(source *prewarmSource, cache *PrewarmCache, options PrewarmerOptions) (*Prewarmer, error)`.
- Produces: `func (p *Prewarmer) RunOnce(ctx context.Context, class PrewarmRefreshClass) error`.
- Produces: `func (p *Prewarmer) Start(ctx context.Context)`; it returns immediately and owns no completed payload.
- Produces config env `AE_TEAM_USAGE_REDIS_PREWARM_ENABLED`, default `false`.

- [ ] **Step 1: Write RED config tests**

```go
t.Setenv("AE_TEAM_USAGE_REDIS_PREWARM_ENABLED", "true")
cfg, err := Load("")
if err != nil || !cfg.TeamUsage.RedisPrewarmEnabled {
	t.Fatalf("prewarm enabled = %v, err = %v", cfg.TeamUsage.RedisPrewarmEnabled, err)
}
```

Assert the default is false so rollout is inert until staging explicitly enables it.

- [ ] **Step 2: Write RED lease and lifecycle tests**

Create two prewarmers sharing miniredis and block the source. Start `RunOnce(moving)` concurrently and assert exactly one source call, one `lease_acquired`, one `lease_busy`, and one manifest publish. Assert the non-holder never polls generation values. Assert wrong-token release cannot remove the holder lease.

- [ ] **Step 3: Write RED schedule/failure tests**

Inject `Now`, `Sleep`, moving interval, historical next-run calculation, and jitter source. Cover startup recovery with a healthy manifest making no source call; missing/hard-expired current/history facts; moving every 60 seconds; one historical run after a deterministic UTC-day jitter; Relay failure retaining the old manifest; cancellation; and provider-version change before publish.

- [ ] **Step 4: Run RED**

```bash
cd backend
go test ./internal/config ./internal/teamusage ./cmd/server -run 'RedisPrewarm|Prewarmer' -count=1
```

Expected: compile failure for config and prewarmer types.

- [ ] **Step 5: Implement the scheduler**

`Start` must launch one goroutine, run startup recovery asynchronously, then select on the 60-second moving timer, the calculated next historical timer, and `ctx.Done()`. One tick performs at most one attempt; later scheduled ticks provide retry. Log one generated operation ID plus class/outcome only, never cache keys or user facts.

- [ ] **Step 6: Wire startup and shutdown behind the flag**

Construct the cache/source/prewarmer after Ent migration, provider runtime, Redis, metrics, and `providerHandler` succeed. Start it with its own cancelable context before HTTP serving without blocking liveness. Cancel it during shutdown next to the directory scheduler. When disabled, do not acquire a lease or call Relay.

- [ ] **Step 7: Run GREEN and race tests**

```bash
cd backend
go test ./internal/config ./internal/teamusage ./cmd/server -run 'RedisPrewarm|Prewarmer' -count=20
go test -race ./internal/teamusage ./cmd/server -run 'Prewarmer' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/teamusage/prewarmer.go backend/internal/teamusage/prewarmer_test.go \
  backend/internal/config/config.go backend/internal/config/config_test.go \
  backend/cmd/server/main.go backend/cmd/server/main_test.go
git commit -m "feat(teamusage): schedule Redis prewarming"
```

---

### Task 8: Use Prewarmed Facts In All Team Usage Lanes

**Files:**
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/origin.go`
- Modify: `backend/internal/teamusage/shared_origin_test.go`
- Create: `backend/internal/teamusage/prewarm_origin_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/runtime_dependencies_test.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Adds `PrewarmCache *PrewarmCache` and `PrewarmEnabled bool` to `teamusage.ServiceOptions`.
- Adds `TeamUsagePrewarmCache *teamusage.PrewarmCache` and `TeamUsagePrewarmEnabled bool` to `handler.RouterOptions`.
- Consumes: the closed `PrewarmFallbackReason` constants defined in Task 4.
- Produces: `func (s *Service) loadPrewarmedScopeOrigin(ctx context.Context, request *splitReadRequest, provider relay.Provider) (*teamUsageScopeOrigin, PrewarmFallbackReason, bool)`.
- Preserves: `loadTeamUsageScopeOrigin` as the complete exact fallback and all public service/handler signatures.

- [ ] **Step 1: Write RED four-lane prewarm tests**

Prepopulate a valid generation, clear response and scope-origin keys, call Summary, Trend, Members, and Organization concurrently, and assert:

```go
if provider.statsCalls != 0 || provider.trendCalls != 0 {
	t.Fatalf("prewarmed lanes called Relay: stats=%d trend=%d", provider.statsCalls, provider.trendCalls)
}
```

Assert the four DTOs and freshness metadata equal the retained authoritative path for the same synthetic scope.

- [ ] **Step 2: Write RED authorization and provider-version tests**

Cache facts for Relay IDs 101, 102, and 999 while the current scope authorizes only 101 and 102. Assert 999 never contributes to summary, member rank, trend, or organization. Change the provider configuration version and assert the old generation is ignored. Change scope version during the request and assert the existing scope race guard prevents stale authorization exposure.

- [ ] **Step 3: Write RED fallback-matrix tests**

Table-test closed reasons: `disabled`, `custom_range`, `unsupported_granularity`, `non_hour_offset`, `manifest_miss`, `current_stats_miss`, `history_miss`, `invalid_generation`, `redis_error`, and `moving_edge_miss`. Assert all but `moving_edge_miss` invoke the complete existing scope-origin path; non-hour zones preserve exact timezone parameters. A moving-edge miss with valid history must fetch only the missing current interval and must not refetch older complete days.

- [ ] **Step 4: Run RED**

```bash
cd backend
go test ./internal/teamusage ./internal/handler -run 'Prewarmed|PrewarmFallback|PrewarmAuthorization' -count=1
```

Expected: compile failure for the new service dependency and read path.

- [ ] **Step 5: Implement prewarm-first origin loading**

After `resolveOverviewSubjects` returns current subjects and Relay IDs, attempt `PrewarmCache.Read` only for supported standard windows. Pass only the authorized sorted Relay IDs to `reconstructPrewarmOrigin`, attach the current subjects after reconstruction, call `completeScopeOriginRanges`, and return it through existing snapshot projection helpers. Do not persist a completed origin in Pod memory.

- [ ] **Step 6: Preserve the exact and partial fallbacks**

For complete fallback, call the unchanged `loadTeamUsageScopeOrigin`. For moving-edge-only failure, reuse validated historical shards and call `GetUsageTrendForUsers` only for the uncovered interval; merge only after provider/version, ordering, and authorization validation. Never treat a missing historical shard as zero.

- [ ] **Step 7: Wire required dependencies**

Production router validation must require a prewarm cache object even when the feature flag is false, just as it requires snapshot/origin caches; the flag decides whether it is read. Update `newTeamUsageService` and all tests with the explicit dependency. Pass the same Redis-backed `PrewarmCache` instance to the prewarmer and request service.

- [ ] **Step 8: Run GREEN, DTO contract, and race tests**

```bash
cd backend
go test ./internal/teamusage ./internal/handler -run 'Prewarmed|PrewarmFallback|PrewarmAuthorization|TeamUsage.*HTTP' -count=20
go test -race ./internal/teamusage ./internal/handler -run 'Prewarmed|SharedOriginFourConcurrentLanes' -count=1
```

Expected: PASS; no Relay call on a fully prewarmed supported window.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/teamusage/service.go backend/internal/teamusage/origin.go \
  backend/internal/teamusage/shared_origin_test.go backend/internal/teamusage/prewarm_origin_test.go \
  backend/internal/handler/router.go backend/internal/handler/team_usage.go \
  backend/internal/handler/runtime_dependencies_test.go backend/internal/handler/team_usage_test.go \
  backend/cmd/server/main.go
git commit -m "perf(teamusage): serve prewarmed Redis facts"
```

---

### Task 9: Add Bounded Observability And Current Architecture Docs

**Files:**
- Create: `backend/internal/telemetry/team_usage_prewarm.go`
- Create: `backend/internal/telemetry/team_usage_prewarm_test.go`
- Modify: `backend/internal/telemetry/metrics.go`
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `deploy/observability/grafana/ai-efficiency-performance.json`
- Modify: `deploy/observability/README.md`
- Modify: `docs/architecture.md`

**Interfaces:**
- Implements the existing `teamusage.PrewarmObserver` methods `ObserveCycle`, `ObserveSource`, `ObserveGeneration`, `RecordCache`, `SetLastSuccess`, and `RecordFallback` with a concrete telemetry recorder.
- Produces Prometheus metrics for cycle count/duration, source bytes/points, generation/day-shard bytes, last successful moving/historical Unix timestamp, cache component outcomes, and closed-enum fallback reasons.
- Uses only labels from fixed class/outcome/component/reason enumerations; never provider IDs, users, cache keys, operation IDs, or request IDs as labels.

- [ ] **Step 1: Write RED telemetry registration and label tests**

Record one successful moving cycle, one source observation, two shard sizes, one history fresh hit, one `non_hour_offset` fallback, and last success. Gather metrics and assert exact names, values, and label sets. Assert unknown class/outcome/component/reason values normalize to `unknown` or are ignored; they must not create new cardinality.

- [ ] **Step 2: Run RED**

```bash
cd backend
go test ./internal/telemetry ./cmd/server -run 'TeamUsagePrewarm|ProductionCacheMetrics' -count=1
```

Expected: compile failure for the observer and recorders.

- [ ] **Step 3: Implement and wire metrics**

Use dedicated Prometheus vectors rather than overloading generic cache outcomes for cycle/source/generation/fallback metrics. Reuse `CacheRecorder` only for stable `team_usage_prewarm_manifest`, `team_usage_prewarm_current`, and `team_usage_prewarm_history` names. Register everything once in `NewMetrics` and inject one observer into both prewarmer and reader.

- [ ] **Step 4: Update dashboard and operator documentation**

Add panels/queries for p50/p95 cycle duration, source and generation sizes, last-success age, fallback rate by closed reason, manifest/current/history outcomes, and Redis pool wait/dial errors. Document the feature flag, key prefix discovery without dumping values, stale-manifest diagnosis, lease diagnosis, safe rollback by disabling the flag, and the three staging audit modes.

- [ ] **Step 5: Update current architecture only now that code exists**

Document that the modular monolith owns provider-wide Redis prewarming, Sub2API remains an HTTP dependency, authorization stays request-time/local, Redis stores UTC-hour facts rather than completed actor responses, non-hour timezones use exact fallback, and no Pod completed-result cache exists. Link the 2026-07-21 spec without rewriting historical specs.

- [ ] **Step 6: Validate JSON, metrics, and docs**

```bash
cd backend
go test ./internal/telemetry ./cmd/server -run 'TeamUsagePrewarm|ProductionCacheMetrics' -count=1
cd ..
jq -e . deploy/observability/grafana/ai-efficiency-performance.json >/dev/null
rg -n 'team_usage_prewarm|RedisPrewarm' docs/architecture.md deploy/observability/README.md
```

Expected: tests pass, Grafana JSON is valid, and both current docs describe the implemented boundary.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/telemetry/team_usage_prewarm.go \
  backend/internal/telemetry/team_usage_prewarm_test.go backend/internal/telemetry/metrics.go \
  backend/cmd/server/cache_metrics.go backend/cmd/server/cache_metrics_test.go \
  backend/cmd/server/main.go deploy/observability/grafana/ai-efficiency-performance.json \
  deploy/observability/README.md docs/architecture.md
git commit -m "feat(teamusage): observe Redis prewarming"
```

---

### Task 10: Verify, Review, Publish, And Audit Staging

**Files:**
- Modify throughout execution: `docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md`
- Modify after reviewed merge: `/Users/admin/helm/ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`

**Interfaces:**
- Produces: one reviewed exact-head PR targeting `feat/platform-loading-performance`.
- Produces after merge: one immutable `staging-<full-merge-sha>` multi-architecture image and an exact two-phase staging rollout.
- Produces: sanitized empty-Redis, prewarmed-cold, and immediate-warm evidence with request/dependency/cache/lease metrics.

- [ ] **Step 1: Run focused, full, race, vet, build, and hygiene verification**

```bash
cd backend
go test ./internal/readcache ./internal/relay ./internal/teamusage ./internal/handler ./internal/telemetry ./cmd/server -count=1
go test ./... -count=1
go test -race ./internal/readcache ./internal/relay ./internal/teamusage ./internal/handler ./cmd/server -count=1
go vet ./...
go build ./...
cd ..
git diff --check
git status --short
rg -n '(agora\.io|password\s*[:=]|api[_-]?key\s*[:=]|Bearer [A-Za-z0-9_-])' \
  docs/superpowers/specs/2026-07-21-team-usage-redis-daily-prewarm-design.md \
  docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md \
  backend/internal/teamusage backend/internal/relay
```

Expected: all commands pass; safety scan finds no real identity or credential. Record exact test counts and any environment-only warning in this plan immediately after running it.

- [ ] **Step 2: Request independent standards/spec review and fix findings**

Review against `AGENTS.md`, the 2026-07-21 design, and PR #192 retained baseline. Require explicit checks for authorization intersection, manifest atomicity, old/new generation mixing, timezone/DST correctness, 5,000-user truncation, bounded payloads, lease token safety, no completed Pod cache, DTO compatibility, and cancellation. Fix every Critical/Important issue and rerun affected tests before proceeding.

- [ ] **Step 3: Push and open the PR without merging**

```bash
git push -u origin perf/team-usage-daily-prewarm
gh pr create \
  --base feat/platform-loading-performance \
  --head perf/team-usage-daily-prewarm \
  --title 'perf(teamusage): prewarm Redis usage facts' \
  --body-file /tmp/team-usage-prewarm-pr.md
```

The PR body must include sanitized POC results, Redis memory/TTL bounds, no-Sub2API/no-Pod-cache boundaries, exact verification, feature-flag rollback, and the staging acceptance plan. Delete `/tmp/team-usage-prewarm-pr.md` after creation. Wait for backend, frontend, ae-cli, and deploy-static CI on the exact reviewed head. Stop at the merge gate until the user merges or explicitly authorizes merge.

- [ ] **Step 4: Publish and deploy only after the reviewed PR is merged**

Build and push `ghcr.io/lichking-2234/ai-efficiency:staging-<full-merge-sha>` for `linux/amd64,linux/arm64`, inspect the OCI index and both manifests, then update only staging selectors in `/Users/admin/helm`. Execute the established two-phase restore: Phase A at zero replicas with restore disabled, verify no app Pod, then Phase B with `--atomic --wait --wait-for-jobs --timeout 20m`. Enable `AE_TEAM_USAGE_REDIS_PREWARM_ENABLED=true` only for staging. Verify live/ready commit identity plus database, Redis, and Relay health. Do not modify production values.

- [ ] **Step 5: Audit empty Redis**

Delete only the new prewarm manifest/generation/lease keys plus existing Team Usage origin and four response keys. Issue the deterministic four-lane request set concurrently. Require HTTP 200, business equivalence, one authoritative refill, one prewarm cycle across Pods, no individual trend endpoint, zero Relay 429/5xx/transport/timeout, zero Redis dial/cache/lease error, and completion no slower than the recorded 14.4-second baseline by more than one second.

- [ ] **Step 6: Audit prewarmed cold UI**

Wait for a successful current and historical manifest, retain all prewarm keys, clear only Summary/Trend/Members/Organization response keys, then issue all four lanes concurrently. Require every lane `<= 5.0s`, zero batch-stats POSTs, zero aggregate or individual trend GETs from the user request, HTTP 200, equivalent business payloads, and zero Redis/cache/lease errors.

- [ ] **Step 7: Audit immediate warm**

Repeat all four lanes without clearing keys. Require every lane `<= 1.5s`, zero Relay calls, and fresh response-cache outcomes. Record request IDs, operation IDs, dependency durations/counts, prewarm/cache outcomes, Redis pool metrics, image digest, and Helm revision without retaining raw bodies or identities.

- [ ] **Step 8: Decide staging acceptance and clean sensitive state**

If every gate passes, leave staging on the exact accepted image and mark production release as separately pending. If any gate fails, disable only `AE_TEAM_USAGE_REDIS_PREWARM_ENABLED`, verify PR #192 fallback behavior and health, and retain sanitized failure evidence. In both cases delete tokens, request bodies, user lists, Redis dumps, and temporary scripts; stop owned port-forwards; preserve unrelated worktrees and root changes.

- [ ] **Step 9: Commit the final execution ledger**

```bash
git add docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md
git commit -m "docs(teamusage): record prewarm verification"
git push
```

If this ledger changes the PR head before merge, require the exact-head CI checks again. If it records post-merge staging evidence, use a narrow follow-up PR rather than rewriting the merged commit.
