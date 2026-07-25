# Stateless Team Usage Prewarm Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace PR #193's per-Backend-Pod prewarm scheduler with one optional stateless worker process while preserving exact Team Usage results, authorization-first Redis reads, and PR #192 fallback behavior.

**Architecture:** The existing provider-wide Relay sources, timezone model, zstd codec, immutable values, and publish-last manifests remain in the modular monolith. Backend Pods construct one read-only schema-v3 Redis reader and never schedule or write prewarm data; one separately deployed `ai-efficiency-prewarmer` process runs a serial 60-second refresh loop, coordinates through one Redis lease, and publishes each timezone independently.

**Tech Stack:** Go 1.24, Ent, go-redis v9, miniredis, Prometheus client_golang, zap, Docker Buildx, Helm, Kubernetes, Google Chrome.

**Status:** Approved plan; implementation has not started. Update this status and each checkbox in the same work session as the corresponding action. Tasks 1-5 use `/Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm`; Task 6 creates an independent Helm worktree and must not modify `/Users/admin/helm`'s dirty main worktree; Task 7 records sanitized staging evidence only.

## Global Constraints

- Backend Pods own no prewarm scheduler, source limiter, generation writer, recovery flow, or completed-result cache.
- The image contains `/app/ai-efficiency-server` and `/app/ai-efficiency-prewarmer`; they share one repository, image tag, release, and version.
- The worker is one replica with Kubernetes `Recreate`, no Service, Ingress, PVC, or user traffic.
- The worker calls `Refresh(context.Context) error` immediately and then serially every 60 seconds; one call has a five-minute deadline.
- One deployment-wide Redis refresh lease lasts six minutes; source calls use one process-local semaphore with capacity two.
- Redis schema version is `3`; schema-v2 keys are neither read nor migrated.
- Each timezone manifest publishes independently after all referenced immutable values, with the refresh token checked in the same Redis command.
- Retain exact timezone segmentation, strict source bounds, zstd values, provider/version isolation, authorization-first reads, and PR #192 exact fallback.
- Retain current freshness and TTL values: moving fresh `75s`, moving hard-valid `4m`, moving Redis TTL `6m`, history fresh `25h`, history hard-valid `49h`, history Redis TTL `50h`, manifest TTL `3m`.
- Remove startup, moving, recovery, historical, cohort, replay, segment-lease, source-slot-lease, reader-slot, and request-time repair state machines.
- Expose only the five approved metric families and their closed vocabularies from the spec.
- `prewarmer.enabled` defaults to `false`; Compose and systemd do not launch the worker.
- Do not change frontend routes, request or response DTOs, cursors, Sub2API source code, or production enablement.
- Tests, logs, plans, and evidence contain no real identities, credentials, groups, Relay IDs, Redis values, or raw downstream bodies.

## File Structure

**Retain and simplify**

- `backend/internal/relay/provider.go`, `sub2api.go`, `sub2api_team_trend_batch.go`: bounded provider-wide source interfaces and Sub2API HTTP adapters.
- `backend/internal/teamusage/prewarm_model.go`: timezone/window recognition, source validation, exact composition, and schema-bounded domain values.
- `backend/internal/teamusage/prewarm_codec.go`: bounded zstd value encoding and decoding.
- `backend/internal/teamusage/prewarm_cache.go`: schema-v3 keys, immutable values, selected-reference MGET, one refresh lease, and publish-last manifests.
- `backend/internal/teamusage/prewarm_source.go`: directory/current-stats and trend-segment construction through a supplied local limiter.
- `backend/internal/teamusage/prewarm_reader.go`: read-only authorized projection with exact fallback.
- `backend/internal/teamusage/origin.go`, `service.go`: authorization-first integration and post-hit scope/provider revalidation.

**Create**

- `backend/internal/teamusage/prewarm_refresher.go`: one serial, restart-safe `Refresher` implementation.
- `backend/internal/teamusage/prewarm_refresher_test.go`: deterministic refresh, contention, restart, and publication tests.
- `backend/internal/redisruntime/client.go`: the existing bounded go-redis client construction shared by server and worker commands.
- `backend/internal/redisruntime/client_test.go`: exact transport option contract.
- `backend/cmd/prewarmer/main.go`: minimal DB, Redis, Relay, metrics, signal, and ticker bootstrap without migration or HTTP application runtime.
- `backend/cmd/prewarmer/main_test.go`: bootstrap boundary and serial loop tests.
- `/Users/admin/helm/.worktrees/ai-efficiency-prewarmer/ai-efficiency/templates/prewarmer-deployment.yaml`: optional one-replica worker Deployment.

**Delete in the final diff**

- `backend/internal/teamusage/prewarmer.go`
- `backend/internal/teamusage/prewarmer_startup.go`
- `backend/internal/teamusage/prewarmer_startup_test.go`
- `backend/internal/teamusage/prewarmer_test.go`
- `backend/internal/teamusage/prewarm_reader_slot.go`
- `docs/superpowers/plans/2026-07-21-team-usage-redis-daily-prewarm.md`
- `docs/superpowers/plans/2026-07-21-team-usage-segmented-timezone-prewarm.md`
- `docs/superpowers/plans/2026-07-23-team-usage-replay-guardian.md`
- `docs/superpowers/plans/2026-07-23-team-usage-startup-cohort-publication.md`
- `docs/superpowers/specs/2026-07-21-team-usage-redis-daily-prewarm-design.md`
- `docs/superpowers/specs/2026-07-21-team-usage-segmented-timezone-prewarm-design.md`
- `docs/superpowers/specs/2026-07-23-team-usage-replay-guardian-design.md`
- `docs/superpowers/specs/2026-07-23-team-usage-startup-cohort-publication-design.md`
- `docs/superpowers/specs/2026-07-24-team-usage-prewarm-closeout-design.md`

---

### Task 1: Implement One Restart-Safe Refresh Cycle

