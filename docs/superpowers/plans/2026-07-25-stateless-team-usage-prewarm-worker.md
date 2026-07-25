# Stateless Team Usage Prewarm Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace PR #193's per-Backend-Pod prewarm scheduler with one optional stateless worker process while preserving exact Team Usage results, authorization-first Redis reads, and PR #192 fallback behavior.

**Architecture:** The existing provider-wide Relay sources, timezone model, zstd codec, immutable values, and publish-last manifests remain in the modular monolith. Backend Pods construct one read-only schema-v3 Redis reader and never schedule or write prewarm data; one separately deployed `ai-efficiency-prewarmer` process runs a serial 60-second refresh loop, coordinates through one Redis lease, and publishes each timezone independently.

**Tech Stack:** Go 1.24, Ent, go-redis v9, miniredis, Prometheus client_golang, zap, Docker Buildx, Helm, Kubernetes, Google Chrome.

**Status:** Implementation, Task 7 staging acceptance, final review remediation, and the telemetry-classification staging replay completed on 2026-07-25. The original acceptance image `staging-7924f8ce750688300c5913058f851cbb8f0903e5` passed the request-metrics replay, three Chrome runs, stateless scale/restart checks, and the controlled manifest-expiry fallback check at Helm revision 82. Chrome fully rendered median was 7021 ms and every immediate warm lane was below 500 ms. The live fallback comparison used the current spec's sampling-aware semantic equality contract: exact shape/cardinality and every stable field matched, while only the four approved current/today-derived usage leaves changed across the source-sampling interval. The initial wrong-cluster denial, revision 79 Worker resource admission failure, and first-image missing request metric are retained below as resolved history. The whole-branch review found one telemetry-classification mismatch: corrupt Redis manifests/values used exact fallback correctly but were counted as `fallback` instead of `invalid`. That mismatch was fixed test-first with one package-local sentinel; fresh backend, race, vet, build, Docker, Compose, Helm, static, and hygiene verification passed, and the follow-up review found no Critical, Important, or Minor issues. Application commit `14ae34ae86762a27a2bc48688358fb6d17a33632` was then published as an amd64/arm64 staging image and deployed without database restore at Helm revision 83. Backend ended 2/2, Worker 1/1, both used the exact image digest, liveness/readiness returned HTTP 200, and the Worker completed consecutive four-lane refreshes. Tasks 1-5 use `/Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm`; Task 6 uses the independent Helm worktree `/Users/admin/helm/.worktrees/ai-efficiency-prewarmer` and leaves `/Users/admin/helm`'s dirty main worktree untouched; Task 7 records sanitized staging evidence only.

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
- Modify: `backend/internal/teamusage/prewarm_cache.go`
- Modify: `backend/internal/teamusage/prewarm_cache_test.go`
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

- [x] **Step 1: Write failing refresh-cycle tests**

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

- [x] **Step 2: Run tests to verify RED**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter' -count=1
```

Expected: FAIL because `Refresher`, `RefresherOptions`, and `NewRefresher` do not exist.

- [x] **Step 3: Implement the minimal refresher**

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

- [x] **Step 4: Verify focused behavior and races**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/teamusage/prewarm_refresher.go internal/teamusage/prewarm_refresher_test.go internal/teamusage/prewarm_source.go internal/teamusage/prewarm_source_test.go
go test ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter|TestPrewarmSource' -count=1
go test -race ./internal/teamusage -run 'TestRefresher|TestLocalSourceCallLimiter' -count=1
```

Expected: PASS; race detector reports no races.

**Task 1 verification (2026-07-25):** the focused refresher/source suite passed, the focused race suite passed without race reports, and the controller-approved exact refresh-lease key/isolation test passed. Test fixtures use synthetic IDs only.

**Task 1 review follow-up:**

- [x] Add blocked-lane, provider-reversion fence, lease-restoration fence, and deterministic contention tests; verify covering RED.
- [x] Publish each complete timezone as soon as its own source work finishes.
- [x] Make provider-version mismatch and refresh-lease loss irreversible for the current cycle.
- [x] Run focused GREEN and race verification, append the SDD report, and commit the review fixes.

- [x] **Step 5: Update this plan and commit**

Mark Task 1 complete, record only sanitized test results, then run:

