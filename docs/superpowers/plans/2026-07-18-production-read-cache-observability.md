# Production Read Cache Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** In progress on `feat/read-cache-metrics-166` from `feat/platform-loading-performance@8db50f91`. The clean backend baseline passed on 2026-07-18. No implementation or focused RED/GREEN evidence has been recorded yet.

**Goal:** Complete issue #166 by emitting privacy-safe, low-cardinality outcomes for every production Redis read model and proving the dashboard receives more than work-item cache data.

**Architecture:** Keep cache decisions and fallback policy inside each owning domain module. Reuse one minimal `readcache.Metrics` interface (`Record(outcome string)`) at constructor boundaries, bind stable cache names in `cmd/server`, and record only the closed telemetry outcome set at the existing state-machine decisions. Team Summary and the compatibility Overview receive separate observers because they are distinct Redis read models.

**Tech Stack:** Go 1.24, `backend/internal/readcache`, Prometheus client, miniredis, existing package fakes, Grafana JSON contract tests.

## Global Constraints

- Metrics labels are exactly `cache` and `outcome`; never include cache keys, actors, scopes, provider IDs, date ranges, credentials, tokens, revisions, or cached values.
- Supported outcomes remain `fresh`, `miss`, `stale`, `error`, `refresh`, `lease_acquired`, `lease_wait`, and `lease_failed`.
- Redis failures must preserve each domain's existing authoritative fallback and authorization/freshness behavior.
- Fresh-only caches do not fabricate `stale`; the Prometheus recorder still preinitializes the closed outcome set.
- The implementation covers `personal_usage`, `team_usage_summary`, `team_usage_overview`, `representative_scope`, `repository_inventory`, `work_items_counts`, and `provider_metadata`.
- Historical specs remain unchanged; update the active 2026-07-14 spec only if the effective contract changes, and update `docs/architecture.md` for current runtime state.

---

### Task 1: Shared Metrics Contract And Domain RED Tests

**Files:**
- Create: `backend/internal/readcache/metrics.go`
- Create: `backend/internal/personalusage/cache_metrics_test.go`
- Create: `backend/internal/teamusage/cache_metrics_test.go`
- Create: `backend/internal/representativescope/cache_metrics_test.go`
- Create: `backend/internal/repo/inventory_cache_metrics_test.go`
- Create: `backend/internal/relayruntime/metadata_metrics_test.go`
- Modify: `backend/internal/workitems/cache.go`

**Interfaces:**
- Produces `readcache.Metrics` with `Record(outcome string)`.
- Every domain option struct accepts one or more `readcache.Metrics` values without importing `telemetry`.
- Team Usage options expose distinct observers for Summary and Overview.

- [ ] **Step 1: Add focused tests that express the constructor contracts**

  Add recording metrics fakes and compile-time constructor usage for each domain. Tests must assert cold `miss`/`refresh`/`lease_acquired`, warm `fresh`, distributed `lease_wait`, Redis or release `error`/`lease_failed`, and eligible personal/team `stale` behavior. Assert observer snapshots contain only outcome strings.

- [ ] **Step 2: Run focused tests and record RED**

  ```bash
  cd backend
  go test ./internal/personalusage ./internal/teamusage ./internal/representativescope ./internal/repo ./internal/relayruntime -run 'CacheMetrics|MetadataMetrics' -count=1 -v
  ```

  Expected: compile failures because the domain option structs do not expose metrics observers.

- [ ] **Step 3: Add the shared interface and adapt the existing work-item alias**

  ```go
  package readcache

  type Metrics interface {
      Record(outcome string)
  }
  ```

  Keep `workitems.CountsCacheMetrics` as an alias so existing callers and tests retain source compatibility.

- [ ] **Step 4: Commit the RED contract checkpoint**

  ```bash
  git add backend/internal/readcache/metrics.go backend/internal/*/*metrics_test.go backend/internal/workitems/cache.go
  git commit -m "test(observability): define production cache metric contracts"
  ```

### Task 2: Instrument Personal, Team, Scope, And Repository Read Models