**Files:**
- Create: `backend/internal/teamusage/prewarm_refresher.go`
- Create: `backend/internal/teamusage/prewarm_refresher_test.go`
- Modify: `backend/internal/teamusage/prewarm_metrics.go`
- Modify: `backend/internal/teamusage/prewarm_source.go`
- Modify: `backend/internal/teamusage/prewarm_source_test.go`

**Interfaces:**
- Consumes: `PrimaryProviderBindingResolver.ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error)`, `PrewarmCache.Read`, `WriteCurrentStats`, `WriteSegment`, `PublishManifest`, `TryAcquireLease`, and `ReleaseLease`.
- Produces:

```go
type Refresher interface {
	Refresh(context.Context) error
}

type PrewarmRefreshOutcome string

const (
	PrewarmRefreshSuccess PrewarmRefreshOutcome = "success"
	PrewarmRefreshPartial PrewarmRefreshOutcome = "partial"
	PrewarmRefreshSkipped PrewarmRefreshOutcome = "skipped"
	PrewarmRefreshError   PrewarmRefreshOutcome = "error"
)

type PrewarmSourceClass string

const (
	PrewarmSourceDirectory    PrewarmSourceClass = "directory"
	PrewarmSourceCurrentStats PrewarmSourceClass = "current_stats"
	PrewarmSourceTodayHour    PrewarmSourceClass = "today_hour"
	PrewarmSourceHistory6d    PrewarmSourceClass = "history_6d"
	PrewarmSourceHistory29d   PrewarmSourceClass = "history_29d"
)

type RefresherOptions struct {
	Timezones       []string
	Now             func() time.Time
	NewToken        func() string
	NewGenerationID func() string
	CycleTimeout    time.Duration
	SourceTimeout   time.Duration
	Metrics         PrewarmMetrics
	Reporter        RefreshReporter
}

type RefreshReport struct {
	Outcome        PrewarmRefreshOutcome
	Duration       time.Duration
	PlannedLanes   int
	PublishedLanes int
	SourceCounts   map[PrewarmSourceClass]int
}

type RefreshReporter interface {
	ReportRefresh(RefreshReport)
}

func NewRefresher(
	resolver PrimaryProviderBindingResolver,
	cache *PrewarmCache,
	options RefresherOptions,
) (Refresher, error)
```

- Invariants: default `CycleTimeout=5m`, refresh lease TTL `6m`, local source concurrency `2`, no ticker or process lifecycle in this package.

- [ ] **Step 1: Write failing refresh-cycle tests**

Add table-driven tests with these exact cases and assertions:

```go
func TestRefresherRefreshPlansFromRedis(t *testing.T) {
	tests := []struct {
		name              string
		seedManifest      bool
		advanceLocalDay   bool
		wantCurrentCalls  int
		wantTodayCalls    int
		wantHistory29Call int
		wantHistory6Call  int
	}{
		{name: "first generation", wantCurrentCalls: 1, wantTodayCalls: 1, wantHistory29Call: 1, wantHistory6Call: 1},
		{name: "same anchor reuses hard-valid history", seedManifest: true, wantCurrentCalls: 1, wantTodayCalls: 1},
		{name: "new local anchor fetches both histories", seedManifest: true, advanceLocalDay: true, wantCurrentCalls: 1, wantTodayCalls: 1, wantHistory29Call: 1, wantHistory6Call: 1},
	}
	// Each case constructs a new Refresher, proving restart behavior derives
	// planning from Redis rather than process-local state.
}
```

Also add:

- `TestRefresherCurrentStatsFailurePublishesNoManifest`
- `TestRefresherTimezoneFailureKeepsOldLaneAndPublishesOtherLane`
- `TestRefresherLeaseContentionAllowsExactlyOneSourceOwner`
- `TestRefresherLeaseExpiryTokenReplacementCancellationAndProviderChangeBlockPublication`
- `TestRefresherUsesAtMostTwoConcurrentSourceCalls`
- `TestRefresherRefreshDoesNotOverlapOrPersistStateBetweenCalls`

Use miniredis and synthetic user IDs. The two-refresher test starts both calls behind one barrier and asserts total directory calls equal one, not one per instance.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter' -count=1
```

Expected: FAIL because `Refresher`, `RefresherOptions`, and `NewRefresher` do not exist.

- [ ] **Step 3: Implement the minimal refresher**

Implement an unexported `refresher` with no `Start`, `Stop`, ticker, atomics, startup state, or recovery state. Its cycle is exactly:

```go
func (r *refresher) Refresh(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, r.cycleTimeout)
	defer cancel()

	binding, err := r.resolver.ResolvePrimaryProviderBinding(ctx)
	if err != nil {
		return fmt.Errorf("resolve primary provider binding: %w", err)
	}
	token := r.newToken()
	leaseKey := r.cache.RefreshLeaseKey()
	owned, err := r.cache.TryAcquireLease(ctx, leaseKey, token, 6*time.Minute)
	if err != nil {
		return fmt.Errorf("acquire refresh lease: %w", err)
	}
	if !owned {
		return nil
	}
	defer r.releaseLease(leaseKey, token)

	cycleTime := r.now().UTC()
	return r.refreshOwned(ctx, binding, cycleTime, leaseKey, token)
}
```

`refreshOwned` must:

1. derive and split-safety-check every configured timezone anchor from `cycleTime`;
2. read each current manifest and retain only same-provider/version/anchor history references that are still hard-valid for the candidate manifest lifetime;
3. fetch provider-wide directory/current stats once and write one immutable current value;
4. schedule `today_hour` for every valid lane and only missing/invalid `history_6d` and `history_29d` work;
5. run source work through a `chan struct{}` of capacity two with one bounded context per Relay call;
6. write immutable segment values before manifest construction;
7. re-resolve provider ID/version immediately before publication;
8. publish each complete lane independently through `PublishManifest(ctx, leaseKey, token, manifest)`;
9. return an error for complete failure or partial lane failure after allowing successful lanes to publish.

Keep `PrewarmSource` as the only Relay-to-domain validation seam. The local limiter is:

```go
type localSourceCallLimiter struct {
	semaphore chan struct{}
	timeout   time.Duration
}