```bash
git add backend/internal/teamusage/prewarm_refresher.go \
  backend/internal/teamusage/prewarm_refresher_test.go \
  backend/internal/teamusage/prewarm_cache.go \
  backend/internal/teamusage/prewarm_cache_test.go \
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

- [x] **Step 1: Write failing schema-v3 and read-only Backend tests**

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

- [x] **Step 2: Run tests to verify RED**

Run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/readcache ./internal/teamusage ./cmd/server \
  -run 'TestPrewarmCacheUsesSchemaV3|TestPrewarmReaderNeverCallsRelay|TestServiceUsesDirectPrewarmReader|TestRedisStoreSetIfLeaseOwned' -count=1
```

Expected: FAIL because schema is v2, the reader requires a limiter/provider, and Backend wiring still owns a mutable runtime.

**Task 2 RED verification (2026-07-25):** the exact focused command failed at compile time because `PrewarmReadInvalid`, the read-only `NewPrewarmReader(cache, options)` signature, and the direct `Service.prewarmReader` dependency did not exist. The readcache and server targets passed their selected tests.

- [x] **Step 3: Simplify the Redis contract**

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

- [x] **Step 4: Make the reader permanently read-only**

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
	if errors.Is(err, errPrewarmCacheInvalid) {
		return nil, PrewarmReadInvalid, err
	}
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

Corrupt, expired, missing, unsupported-window, roster-incomplete, provider-version-mismatched, and Redis-error cases immediately return to the existing exact loader. Corrupt manifest/value decode or validation errors carry the package-local invalid-cache sentinel and record `invalid`; Redis transport errors remain `fallback`. A complete sparse provider trend still contributes zero for an omitted authorized user only when the current-stats roster proves that user exists.

- [x] **Step 5: Remove Backend lifecycle ownership and wire the direct reader**

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

- [x] **Step 6: Verify stateless Backend behavior**

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

**Task 2 verification (2026-07-25):** the complete readcache, Team Usage, handler, and server package suite passed. The exact readcache, Team Usage, and server race suite passed without race reports; the macOS linker emitted only its known `LC_DYSYMTAB` warning. The removal scan found `LeaseTTL` only in the shared store and non-prewarm caches/tests, with no old runtime, reader slot, recovery, or multi-lease publication symbols.

- [x] **Step 7: Update this plan and commit**

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

- [x] **Step 1: Write the exact metric contract test**

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

- [x] **Step 2: Run the test to verify RED**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./internal/telemetry ./cmd/server -run 'TestTeamUsagePrewarm|TestProductionCacheMetrics' -count=1
```

Expected: FAIL because the current implementation registers more than five prewarm families and uses duplicated string allowlists.

- [x] **Step 3: Implement the closed typed vocabulary and recorder**

Define each enum, its `Valid() bool`, and its ordered `All...()` slice once in `teamusage/prewarm_metrics.go`. Telemetry must iterate those exported typed values for preinitialization and must not repeat string allowlists.

Remove prewarm-specific Redis command metrics from `PrewarmCache`; the existing Redis pool metrics remain authoritative. `PrewarmSource` records only its source class/outcome/duration. `Refresher` records one refresh result and lane success timestamps. `PrewarmReader` records one request result.

Update the dashboard to no more than five focused panels, each backed by one approved family. Remove startup, cohort, lease-TTL, scheduler-tick, and generation-size operations guidance.

- [x] **Step 4: Verify metrics and bounded cardinality**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
gofmt -w internal/teamusage/prewarm_metrics.go internal/teamusage/prewarm_cache.go \
  internal/teamusage/prewarm_source.go internal/teamusage/prewarm_refresher.go \
  internal/teamusage/prewarm_reader.go internal/telemetry/team_usage_prewarm.go \
  internal/telemetry/team_usage_prewarm_test.go cmd/server/cache_metrics.go cmd/server/cache_metrics_test.go
go test ./internal/teamusage ./internal/telemetry ./cmd/server -run 'Prewarm|ProductionCacheMetrics|Dashboard' -count=1
```

Expected: PASS and exactly five `ai_efficiency_team_usage_prewarm_*` families.

- [x] **Step 5: Update this plan and commit**