**Files:**
- Modify: `backend/internal/personalusage/types.go`
- Modify: `backend/internal/personalusage/cache.go`
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/representativescope/cache.go`
- Modify: `backend/internal/repo/inventory_cache.go`
- Modify: the focused test files from Task 1

**Interfaces:**
- `personalusage.CacheOptions.Metrics readcache.Metrics`
- `teamusage.SnapshotCacheOptions.SummaryMetrics readcache.Metrics`
- `teamusage.SnapshotCacheOptions.OverviewMetrics readcache.Metrics`
- `representativescope.CacheOptions.Metrics readcache.Metrics`
- `repo.InventoryCacheOptions.Metrics readcache.Metrics`

- [ ] **Step 1: Record outcomes only at existing state decisions**

  Add nil-safe `record` helpers. Record valid decoded hits as `fresh`, misses/malformed envelopes as `miss`, authoritative calls as `refresh`, eligible stale fallback as `stale`, and Redis/loader/write/release failures as `error`. Record lease acquisition, observed contention, and acquire/TTL/release failures with the closed lease outcomes.

- [ ] **Step 2: Verify focused GREEN**

  ```bash
  cd backend
  gofmt -w internal/readcache/metrics.go internal/personalusage/*.go internal/teamusage/*.go internal/representativescope/*.go internal/repo/*.go
  go test ./internal/readcache ./internal/personalusage ./internal/teamusage ./internal/representativescope ./internal/repo -run 'Cache|Metrics|Snapshot|Inventory' -count=2
  go test -race ./internal/personalusage ./internal/teamusage ./internal/representativescope ./internal/repo -run 'CacheMetrics' -count=1
  ```

  Expected: all focused tests pass and existing cache result/fallback assertions remain unchanged.

- [ ] **Step 3: Commit domain instrumentation**

  ```bash
  git add backend/internal/readcache backend/internal/personalusage backend/internal/teamusage backend/internal/representativescope backend/internal/repo
  git commit -m "feat(observability): instrument production read caches"
  ```

### Task 3: Instrument Provider Metadata And Production Wiring

**Files:**
- Modify: `backend/internal/relayruntime/manager.go`
- Modify: `backend/internal/relayruntime/metadata.go`
- Modify: `backend/internal/relayruntime/metadata_metrics_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go` or the existing server contract test nearest cache wiring

**Interfaces:**
- `relayruntime.Options.MetadataMetrics readcache.Metrics`
- `initializeRepoInventory(..., metrics readcache.Metrics)` passes the repository recorder into its cache.

- [ ] **Step 1: Add a server wiring RED test**

  Assert startup binds the seven stable names and that Team Summary and Overview receive different recorders. Keep the assertion at the constructor/wiring source boundary rather than starting a real server.

- [ ] **Step 2: Record provider metadata outcomes and wire stable names**

  Provider metadata records the same fresh/miss/refresh/lease/error lifecycle without caching membership or introducing identity labels. In `main.go`, bind all stable recorder names once and inject them through domain options.

- [ ] **Step 3: Verify focused GREEN**

  ```bash
  cd backend
  gofmt -w internal/relayruntime/*.go cmd/server/*.go
  go test ./internal/relayruntime ./internal/telemetry ./cmd/server -run 'Metadata|Cache|Metrics' -count=2
  go test -race ./internal/relayruntime -run 'MetadataMetrics' -count=1
  ```

- [ ] **Step 4: Commit provider and startup wiring**

  ```bash
  git add backend/internal/relayruntime backend/cmd/server
  git commit -m "feat(observability): wire all read cache metrics"
  ```

### Task 4: Dashboard, Architecture, Full Verification, And Delivery

**Files:**
- Modify: `backend/internal/telemetry/dashboard_contract_test.go`
- Modify: `deploy/observability/grafana/ai-efficiency-performance.json`
- Modify: `deploy/observability/README.md`
- Modify: `docs/architecture.md`
- Modify: this plan

**Interfaces:**
- Grafana exposes application cache rates grouped by stable cache and outcome and demonstrates the complete production name set.

- [ ] **Step 1: Add RED dashboard/name coverage**

  Require the dashboard or its documented variables to identify every stable production cache name while retaining generic `cache`/`outcome` queries and prohibited-label checks.

- [ ] **Step 2: Update the dashboard, operator documentation, and architecture**

  Record the complete cache name set, per-domain lifecycle coverage, privacy boundary, Redis fallback behavior, and that Summary/Overview are distinct current read models. Do not claim #167/#168 independent read models in this ticket.

- [ ] **Step 3: Run full verification**

  ```bash
  git diff --check
  cd backend
  go test ./... -count=1
  go vet ./...
  go test -race ./internal/readcache ./internal/personalusage ./internal/teamusage ./internal/representativescope ./internal/repo ./internal/relayruntime ./internal/telemetry -count=1
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  ```

- [ ] **Step 4: Review the exact branch and deliver**

  Verify every #166 acceptance criterion, commit the plan/documentation, push the branch, open a PR targeting `feat/platform-loading-performance`, wait for all CI checks, merge, close #166 with exact-head evidence, and update #173 dependency state.

  ```bash
  git add docs deploy backend/internal/telemetry
  git commit -m "docs(architecture): document read cache observability"
  ```