func (l *localSourceCallLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	select {
	case l.semaphore <- struct{}{}:
		defer func() { <-l.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	callCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	return call(callCtx)
}
```

- [ ] **Step 4: Verify focused behavior and races**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/teamusage/prewarm_refresher.go internal/teamusage/prewarm_refresher_test.go internal/teamusage/prewarm_source.go internal/teamusage/prewarm_source_test.go
go test ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter|TestPrewarmSource' -count=1
go test -race ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter' -count=1
```

Expected: PASS; race detector reports no races.

- [ ] **Step 5: Update this plan and commit**

Mark Task 1 complete, record only sanitized test results, then run:

```bash
git add backend/internal/teamusage/prewarm_refresher.go \
  backend/internal/teamusage/prewarm_refresher_test.go \
  backend/internal/teamusage/prewarm_metrics.go \
  backend/internal/teamusage/prewarm_source.go \
  backend/internal/teamusage/prewarm_source_test.go \
  docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md
git commit -m "perf(teamusage): add serial Redis prewarm refresher"
```

---

### Task 2: Make Backend Reads Stateless and Redis Schema v3 Single-Lease

**Files:**
- Modify: `backend/internal/readcache/store.go`
- Modify: `backend/internal/readcache/store_test.go`
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_cache_test.go`
- Modify: `backend/internal/teamusage/prewarm_reader.go`
- Modify: `backend/internal/teamusage/prewarm_reader_test.go`
- Delete: `backend/internal/teamusage/prewarm_reader_slot.go`
- Delete: `backend/internal/teamusage/prewarmer.go`
- Delete: `backend/internal/teamusage/prewarmer_startup.go`
- Delete: `backend/internal/teamusage/prewarmer_startup_test.go`
- Delete: `backend/internal/teamusage/prewarmer_test.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/origin.go`
- Modify: `backend/internal/teamusage/service_dependencies_test.go`
- Modify: `backend/internal/teamusage/shared_origin_test.go`
- Modify: `backend/internal/teamusage/range_completion_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/runtime_dependencies_test.go`

**Interfaces:**
- Consumes: Task 1 `Refresher`, existing `readcache.BatchStore.MGet`, and existing PR #192 scope-origin fallback.
- Produces:

```go
func NewPrewarmReader(
	cache *PrewarmCache,
	options PrewarmReaderOptions,
) (*PrewarmReader, error)

type PrewarmReaderOptions struct {
	Now     func() time.Time
	Metrics PrewarmMetrics
}

type PrewarmReadRequest struct {
	ProviderID             int
	ProviderVersion        int64
	Params                 OverviewParams
	AuthorizedRelayUserIDs []int64
}

const (
	PrewarmReadFullHit    PrewarmReadOutcome = "full_hit"
	PrewarmReadMiss       PrewarmReadOutcome = "miss"
	PrewarmReadIneligible PrewarmReadOutcome = "ineligible"
	PrewarmReadInvalid    PrewarmReadOutcome = "invalid"
	PrewarmReadFallback   PrewarmReadOutcome = "fallback"
)

type ServiceOptions struct {
	SnapshotCache *SnapshotCache
	OriginCache   *OriginCache
	PrewarmReader *PrewarmReader
	CursorSecret  string
}
```

- `PrewarmCache.RefreshLeaseKey() string` is the only prewarm coordination-key constructor.
- `PrewarmCache.PublishManifest(ctx, leaseKey, token string, manifest PrewarmManifest) (bool, error)` is the only manifest publication API.
- `readcache.BatchStore` retains `MGet` and `SetIfLeaseOwned`; it no longer exposes `SetIfLeasesOwned`. `readcache.Store.LeaseTTL` remains unchanged because non-prewarm caches use it.

- [ ] **Step 1: Write failing schema-v3 and read-only Backend tests**

Add tests that assert:

```go
func TestPrewarmCacheUsesSchemaV3AndOneRefreshLease(t *testing.T) {
	if prewarmCacheSchemaVersion != 3 {
		t.Fatalf("schema = %d, want 3", prewarmCacheSchemaVersion)
	}
	// Seed a schema-v2 manifest and assert ReadWindow reports a miss.
	// Replace the refresh token before PublishManifest and assert published=false.
}

func TestPrewarmReaderNeverCallsRelayOrWritesRedis(t *testing.T) {
	// The request contains no relay.Provider. A full hit succeeds through GET and
	// MGET only; miss, corruption, expiry, and roster absence return no origin.
}

func TestServiceUsesDirectPrewarmReaderWithoutMutableSlot(t *testing.T) {
	service := newServiceForTest(t)
	service.prewarmReader = mustReadOnlyPrewarmReader(t)
	// Assert authorized resolution precedes Redis and post-hit scope/version
	// revalidation still rejects a changed authorization snapshot.
}
```

Add an operation-recording `readcache.BatchStore` test double and assert the reader operation set is exactly `GET` plus `MGET`. Add compile-time assertions that no test double implements `SetIfLeasesOwned`.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/readcache ./internal/teamusage ./cmd/server \
  -run 'TestPrewarmCacheUsesSchemaV3|TestPrewarmReaderNeverCallsRelay|TestServiceUsesDirectPrewarmReader|TestRedisStoreSetIfLeaseOwned' -count=1
```

Expected: FAIL because schema is v2, the reader requires a limiter/provider, and Backend wiring still owns a mutable runtime.

- [ ] **Step 3: Simplify the Redis contract**

Set `prewarmCacheSchemaVersion = 3`. Keep current TTLs, zstd bounds, manifest validation, immutable write order, and window-selected MGET behavior. Remove `PrewarmLeaseClaim`, multi-lease publication, recovered publication, lease-TTL reads, segment/source-slot key kinds, and every startup/recovery helper.

Replace the multi-lease Lua script in `readcache` with a one-lease script:

```lua
if redis.call("GET", KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call("SET", KEYS[2], ARGV[2], "PX", ARGV[3])
return 1
```

`SetIfLeaseOwned` validates one lease key/token and runs that script atomically. Do not remove `Store.LeaseTTL` or its tests.

- [ ] **Step 4: Make the reader permanently read-only**

Remove `source`, `newToken`, `flights`, `SourceCallLimiter`, `recoverToday`, `PrewarmReadPartialToday`, and the timezone allowlist from `PrewarmReader`. The read path is:

```go
window, recognized, err := RecognizePrewarmWindow(request.Params, r.now())
if err != nil {
	return nil, PrewarmReadInvalid, err
}
if !recognized {
	return nil, PrewarmReadIneligible, nil
}
result, found, err := r.cache.ReadWindow(ctx, PrewarmCacheIdentity{
	ProviderID: request.ProviderID, ProviderVersion: request.ProviderVersion,
	Timezone: window.Coverage.Timezone, AnchorDate: window.AnchorDate,
}, window.Class)
if err != nil {
	return nil, PrewarmReadFallback, err
}
if !found || result == nil {
	return nil, PrewarmReadMiss, nil
}
if !result.Complete {
	return nil, PrewarmReadInvalid, nil
}
origin, eligible, _, err := composePrewarmedOriginWithUnion(
	window, *result.CurrentStats, result.Segments, request.AuthorizedRelayUserIDs,
)
if err != nil || !eligible {
	return nil, PrewarmReadFallback, err
}
return origin, PrewarmReadFullHit, nil
```

Corrupt, expired, missing, unsupported-window, roster-incomplete, provider-version-mismatched, and Redis-error cases immediately return to the existing exact loader. A complete sparse provider trend still contributes zero for an omitted authorized user only when the current-stats roster proves that user exists.

- [ ] **Step 5: Remove Backend lifecycle ownership and wire the direct reader**

Delete `teamUsagePrewarmRuntime`, `prepareTeamUsagePrewarm`, the background reporter, startup/shutdown calls, and prewarm config branches from `cmd/server/main.go`. Construct `PrewarmCache` and `PrewarmReader` synchronously from the already-required Redis store and pass the reader through `handler.RouterOptions` to `teamusage.ServiceOptions`.

Store the reader directly:

```go
type Service struct {
	client        *ent.Client
	scopeResolver ScopeResolver
	prewarmReader *PrewarmReader
}
```

This snippet shows the changed field, not a replacement for the complete
service struct. Keep every unrelated existing field. `loadPrewarmFirstScopeOrigin`
reads `s.prewarmReader` directly; delete the mutable `currentPrewarmReader`
indirection.

Backend construction does not check an enable flag and does not know the worker timezone list. A missing schema-v3 manifest is a normal miss. Delete the old lifecycle and slot files in the same step.

- [ ] **Step 6: Verify stateless Backend behavior**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/readcache/store.go internal/readcache/store_test.go \
  internal/teamusage/prewarm_cache.go internal/teamusage/prewarm_cache_test.go \
  internal/teamusage/prewarm_reader.go internal/teamusage/prewarm_reader_test.go \
  internal/teamusage/service.go internal/teamusage/origin.go \
  cmd/server/main.go cmd/server/main_test.go
go test ./internal/readcache ./internal/teamusage ./internal/handler ./cmd/server -count=1
go test -race ./internal/readcache ./internal/teamusage ./cmd/server -count=1
rg -n 'teamUsagePrewarmRuntime|prepareTeamUsagePrewarm|PrewarmReaderSlot|recoverToday|SetIfLeasesOwned|PublishManifestWithLeases|LeaseTTL\(' \
  cmd/server internal/teamusage internal/readcache
```

Expected: tests PASS; race detector is clean; `rg` finds `LeaseTTL` only in shared readcache/non-prewarm cache code and finds none of the other removed symbols.

- [ ] **Step 7: Update this plan and commit**

```bash
git add -A backend/cmd/server backend/internal/readcache backend/internal/teamusage backend/internal/handler \
  docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md
git commit -m "refactor(teamusage): make prewarm backend read only"
```

---

### Task 3: Collapse Observability to Five Metric Families

**Files:**
- Modify: `backend/internal/teamusage/prewarm_metrics.go`
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_source.go`
- Modify: `backend/internal/teamusage/prewarm_refresher.go`
- Modify: `backend/internal/teamusage/prewarm_reader.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm.go`
- Modify: `backend/internal/telemetry/team_usage_prewarm_test.go`
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/internal/telemetry/dashboard_contract_test.go`
- Modify: `deploy/observability/grafana/ai-efficiency-performance.json`
- Modify: `deploy/observability/README.md`

**Interfaces:**
- Completes the typed vocabulary started in Task 1 and produces one recorder interface:

```go
type PrewarmMetrics interface {
	RecordRefresh(PrewarmRefreshOutcome, time.Duration)
	SetLaneLastSuccess(string, time.Time)
	RecordSource(PrewarmSourceClass, PrewarmSourceOutcome, time.Duration)
	RecordRequest(PrewarmReadOutcome)
}

type PrewarmSourceOutcome string
```

- Refresh outcomes: `success`, `partial`, `skipped`, `error`.
- Sources: `directory`, `current_stats`, `today_hour`, `history_6d`, `history_29d`.
- Source outcomes: `success`, `error`, `canceled`, `rejected`.
- Request outcomes: `full_hit`, `miss`, `ineligible`, `invalid`, `fallback`.
- `RefreshReporter` and `RefreshReport` from Task 1 remain the single bounded
  structured-log boundary; replace their temporary outcome/source values with
  the typed values above.

- [ ] **Step 1: Write the exact metric contract test**

Replace the broad metric test with one that gathers names and labels:

```go
wantFamilies := map[string][]string{
	"ai_efficiency_team_usage_prewarm_refresh_total":                     {"outcome"},
	"ai_efficiency_team_usage_prewarm_refresh_duration_seconds":          {},
	"ai_efficiency_team_usage_prewarm_lane_last_success_timestamp_seconds": {"timezone"},
	"ai_efficiency_team_usage_prewarm_source_duration_seconds":           {"source", "outcome"},
	"ai_efficiency_team_usage_prewarm_request_total":                     {"outcome"},
}
```

Assert the prior cycle, scheduler, Redis, quantity, generation, validation, cache, fallback-reason, byte, point, and user families are absent. Assert invalid typed values do not create new series and at most four configured timezone labels exist.

- [ ] **Step 2: Run the test to verify RED**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/telemetry ./cmd/server -run 'TestTeamUsagePrewarm|TestProductionCacheMetrics' -count=1
```

Expected: FAIL because the current implementation registers more than five prewarm families and uses duplicated string allowlists.

- [ ] **Step 3: Implement the closed typed vocabulary and recorder**

Define each enum, its `Valid() bool`, and its ordered `All...()` slice once in `teamusage/prewarm_metrics.go`. Telemetry must iterate those exported typed values for preinitialization and must not repeat string allowlists.

Remove prewarm-specific Redis command metrics from `PrewarmCache`; the existing Redis pool metrics remain authoritative. `PrewarmSource` records only its source class/outcome/duration. `Refresher` records one refresh result and lane success timestamps. `PrewarmReader` records one request result.

Update the dashboard to no more than five focused panels, each backed by one approved family. Remove startup, cohort, lease-TTL, scheduler-tick, and generation-size operations guidance.

- [ ] **Step 4: Verify metrics and bounded cardinality**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/teamusage/prewarm_metrics.go internal/teamusage/prewarm_cache.go \
  internal/teamusage/prewarm_source.go internal/teamusage/prewarm_refresher.go \
  internal/teamusage/prewarm_reader.go internal/telemetry/team_usage_prewarm.go \
  internal/telemetry/team_usage_prewarm_test.go cmd/server/cache_metrics.go cmd/server/cache_metrics_test.go
go test ./internal/teamusage ./internal/telemetry ./cmd/server -run 'Prewarm|ProductionCacheMetrics|Dashboard' -count=1
```

Expected: PASS and exactly five `ai_efficiency_team_usage_prewarm_*` families.

- [ ] **Step 5: Update this plan and commit**

```bash
git add backend/internal/teamusage backend/internal/telemetry backend/cmd/server/cache_metrics* \
  deploy/observability docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md
git commit -m "refactor(backend): simplify prewarm telemetry"
```

---

### Task 4: Add the Worker Command and Dual-Binary Image

**Files:**
- Create: `backend/internal/redisruntime/client.go`
- Create: `backend/internal/redisruntime/client_test.go`
- Create: `backend/cmd/prewarmer/main.go`
- Create: `backend/cmd/prewarmer/main_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/config/writable_config.go`
- Modify: `deploy/Dockerfile`
- Modify: `deploy/docker-entrypoint.sh`
- Modify: `deploy/test/dockerfile-multiarch-test.sh`

**Interfaces:**
- Produces `redisruntime.NewClient(config.RedisConfig) *redis.Client` with the existing bounded options.
- Produces `/app/ai-efficiency-prewarmer` and keeps `/app/ai-efficiency-server` as the default image process.
- Worker-only environment binding is `AE_PREWARMER_TIMEZONES`; deployment presence is the enable switch, so no application-level `enabled` flag exists.

```go
type PrewarmerConfig struct {
	Timezones []string `mapstructure:"timezones"`
}
```

Add this field to the existing `Config` struct:

```go
Prewarmer PrewarmerConfig `mapstructure:"prewarmer"`
```

Remove `TeamUsagePrewarmConfig`, `team_usage_prewarm.enabled`, and their
writable config output. Default `prewarmer.timezones` to the four approved
zones and bind only `AE_PREWARMER_TIMEZONES` for the worker.

- [ ] **Step 1: Write failing worker bootstrap and image tests**

Add tests for:

```go
func TestRunLoopRefreshesImmediatelyAndSerially(t *testing.T) {
	// Block the first Refresh, send multiple ticks, and assert max concurrent
	// Refresh calls is one. Unblock, then assert exactly one subsequent call.
}

func TestRunLoopContinuesAfterTransientRefreshError(t *testing.T) {
	// First call returns an error, second tick succeeds, cancellation exits nil.
}

func TestWorkerBootstrapDoesNotMigrateOrStartHTTPApplicationRuntime(t *testing.T) {
	// Inject DB/Redis/manager factories; assert Ping and Resolve are used while
	// Schema.Create, provider invalidation Start, router, auth, and SCM are absent.
}
```

Update `deploy/test/dockerfile-multiarch-test.sh` to require both build commands and both final paths.

- [ ] **Step 2: Run tests to verify RED**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./cmd/prewarmer ./internal/redisruntime -count=1
cd ..
bash deploy/test/dockerfile-multiarch-test.sh
```

Expected: Go package paths and worker binary checks fail because they do not exist.

- [ ] **Step 3: Extract only the shared Redis client construction**

Move the current server options unchanged into:

```go
func NewClient(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB,
		MaxRetries: -1, DialTimeout: time.Second, DialerRetries: 1,
		ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
		PoolTimeout: time.Second, MinIdleConns: 4,
		ContextTimeoutEnabled: true,
	})
}
```

Use it from both commands. Do not move unrelated server bootstrap into a framework package.

- [ ] **Step 4: Implement the minimal worker bootstrap**

`cmd/prewarmer/main.go` must:

1. load config without creating or rewriting the writable settings file;
2. require PostgreSQL DSN, Redis namespace, encryption key, and a non-empty normalized maximum-four timezone list;
3. open and ping PostgreSQL, configure the existing bounded DB pool, and construct Ent without calling `Schema.Create` or legacy migrations;
4. create Redis, ping it, register Redis/DB metrics, and construct `readcache.RedisStore` plus `PrewarmCache`;
5. create `relayruntime.Manager` with the current Relay factory but no invalidation bus and without calling `Start`;
6. construct `primaryTeamUsagePrewarmResolver`, `teamusage.Refresher`, and a dedicated `/metrics` HTTP server;
7. run one refresh immediately, then one serial call per `time.NewTicker(60*time.Second)` tick;
8. log only bounded outcome, duration, planned/published lane counts, and source class counts;
9. on SIGINT/SIGTERM cancel the active refresh, stop the ticker, shut down metrics, and close Redis/Ent/SQL/HTTP transports.

The loop surface is deterministic:

```go
func runLoop(ctx context.Context, refresher teamusage.Refresher, ticks <-chan time.Time, report func(error)) error {
	for {
		report(refresher.Refresh(ctx))
		select {
		case <-ctx.Done():
			return nil
		case <-ticks:
		}
	}
}
```

Ordinary `Refresh` errors are reported and retried. Invalid bootstrap configuration or an unavailable required dependency returns from bootstrap so `main` exits non-zero.

The command owns an unexported `primaryProviderBindingResolver` that queries the
enabled primary `ent.RelayProvider` row and delegates `Resolve` to
`relayruntime.Manager`. Do not export another Relay runtime abstraction merely
to share the old server-local adapter.

- [ ] **Step 5: Build both binaries into one image**

Build both commands in the existing backend builder:

```dockerfile
RUN export GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" && \
  export LDFLAGS="-X github.com/ai-efficiency/backend/internal/buildinfo.BuildVersion=${APP_VERSION} -X github.com/ai-efficiency/backend/internal/buildinfo.BuildCommit=${APP_COMMIT} -X github.com/ai-efficiency/backend/internal/buildinfo.BuildTime=${APP_BUILD_TIME}" && \
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -o /app/server ./cmd/server/ && \
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -o /app/prewarmer ./cmd/prewarmer/

COPY --from=backend-builder /app/server /app/ai-efficiency-server
COPY --from=backend-builder /app/prewarmer /app/ai-efficiency-prewarmer
```

Keep the default entrypoint behavior for Compose/systemd. Make `docker-entrypoint.sh` execute an explicit command when present and otherwise execute `/app/ai-efficiency-server`.

- [ ] **Step 6: Verify command boundaries and image contract**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/redisruntime cmd/prewarmer cmd/server/main.go
go test ./internal/redisruntime ./cmd/prewarmer ./cmd/server -count=1
go build ./cmd/server ./cmd/prewarmer
cd ..
bash deploy/test/dockerfile-multiarch-test.sh
docker build --platform linux/amd64 -f deploy/Dockerfile -t ai-efficiency-prewarmer-plan-check .
docker run --rm --entrypoint /bin/sh ai-efficiency-prewarmer-plan-check -ec \
  'test -x /app/ai-efficiency-server && test -x /app/ai-efficiency-prewarmer'
```

Expected: PASS; both binaries exist and are executable.

- [ ] **Step 7: Update this plan and commit**

```bash
git add backend/internal/redisruntime backend/cmd/prewarmer backend/cmd/server \
  backend/internal/config deploy/Dockerfile deploy/docker-entrypoint.sh \
  deploy/test/dockerfile-multiarch-test.sh \
  docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md
git commit -m "feat(backend): add stateless prewarm worker command"
```

---

### Task 5: Remove Superseded Artifacts and Verify the Backend

**Files:**
- Modify: `deploy/.env.example`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/docker-compose.local.yml`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md`
- Delete: all superseded prewarm plan/spec files listed in File Structure.

**Interfaces:**
- Produces the final repository contract: one current spec, one live plan, architecture reflecting web/worker runtime topology, and no Compose worker.

- [ ] **Step 1: Add a failing final-state audit**

Run before cleanup:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
rg -n 'AE_TEAM_USAGE_PREWARM|teamUsagePrewarmRuntime|PrewarmReaderSlot|startup cohort|replay guardian|partial_today|source_slot|segment_lease' \
  backend deploy docs/architecture.md docs/superpowers
```

Expected: matches remain in old runtime/config/operations/documents.

- [ ] **Step 2: Remove obsolete configuration and documents**

Remove Backend/Compose `AE_TEAM_USAGE_PREWARM_ENABLED` and `AE_TEAM_USAGE_PREWARM_TIMEZONES`; Compose launches only the default server binary. Remove all unmerged superseded plans/specs listed above. Do not rewrite unrelated historical specs.

Update the current spec status to `Implemented; staging acceptance pending` only after Tasks 1-4 actually pass. Keep the plan status and checkboxes aligned.

- [ ] **Step 3: Update current architecture and operations documentation**

Add one project-level runtime diagram to `docs/architecture.md`:

```text
Browser -> Backend Deployment (N stateless HTTP Pods)
                    | authorization-first schema-v3 reads
                    v
                  Redis <--- one refresh lease / immutable values / manifests
                    ^
                    | provider-wide source refresh
Prewarmer Deployment (1 optional Pod) -> Relay HTTP API
                    |
                    +-> PostgreSQL provider row/version
```

Document that Backend always attempts eligible schema-v3 reads, Worker absence is a normal miss, exact fallback remains authoritative, and Worker readiness is independent of Backend readiness. `deploy/README.md` must say Helm is the only initial worker deployment path.

- [ ] **Step 4: Run the complete backend and repository verification**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./...
go test -race ./internal/readcache ./internal/relay ./internal/teamusage ./internal/telemetry ./cmd/server ./cmd/prewarmer
go vet ./...
go build ./...
cd ..
bash deploy/test/dockerfile-multiarch-test.sh
docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null
docker compose --env-file deploy/.env.example -f deploy/docker-compose.external.yml config >/dev/null
rg -n 'teamUsagePrewarmRuntime|prepareTeamUsagePrewarm|PrewarmReaderSlot|recoverToday|SetIfLeasesOwned|AE_TEAM_USAGE_PREWARM' backend deploy
git diff --check
```

Expected: all tests/builds/config renders pass; final `rg` has no matches; `git diff --check` is empty.

- [ ] **Step 5: Review the final PR scope for overdesign**

```bash
git diff --stat perf/team-usage-scope-origin-baseline...HEAD
git diff --name-status perf/team-usage-scope-origin-baseline...HEAD
gh pr view 193 --json changedFiles,additions,deletions,isDraft,baseRefName,headRefName,url
```

Reject the result if the final diff still contains a scheduler in `cmd/server`, more than one worker lifecycle, any Pod-local correctness cache, any second deployment coordinator, or superseded execution ledgers. Record the reduced file/line counts in this plan without real runtime data.

- [ ] **Step 6: Commit the cleanup and architecture**

```bash
git add -A deploy docs backend
git commit -m "docs(teamusage): finalize stateless prewarm architecture"
```

---

### Task 6: Add the Optional Helm Worker from an Independent Worktree

**Files:**
- Create worktree: `/Users/admin/helm/.worktrees/ai-efficiency-prewarmer`
- Modify there: `ai-efficiency/values.yaml`
- Modify there: `ai-efficiency/values-staging.yaml`
- Modify there: `ai-efficiency/templates/_helpers.tpl`
- Create there: `ai-efficiency/templates/prewarmer-deployment.yaml`
- Modify there: `ai-efficiency/tests/staging-scripts-test.sh`

**Interfaces:**
- Consumes: Task 4 image paths and worker environment.
- Produces:

```yaml
prewarmer:
  enabled: false
  timezones:
    - UTC
    - Asia/Shanghai
    - America/Los_Angeles
    - Europe/Berlin
  resources: {}
```

- The template always renders `replicas: 1`, `strategy.type: Recreate`, the exact shared image tag, and command `/app/ai-efficiency-prewarmer`.

- [ ] **Step 1: Create the isolated Helm worktree without touching dirty main**

```bash
cd /Users/admin/helm
git status --short --branch
git fetch origin main
git worktree add /Users/admin/helm/.worktrees/ai-efficiency-prewarmer \
  -b perf/ai-efficiency-prewarmer origin/main
cd /Users/admin/helm/.worktrees/ai-efficiency-prewarmer
git status --short --branch
```

Expected: the new worktree is clean. The modified main-worktree staging secret and unrelated CSV files remain untouched.

- [ ] **Step 2: Add failing Helm render assertions**

Extend `ai-efficiency/tests/staging-scripts-test.sh` with disabled and enabled renders. Assert:

```bash
helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml \
  --set prewarmer.enabled=false >"${disabled_chart}"
! grep -q 'app.kubernetes.io/component: prewarmer' "${disabled_chart}"

helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml \
  -f ai-efficiency/values-staging.yaml \
  --set replicaCount=2 \
  --set prewarmer.enabled=true >"${enabled_chart}"
```

For the enabled render, parse YAML documents and assert one Backend Deployment with two replicas, one prewarmer Deployment with one replica, identical images, `Recreate`, worker command, no worker Service/Ingress/PVC/volume, and no `AE_PREWARMER_*` variable in the Backend container.

- [ ] **Step 3: Run the Helm test to verify RED**

```bash
cd /Users/admin/helm/.worktrees/ai-efficiency-prewarmer
bash ai-efficiency/tests/staging-scripts-test.sh
```

Expected: FAIL because `prewarmer.enabled=true` does not render a worker Deployment.

- [ ] **Step 4: Implement the worker Deployment**

Add a dedicated helper label set with `app.kubernetes.io/component: prewarmer`. The Deployment container must include only:

- shared image repository/tag/pull policy;
- command `/app/ai-efficiency-prewarmer`;
- `AE_DB_DSN`, `AE_ENCRYPTION_KEY`, Redis address/password/DB/namespace, metrics listen address, and `AE_PREWARMER_TIMEZONES`;
- a named metrics container port; and
- `.Values.prewarmer.resources` when non-empty.

Do not copy auth, OAuth, frontend, webhook, SCM, Service, Ingress, persistence, init-container, or Backend health probe configuration into the worker.

Set staging to `replicaCount: 2` and `prewarmer.enabled: true` only for staging acceptance. Keep default values disabled and do not alter production secrets or production rollout inputs.

- [ ] **Step 5: Verify Helm rendering and lint**

```bash
cd /Users/admin/helm/.worktrees/ai-efficiency-prewarmer
bash ai-efficiency/tests/staging-scripts-test.sh
helm lint ai-efficiency
helm template ai-efficiency ./ai-efficiency -f ai-efficiency/values.yaml >/tmp/ai-efficiency-disabled.yaml
helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml -f ai-efficiency/values-staging.yaml \
  --set image.tag=staging-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  >/tmp/ai-efficiency-prewarmer-enabled.yaml
```

Expected: tests and lint PASS; disabled output has no prewarmer; enabled output has exactly one worker and two Backend replicas.

- [ ] **Step 6: Commit and open the dependent Helm PR**

```bash
git add ai-efficiency/values.yaml ai-efficiency/values-staging.yaml \
  ai-efficiency/templates/_helpers.tpl ai-efficiency/templates/prewarmer-deployment.yaml \
  ai-efficiency/tests/staging-scripts-test.sh
git commit -m "feat(ai-efficiency): add optional prewarm worker"
git push -u origin perf/ai-efficiency-prewarmer
gh pr create --draft --base main --head perf/ai-efficiency-prewarmer \
  --title "feat(ai-efficiency): add optional prewarm worker" \
  --body $'## Summary\n- add one optional Recreate prewarmer Deployment\n- keep Backend replicas independent and free of worker configuration\n- use the application image worker binary at /app/ai-efficiency-prewarmer\n\n## Dependency\nDepends on the application PR that publishes both process binaries in one image.\n\n## Verification\n- bash ai-efficiency/tests/staging-scripts-test.sh\n- helm lint ai-efficiency'
```

The PR body must state that it depends on the application PR image containing `/app/ai-efficiency-prewarmer`; it must contain no credentials or staging secret values.

---

### Task 7: Prove Statelessness and Chrome Performance in Staging

**Files:**
- Modify with sanitized evidence: `docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md`
- Modify status only after all gates pass: `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md`

**Interfaces:**
- Staging URL: `https://ai-efficiency-staging.la3.agoralab.co/usage/team`
- Kubernetes namespace: `la3-ai-efficiency-staging`
- Acceptance window: default 30-day, `Asia/Shanghai`.
- Merge gate: three Chrome cold runs, median fully rendered `<=8s`, every immediate warm API lane `<1.5s`, HTTP 200, matching business hashes, four response-cache misses, and four prewarm full hits.

- [ ] **Step 1: Publish the exact application image and deploy staging only**

From the application worktree, build and push the immutable staging tag for `HEAD`, verify both architecture manifests, then use the Helm worktree's staging refresh/deploy path:

```bash
APP_WORKTREE=/Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
HELM_WORKTREE=/Users/admin/helm/.worktrees/ai-efficiency-prewarmer
APP_COMMIT="$(git -C "$APP_WORKTREE" rev-parse HEAD)"
IMAGE_TAG="staging-${APP_COMMIT}"

docker buildx use static-spaces-release-builder
docker buildx build --platform linux/amd64,linux/arm64 \
  -f "$APP_WORKTREE/deploy/Dockerfile" \
  -t "ghcr.io/lichking-2234/ai-efficiency:${IMAGE_TAG}" \
  --push "$APP_WORKTREE"
docker buildx imagetools inspect "ghcr.io/lichking-2234/ai-efficiency:${IMAGE_TAG}"

cd "$HELM_WORKTREE"
./ai-efficiency/scripts/refresh-staging-upgrade-values.sh "$IMAGE_TAG"
```

Follow `docs/staging-playbook.md` for the existing paused restore and atomic rollout. Do not modify or deploy production.

- [ ] **Step 2: Verify one Worker and Backend scaling independence**

```bash
kubectl -n la3-ai-efficiency-staging get deploy,pod -o wide
kubectl -n la3-ai-efficiency-staging get deploy \
  ai-efficiency-staging ai-efficiency-staging-prewarmer -o json
kubectl -n la3-ai-efficiency-staging rollout status deploy/ai-efficiency-staging --timeout=10m
kubectl -n la3-ai-efficiency-staging rollout status deploy/ai-efficiency-staging-prewarmer --timeout=10m
```

Assert Backend replicas are two, Worker replicas are one, images match, and no prewarm scheduler log or environment exists in Backend Pods. Record worker refresh/source counters, scale Backend `1 -> 2`, wait two intervals, and assert the Worker Pod UID and one-cycle-per-minute behavior are unchanged.

Use the explicit scale sequence:

```bash
kubectl -n la3-ai-efficiency-staging scale deploy/ai-efficiency-staging --replicas=1
kubectl -n la3-ai-efficiency-staging rollout status deploy/ai-efficiency-staging --timeout=10m
kubectl -n la3-ai-efficiency-staging scale deploy/ai-efficiency-staging --replicas=2
kubectl -n la3-ai-efficiency-staging rollout status deploy/ai-efficiency-staging --timeout=10m
```

- [ ] **Step 3: Verify restart reconstruction from Redis**

Delete only the Worker Pod:

```bash
WORKER_POD="$(kubectl -n la3-ai-efficiency-staging get pod \
  -l app.kubernetes.io/component=prewarmer -o jsonpath='{.items[0].metadata.name}')"
kubectl -n la3-ai-efficiency-staging delete pod "$WORKER_POD"
kubectl -n la3-ai-efficiency-staging rollout status deploy/ai-efficiency-staging-prewarmer --timeout=10m
```

Assert the replacement performs an immediate successful refresh using hard-valid history from Redis, publishes current/today generations, and has no PVC, local checkpoint, startup marker, recovery mode, or effect on Backend readiness.

- [ ] **Step 4: Run three real Chrome cold navigations**

Use installed Google Chrome with a fresh profile for each run and a valid authenticated staging session established outside the repository. Before each run, delete only the four typed Team Usage response-cache keys for the representative scope; keep schema-v3 prewarm manifests and immutable values. Verify counters show four response-cache misses and four prewarm `full_hit` outcomes.

For each run record sanitized values only:

```text
run: 1|2|3
http_status: 200
window: 30d Asia/Shanghai
fully_rendered_ms: integer
summary_ms: integer
trend_ms: integer
members_ms: integer
organization_ms: integer
warm_max_ms: integer
business_hash_match: true|false
response_cache_misses: 4
prewarm_full_hits: 4
```

Do not record cookies, tokens, usernames, emails, Relay IDs, cache keys, or response bodies. Sort the three `fully_rendered_ms` values and compare the middle value to `8000`; assert each `warm_max_ms < 1500`.

- [ ] **Step 5: Run final health and fallback checks**

Temporarily scale only the Worker to zero and wait beyond the three-minute manifest lifetime. Verify Backend liveness/readiness remain HTTP 200 and one Team Usage request completes through exact fallback with the same business hash. Restore the Worker to one and verify a new generation appears.

Then run:

```bash
curl -fsS https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/live
curl -fsS https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/ready
kubectl -n la3-ai-efficiency-staging get deploy,pod
```

- [ ] **Step 6: Record sanitized acceptance and close the implementation ledger**

If and only if every gate passes, set the spec status to `Implemented and staging-verified on 2026-07-25`, record all three timings plus median and final staging topology in this plan, and run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
git add docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md \
  docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md
git commit -m "docs(teamusage): record stateless prewarm acceptance"
```

If any gate fails, leave the failing checkbox open, set the plan status to the exact remaining gap, keep PR #193 draft, and do not claim performance acceptance.

## Final Review Checklist

- [ ] Every approved spec requirement maps to a task and a verification command.
- [ ] No placeholder, real credential, real identity, or raw response data appears in the plan or final diff.
- [ ] `Refresher`, cache, reader, metrics, config, Helm values, and command signatures are consistent across tasks.
- [ ] Backend has no worker lifecycle; Worker has no application HTTP/auth/SCM lifecycle.
- [ ] Redis schema v3 is read/write isolated from v2 and publication checks one refresh token atomically.
- [ ] The final PR removes superseded lifecycle code and execution ledgers instead of layering new machinery over them.
- [ ] Backend, race, vet, build, Docker, Helm, stateless restart, fallback, and Chrome acceptance evidence all exist before merge-ready status.