```bash
git add backend/internal/teamusage backend/internal/telemetry backend/cmd/server/cache_metrics* \
  deploy/observability docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md
git commit -m "refactor(backend): simplify prewarm telemetry"
```

**Task 3 production-wiring review follow-up (2026-07-25):**

- [x] Add a focused server-boundary RED test that drives a real 30-day
  `Asia/Shanghai` prewarm miss through miniredis, gathers the production
  Prometheus registry, and requires `team_usage_prewarm_request_total{outcome="miss"}=1`
  with no Backend timezone lane series.
- [x] Allow a production prewarm recorder with no configured timezones, inject
  that recorder into the read-only Backend `PrewarmReader`, and keep Worker
  timezone ownership unchanged.
- [x] Capture the expected missing-wiring RED, then pass the focused GREEN and
  the adjacent Team Usage, telemetry, server, and dashboard metric suite.
- [x] Keep Reader/cache behavior, DTOs, Redis schema, routes, Worker config,
  dashboard families, and Backend lifecycle ownership unchanged.

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

- [x] **Step 1: Write failing worker bootstrap and image tests**

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

- [x] **Step 2: Run tests to verify RED**

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm/backend
go test ./cmd/prewarmer ./internal/redisruntime -count=1
cd ..
bash deploy/test/dockerfile-multiarch-test.sh
```

Expected: Go package paths and worker binary checks fail because they do not exist.

- [x] **Step 3: Extract only the shared Redis client construction**

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

- [x] **Step 4: Implement the minimal worker bootstrap**

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

- [x] **Step 5: Build both binaries into one image**

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

- [x] **Step 6: Verify command boundaries and image contract**

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

- [x] **Step 7: Update this plan and commit**

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

- [x] **Step 1: Add a failing final-state audit**

Run before cleanup:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
rg -n 'AE_TEAM_USAGE_PREWARM|teamUsagePrewarmRuntime|PrewarmReaderSlot|startup cohort|replay guardian|partial_today|source_slot|segment_lease' \
  backend deploy docs/architecture.md docs/superpowers
```

Expected: matches remain in old runtime/config/operations/documents.

- [x] **Step 2: Remove obsolete configuration and documents**

Remove Backend/Compose `AE_TEAM_USAGE_PREWARM_ENABLED` and `AE_TEAM_USAGE_PREWARM_TIMEZONES`; Compose launches only the default server binary. Remove all unmerged superseded plans/specs listed above. Do not rewrite unrelated historical specs.

Update the current spec status to `Implemented; staging acceptance pending` only after Tasks 1-4 actually pass. Keep the plan status and checkboxes aligned.

- [x] **Step 3: Update current architecture and operations documentation**

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

- [x] **Step 4: Run the complete backend and repository verification**

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

- [x] **Step 5: Review the final PR scope for overdesign**

```bash
git diff --stat perf/team-usage-scope-origin-baseline...HEAD
git diff --name-status perf/team-usage-scope-origin-baseline...HEAD
gh pr view 193 --json changedFiles,additions,deletions,isDraft,baseRefName,headRefName,url
```

Reject the result if the final diff still contains a scheduler in `cmd/server`, more than one worker lifecycle, any Pod-local correctness cache, any second deployment coordinator, or superseded execution ledgers. Record the reduced file/line counts in this plan without real runtime data.

**Task 5 scope review (2026-07-25):** The final local diff against
`perf/team-usage-scope-origin-baseline` is 55 files, 12,106 additions, and 108
deletions, reduced from the current unpushed PR metadata of 68 files, 19,050
additions, and 108 deletions. Static review found no prewarm scheduler in
`cmd/server`, exactly one worker run loop in `cmd/prewarmer`, no Pod-local
completed-result cache, one deployment-wide refresh owner, and no superseded
execution ledger. Immutable-value write claims are data-level create fencing,
not a second lifecycle coordinator. No runtime or staging measurements are
claimed by this review.

- [x] **Step 6: Commit the cleanup and architecture**

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
- Modify there: `ai-efficiency/docs/deploy.md`
- Modify there: `docs/staging-playbook.md`

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

- [x] **Step 1: Create the isolated Helm worktree without touching dirty main**

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

- [x] **Step 2: Add failing Helm render assertions**

Extend `ai-efficiency/tests/staging-scripts-test.sh` with disabled and enabled renders. Assert:

```bash
cat >"${render_input_values}" <<'YAML'
secretEnv:
  AE_DB_DSN: "postgres://app:test-password@db.example.com:5432/ai_efficiency?sslmode=disable"
  AE_AUTH_JWT_SECRET: "test-jwt-secret"
  AE_ENCRYPTION_KEY: "test-encryption-key"
  AE_REDIS_PASSWORD: "test-redis-password"
postgres:
  auth:
    password: "test-postgres-password"
  restore:
    sourceDbDsn: "postgres://app:test-password@db.example.com:5432/ai_efficiency?sslmode=require"
YAML

helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml \
  -f "${render_input_values}" >"${disabled_chart}"
! grep -q 'app.kubernetes.io/component: prewarmer' "${disabled_chart}"

helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml \
  -f ai-efficiency/values-staging.yaml \
  -f "${render_input_values}" >"${enabled_chart}"
```

For the enabled render, parse YAML documents and assert one Backend Deployment with two replicas, one prewarmer Deployment with one replica, identical images, `Recreate`, worker command, no worker Service/Ingress/PVC/volume, and no `AE_PREWARMER_*` variable in the Backend container.

- [x] **Step 3: Run the Helm test to verify RED**

```bash
cd /Users/admin/helm/.worktrees/ai-efficiency-prewarmer
bash ai-efficiency/tests/staging-scripts-test.sh
```

Expected: FAIL because `prewarmer.enabled=true` does not render a worker Deployment.

- [x] **Step 4: Implement the worker Deployment**

Add a dedicated helper label set with `app.kubernetes.io/component: prewarmer`. The Deployment container must include only:

- shared image repository/tag/pull policy;
- command `/app/ai-efficiency-prewarmer`;
- `AE_DB_DSN`, `AE_ENCRYPTION_KEY`, Redis address/password/DB/namespace, metrics listen address, and `AE_PREWARMER_TIMEZONES`;
- a named metrics container port; and
- `.Values.prewarmer.resources` when non-empty.

Do not copy auth, OAuth, frontend, webhook, SCM, Service, Ingress, persistence, init-container, or Backend health probe configuration into the worker.

Set staging to `replicaCount: 2` and `prewarmer.enabled: true` only for staging acceptance. Keep default values disabled and do not alter production secrets or production rollout inputs.

- [x] **Step 5: Verify Helm rendering and lint**

```bash
cd /Users/admin/helm/.worktrees/ai-efficiency-prewarmer
bash ai-efficiency/tests/staging-scripts-test.sh
helm lint ai-efficiency
helm template ai-efficiency ./ai-efficiency -f ai-efficiency/values.yaml \
  --set-string secretEnv.AE_DB_DSN='postgres://app:test-password@db.example.com:5432/ai_efficiency?sslmode=disable' \
  --set-string secretEnv.AE_AUTH_JWT_SECRET='test-jwt-secret' \
  --set-string secretEnv.AE_ENCRYPTION_KEY='test-encryption-key' \
  >/tmp/ai-efficiency-disabled.yaml
helm template ai-efficiency-staging ./ai-efficiency \
  -f ai-efficiency/values.yaml -f ai-efficiency/values-staging.yaml \
  --set image.tag=staging-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --set-string secretEnv.AE_DB_DSN='postgres://app:test-password@db.example.com:5432/ai_efficiency?sslmode=disable' \
  --set-string secretEnv.AE_AUTH_JWT_SECRET='test-jwt-secret' \
  --set-string secretEnv.AE_ENCRYPTION_KEY='test-encryption-key' \
  --set-string secretEnv.AE_REDIS_PASSWORD='test-redis-password' \
  --set-string postgres.auth.password='test-postgres-password' \
  --set-string postgres.restore.sourceDbDsn='postgres://app:test-password@db.example.com:5432/ai_efficiency?sslmode=require' \
  >/tmp/ai-efficiency-prewarmer-enabled.yaml
```

Expected: tests and lint PASS; disabled output has no prewarmer; enabled output has exactly one worker and two Backend replicas. The staging playbook contract also proves Phase A pauses Backend and Worker, Phase B restores with the Worker disabled, Phase C enables the Worker only after readiness, and rollback selects only a prior complete Phase C revision.

- [x] **Step 6: Commit and open the dependent Helm PR**

```bash
git add ai-efficiency/values.yaml ai-efficiency/values-staging.yaml \
  ai-efficiency/templates/_helpers.tpl ai-efficiency/templates/prewarmer-deployment.yaml \
  ai-efficiency/tests/staging-scripts-test.sh ai-efficiency/docs/deploy.md \
  docs/staging-playbook.md
git commit -m "feat(ai-efficiency): add optional prewarm worker"
git push -u origin perf/ai-efficiency-prewarmer
/Users/admin/.local/bin/atlassian bitbucket pr create TOOL helm \
  --title "feat(ai-efficiency): add optional prewarm worker" \
  --description $'PENDING DEPENDENCY\n\nDepends on https://github.com/LichKing-2234/ai-efficiency/pull/193 publishing an image that contains /app/ai-efficiency-prewarmer.\n\nVerification:\n- bash ai-efficiency/tests/staging-scripts-test.sh\n- helm lint ai-efficiency' \
  --from-ref refs/heads/perf/ai-efficiency-prewarmer \
  --to-ref refs/heads/main
```

The PR body must state that it depends on the application PR image containing `/app/ai-efficiency-prewarmer`; it must contain no credentials or staging secret values. Internal Bitbucket does not support draft PRs, so the dependent PR remains `OPEN` with `PENDING DEPENDENCY` as its first description line.

**Task 6 verification (2026-07-25):** isolated Helm worktree and dirty-main preservation checks passed; RED/GREEN render tests, chart lint, sanitized default/staging/boundary/resource renders, and the Phase A/B/C playbook contract passed. Review follow-ups fixed suffix-preserving 63-character names, restore-time Worker isolation, completed-revision rollback selection, and Backend Service test precision. The latest resource-admission follow-up added staging Worker CPU, memory, and ephemeral-storage requests/limits, passed chart tests, server-side Pod admission, and independent review, and advanced the pushed branch head to `5c333cb470a3691101fc8e1836516861e22152f9`. Internal Bitbucket PR [TOOL/helm #5](https://bitbucket.agoralab.co/projects/TOOL/repos/helm/pull-requests/5) remains the dependent application-delivery PR with the dependency explicitly recorded. The final staging deployment consumed this reviewed head; production was not targeted.

---

### Task 7: Prove Statelessness and Chrome Performance in Staging

**Files:**
- Modify with sanitized evidence: `docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md`
- Modify status only after all gates pass: `docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md`

**Interfaces:**
- Staging URL: `https://ai-efficiency-staging.la3.agoralab.co/usage/team`
- Kubernetes context: `luxuhui-agora-hci-losangeles3s`
- Kubernetes namespace: `la3-ai-efficiency-prod` (shared namespace; staging
  resources are isolated by the release labels)
- Helm release: `ai-efficiency-staging`
- Acceptance window: default 30-day, `Asia/Shanghai`.
- Merge gate: three Chrome cold runs, median fully rendered `<=8s`, every immediate warm API lane `<1.5s`, HTTP 200, matching same-generation business hashes, four response-cache misses, four prewarm full hits, and the current spec's sampling-aware manifest-expiry semantic equality contract.

- [x] **Step 1: Publish the exact application image and deploy staging only**

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

**Task 7 Step 1 evidence (2026-07-25):** The exact image
`staging-b32e931a95a42d6d8d6dae1813827955b8557ca1` was published as an OCI
index containing `linux/amd64` and `linux/arm64`, with digest
`sha256:a3465a8a5745e8f8e8c169d335ddbab378a48788bf2b1f6e03cbf435329a738e`.
Staging Phase A completed at revision 77 and Phase B at revision 78 with two
ready Backend Pods on that image and HTTP 200 readiness. Phase C revision 79
failed admission because the Worker had no CPU, memory, or ephemeral-storage
requests or limits; the atomic rollback deployed revision 80 from Phase B.
After rollback the Backend was 2/2 available on the exact image, the Worker
Deployment was absent, and liveness/readiness were both HTTP 200. The reviewed
Task 6 Helm follow-up then added the missing staging resources and passed chart,
server-side Pod admission, and independent review gates. One corrected Phase C
completed at revision 81 without repeating Phase A, Phase B, or the restore:
Backend was 2/2, Worker was 1/1 with `Recreate`, both used the exact image, and
readiness remained HTTP 200. No production release was targeted.

**Final-image replay:** The exact application HEAD
`7924f8ce750688300c5913058f851cbb8f0903e5` was published as
`staging-7924f8ce750688300c5913058f851cbb8f0903e5`, OCI index digest
`sha256:032a87c5ba8a5a2d94471c05032611e6d6c4c41ac80241978bd9e440f8913f16`,
for `linux/amd64` and `linux/arm64`. Phase C-only revision 82 deployed the
request-metrics fix without repeating Phase A, Phase B, or restore. Backend was
2/2, Worker 1/1, both images matched, and liveness/readiness were HTTP 200.

**Telemetry-classification replay:** Application commit
`14ae34ae86762a27a2bc48688358fb6d17a33632` was published as
`staging-14ae34ae86762a27a2bc48688358fb6d17a33632`, OCI index digest
`sha256:7a373731028ed1fa010c42d9b517f9a25a5e6986884514b983ec349996aee9d8`,
for `linux/amd64` and `linux/arm64`. Phase C-only revision 83 preserved the
existing staging JWT, database, Redis, and completed restore state. Backend was
2/2 and Worker 1/1 on the exact image digest, liveness/readiness returned HTTP
200 with database, Redis, and Relay up, and the Worker completed consecutive
refreshes in 4.466 and 4.207 seconds with all four timezone lanes published.
Production remained revision 69 and ready on `v0.1.0-preview.73`.

- [x] **Step 2: Verify one Worker and Backend scaling independence**

```bash
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  get deploy,pod -o wide
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod get deploy \
  ai-efficiency-staging ai-efficiency-staging-prewarmer -o json
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  rollout status deploy/ai-efficiency-staging --timeout=10m
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  rollout status deploy/ai-efficiency-staging-prewarmer --timeout=10m
```

Assert Backend replicas are two, Worker replicas are one, images match, and no prewarm scheduler log or environment exists in Backend Pods. Record worker refresh/source counters, scale Backend `1 -> 2`, wait two intervals, and assert the Worker Pod UID and one-cycle-per-minute behavior are unchanged.

Use the explicit scale sequence:

```bash
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  scale deploy/ai-efficiency-staging --replicas=1
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  rollout status deploy/ai-efficiency-staging --timeout=10m
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  scale deploy/ai-efficiency-staging --replicas=2
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  rollout status deploy/ai-efficiency-staging --timeout=10m
```

**Task 7 Step 2 evidence (2026-07-25):** Backend scaled `2 -> 1 -> 2` and
returned 2/2 ready while Worker stayed 1/1 with an unchanged Pod UID. Backend
had no prewarm/scheduler environment entries and no scheduler, prewarm writer,
or refresh-cycle log matches. Over a fixed 125-second post-scale window the
Worker completed exactly two refreshes and twelve source calls, six per moving
cycle, without changing UID. Backend and Worker continued using the exact same
image.

**Final-image replay:** The same assertions passed again on
`staging-7924f8ce750688300c5913058f851cbb8f0903e5`: Backend completed
`2 -> 1 -> 2`, Worker UID was unchanged, and the 125-second window recorded
exactly two refreshes and twelve source calls.

- [x] **Step 3: Verify restart reconstruction from Redis**

Delete only the Worker Pod:

```bash
WORKER_POD="$(kubectl --context luxuhui-agora-hci-losangeles3s \
  -n la3-ai-efficiency-prod get pod \
  -l app.kubernetes.io/component=prewarmer -o jsonpath='{.items[0].metadata.name}')"
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  delete pod "$WORKER_POD"
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  rollout status deploy/ai-efficiency-staging-prewarmer --timeout=10m
```

Assert the replacement performs an immediate successful refresh using hard-valid history from Redis, publishes current/today generations, and has no PVC, local checkpoint, startup marker, recovery mode, or effect on Backend readiness.

**Task 7 Step 3 evidence (2026-07-25):** Deleting only the Worker Pod
produced a replacement UID and one successful immediate refresh within 25
seconds. Reset metrics recorded one directory call, one current-stats call,
four `today_hour` calls, zero `history_6d` or `history_29d` calls, and four lane
success timestamps after restart. This proves Redis-derived hard-valid history
reuse plus new current/today publication. The Worker had no volumes,
initContainers, PVCs, checkpoint/recovery/startup-marker log path, or local
state surface. Backend stayed 2/2 and readiness stayed HTTP 200 throughout.

**Final-image replay:** Deleting only the Worker Pod on
`staging-7924f8ce750688300c5913058f851cbb8f0903e5` produced a new UID and an
immediate successful refresh observed within 24 seconds. Reset counters were
directory 1, current stats 1, `today_hour` 4, both history classes 0, and four
published lanes. There was no Worker PVC, local data mount, checkpoint, or
recovery path; Backend remained 2/2 and ready.

- [x] **Step 4: Run three real Chrome cold navigations**

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

**Task 7 Step 4 blocked evidence (2026-07-25):** A temporary authenticated
Chrome discovery profile completed the default 30-day `Asia/Shanghai` page
with four HTTP 200 lanes. Redis pre-scan found zero typed keys; discovery then
created exactly one key in each typed lane. Deleting only those four keys
atomically kept the schema-v3 prewarm key count unchanged at 44. Aggregate
metrics from both Backend Pods recorded four typed-cache misses, but neither Pod
exported `ai_efficiency_team_usage_prewarm_request_total`. The deployed
`b32e931a95a42d6d8d6dae1813827955b8557ca1` server constructed
`NewPrewarmReader` with empty options, selecting no-op request metrics. Therefore
the mandatory aggregate `full_hit=4` delta was unobservable on that image.
Execution stopped before measured run 1; no timing, median, warm-lane, or
business-hash acceptance is claimed.

**Resolved on final image:** Both Backend Pods exported the request metric
family. A non-measured discovery produced exactly four typed-cache misses and
four prewarm full hits. Three isolated installed-Chrome profiles then recorded:

| Run | Fully rendered | Summary | Trend | Members | Organization | Warm max | Hash match | Misses | Full hits |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | :---: | ---: | ---: |
| 1 | 7720 ms | 4767 ms | 5578 ms | 5060 ms | 5581 ms | 470 ms | true | 4 | 4 |
| 2 | 6295 ms | 4385 ms | 4110 ms | 4931 ms | 4542 ms | 398 ms | true | 4 | 4 |
| 3 | 7021 ms | 5286 ms | 4389 ms | 5265 ms | 4400 ms | 461 ms | true | 4 | 4 |

All document, cold-lane, and warm-lane responses were HTTP 200 for the default
30-day `Asia/Shanghai` window. Fully rendered median was 7021 ms, and schema-v3
prewarm manifests/immutable values were retained across every exact four-key
typed-cache reset.

- [x] **Step 5: Run final health and fallback checks**

Temporarily scale only the Worker to zero and wait beyond the three-minute
manifest lifetime. Verify Backend liveness/readiness remain HTTP 200 and one
Team Usage request completes through exact fallback. Restore the Worker to one
and verify a new generation appears. Compare fallback and restored-prewarm
responses using the current spec's sampling-aware live semantic equality
contract; same-source deterministic automated equivalence remains strict full
JSON.

Then run:

```bash
curl -fsS https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/live
curl -fsS https://ai-efficiency-staging.la3.agoralab.co/api/v1/health/ready
kubectl --context luxuhui-agora-hci-losangeles3s -n la3-ai-efficiency-prod \
  get deploy,pod -l app.kubernetes.io/instance=ai-efficiency-staging
```

**Task 7 Step 5 evidence (2026-07-25):** Worker was scaled to zero and the
four manifests expired naturally beyond their three-minute TTL while Backend
remained 2/2 and liveness/readiness stayed HTTP 200. The controlled comparison
issued exactly one fallback request and one restored-prewarm request. Fallback
returned HTTP 200 with one summary-cache miss and one prewarm miss; restored
prewarm returned HTTP 200 with one summary-cache miss and one prewarm full hit.
The replacement Worker UID changed, a successful four-lane refresh published
four new manifests, and no Backend replica changed.

The sanitized salted comparison retained no values or reusable hashes. Both
responses had identical top-level and business keys, 17 scalar leaves, four
objects, zero arrays, no missing paths, and no shape, cardinality, or ordering
differences. API code, `scope_version`, all six window fields, member and Relay
member counts, unavailable status/reason, and unit label matched exactly. Only
`range_actual_cost`, `range_total_tokens`, `today_actual_cost`, and
`total_actual_cost` differed. Fallback completion preceded restored-prewarm
start by 101.671 seconds; the first restored source read began 28.540 seconds
after fallback completion, and two successful refreshes completed before the
restored request because the aggregate counter baselines were collected first.
This confirms source-time drift limited to the current/today-derived fields,
not structural or composer divergence. The strict same-source
`TestPrewarmEquivalenceMatchesExactPublicLanesCursorsFreshnessAndKeys` test also
passed in a fresh focused run.

The initial whole-response hash comparison is retained as diagnostic history:
it compared source samples taken several minutes apart and therefore changed.
Its canonicalizer removed freshness/request metadata and sorted object keys,
but a strict live hash still cannot distinguish valid current/today growth from
composer divergence for an active team. The current spec corrects that live
acceptance contract without relaxing same-source automated equality.

- [x] **Step 6: Record sanitized acceptance and close the implementation ledger**

If and only if every gate passes, set the spec status to `Implemented and staging-verified on 2026-07-25`, record all three timings plus median and final staging topology in this plan, and run:

```bash
cd /Users/admin/ai-efficiency/.worktrees/team-usage-daily-prewarm
git add docs/superpowers/plans/2026-07-25-stateless-team-usage-prewarm-worker.md \
  docs/superpowers/specs/2026-07-25-stateless-team-usage-prewarm-worker-design.md
git commit -m "docs(teamusage): record stateless prewarm acceptance"
```

If any gate fails, leave the failing checkbox open, set the plan status to the exact remaining gap, keep PR #193 draft, and do not claim performance acceptance.

**Task 7 Step 6 evidence (2026-07-25):** The current spec status and live
semantic equality contract, all three Chrome samples and median, final staging
topology, resolved historical failures, and sanitized fallback evidence are
recorded here. Final read-only verification pinned context
`luxuhui-agora-hci-losangeles3s`, shared namespace
`la3-ai-efficiency-prod`, and Helm release `ai-efficiency-staging`: revision 82
was deployed, Backend was 2/2, Worker was 1/1, both used the exact final image,
and liveness/readiness were HTTP 200. A transient Worker refresh error was
followed by the latest successful refresh publishing all four lanes; all four
lane success gauges remained present. The temporary Redis helper, response
bodies, salt, authenticated state, and browser profile were absent. The
sanitized report is retained at
`.superpowers/sdd/stateless-task-7-report.md`. PR #193 remains draft for the
controller-owned Final Review Checklist.

## Final Review Checklist

- [x] Every approved spec requirement maps to a task and a verification command.
- [x] No placeholder, real credential, real identity, or raw response data appears in the plan or final diff.
- [x] `Refresher`, cache, reader, metrics, config, Helm values, and command signatures are consistent across tasks.
- [x] Backend has no worker lifecycle; Worker has no application HTTP/auth/SCM lifecycle.
- [x] Redis schema v3 is read/write isolated from v2 and publication checks one refresh token atomically.
- [x] The final PR removes superseded lifecycle code and execution ledgers instead of layering new machinery over them.
- [x] Backend, race, vet, build, Docker, Helm, stateless restart, fallback, and Chrome acceptance evidence all exist before merge-ready status.

**Final review evidence (2026-07-25):** The original whole-branch review reported
no Critical or Important findings and one Minor spec mismatch in corrupt-cache
request telemetry. A failing reader test first proved that corrupt values were
reported as `fallback`; the remediation now distinguishes corrupt manifest/value
decode or validation errors as `invalid` from Redis GET/MGET transport failures
as `fallback`, while both continue through exact fallback. The focused RED/GREEN
tests, full backend suite, six-package race suite, `go vet ./...`, `go build ./...`,
Dockerfile multi-architecture contract, both Compose renders, Helm
staging script suite, `helm lint ai-efficiency`, deletion constraints,
`git diff --check`, and sanitized-diff hygiene scan all passed. The read-only
follow-up review found no Critical, Important, or Minor issues and confirmed all
seven checklist items.
