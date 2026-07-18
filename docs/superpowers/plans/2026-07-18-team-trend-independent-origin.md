# Team Trend Independent Origin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** In progress on `feat/team-trend-independent-167` from `feat/platform-loading-performance@7c8401d3`. Baseline teamusage/handler/server and focused Team Overview frontend tests pass. The Trend cache/origin/metrics RED contract is confirmed; implementation is next.

**Goal:** Complete issue #167 by giving Team Usage Trend its own bounded authoritative origin, versioned Redis read model, cache telemetry, and failure lifecycle without changing the existing response or frontend chart contracts.

**Architecture:** Add a `TrendSnapshot` lane beside the independent Summary and compatibility Overview lanes in `teamusage.SnapshotCache`. `Service.Trend` will authorize and version the current scope, load only summary stats plus trend points needed for the bounded trend DTO, and cache only window/top-member/department trend fields. A shared pure projection helper keeps compatibility Overview series semantics aligned without letting either endpoint read the other's cache.

**Tech Stack:** Go 1.24, Ent, Gin, Redis/readcache, Prometheus, Vue 3, TypeScript, Vitest, Vite.

## Global Constraints

- Keep the current `GET /api/v1/user/team-usage/trend` response fields and request normalization unchanged.
- Keep current representative authorization, provider configuration version, actor, scope version/hash, range, granularity, and timezone in every Trend cache key.
- Trend cache values contain only `window`, `top_members`, `top_member_trend`, and `department_trend`; no summary, full member page, or organization tree.
- Retain at most 12 top members and 12 department comparisons plus one complete team-total series.
- Preserve stable subject/department identities and `scope_too_large` / `provider_error` unavailable reasons.
- Redis failure falls back to the authoritative Trend origin; eligible stale values never bypass current authorization or survive the hard stale deadline.
- Add the stable production cache name `team_usage_trend` with only `cache` and `outcome` labels and the existing closed outcome set.
- Preserve the compatibility `team_usage_overview` lane until #172; do not compose or remove it in this issue.
- Frontend chart code remains asynchronous and is rendered only when the Trend response exists.
- Use synthetic identities and domains in all tests and examples.
- Update this plan checkbox immediately after each completed step.

---

### Task 1: Define The Trend Snapshot And Cache Isolation Contract

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache_test.go`
- Modify: `backend/internal/teamusage/cache_metrics_test.go`
- Modify: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- Produces `TrendSnapshot`, `TrendOriginLoadResult`, `TrendOriginLoader`, and `TrendCacheResult`.
- Produces `SnapshotCache.GetTrendOrLoad(context.Context, SnapshotCacheKey, TrendOriginLoader)`.
- Extends `SnapshotCacheOptions` with `TrendMetrics readcache.Metrics`.

- [x] **Step 1: Add RED cache tests for the Trend-only value and key space**

  Add focused tests that require a `team-usage-trend` key distinct from `team-usage-summary` and `team-usage-snapshot`, validate cold/fresh/stale/expired behavior, reject malformed or old-schema values, and assert serialized Trend values do not contain `summary`, `members`, or `member_tree`.

- [x] **Step 2: Add RED service isolation tests**

  Require `Service.Trend` to use its own cache lane, make only summary-stats and trend capability calls on a cold miss, cap Top 12/comparison series, avoid satisfying or warming compatibility Overview, and preserve Summary results while Trend is delayed or fails. Keep the 501-subject no-origin `scope_too_large` case.

- [x] **Step 3: Add RED metrics and production-name tests**

  Require a distinct `TrendMetrics` recorder, exactly one `fresh` event per logical warm read, and stable server recorder name `team_usage_trend` with labels exactly `cache` and `outcome`.

- [x] **Step 4: Run focused RED and record the expected failures**

  ```bash
  cd backend
  go test ./internal/teamusage ./cmd/server -run 'Trend.*(Cache|Origin|Isolation|Metrics)|ProductionCacheMetrics' -count=1 -v
  ```

  Expected: compile failures for the missing Trend snapshot/cache interfaces, missing `TrendMetrics`, and absent production recorder.

  RED evidence (2026-07-18): the focused command failed only on missing `TrendSnapshot`, `TrendOriginLoadResult`, `GetTrendOrLoad`, `TrendMetrics`, and the absent `team_usage_trend` production recorder.

### Task 2: Implement The Independent Trend Origin And Cache Lane

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: tests from Task 1

**Interfaces:**
- `readTrendSnapshot(context.Context, int, OverviewParams) (*TrendCacheResult, string, error)` owns Trend authorization/version/cache flow.
- `generateTrendSnapshot(context.Context, *representativescope.Scope, relay.Provider, OverviewParams) (*TrendSnapshot, error)` owns the bounded Trend origin.
- A shared internal projection builds Trend series for both Trend and compatibility Overview without generating other DTO sections.

- [x] **Step 1: Add the Trend cache lane**

  Add schema version 1, key prefix `team-usage-trend`, validation for window/non-nil bounded series, `GetTrendOrLoad`, and the same two-window/lease/fallback policy used by Summary and Overview. Remove the duplicate `fresh` metric record currently present in the generic cache warm-hit path.

- [x] **Step 2: Implement `readTrendSnapshot`**

  Normalize input, require current representative scope, read current provider configuration, resolve the provider only for supported scope sizes, classify hard versus stale-eligible origin failures, and use a key containing provider/actor/scope/range dimensions.

- [x] **Step 3: Implement the bounded Trend origin**

  For scopes at or below the existing cap, resolve Relay identities, load batch summary stats and trend points under the existing timeout, then build at most 12 ranked members, Top 12 series, one team-total series, and at most 12 department comparisons. Return a Trend-only unavailable snapshot for `scope_too_large` without resolving a provider.

- [x] **Step 4: Keep compatibility behavior aligned without cache coupling**

  Reuse the internal Trend projection from `generateOverviewSnapshot`, but keep Overview on `team_usage_overview` and keep its summary/full members/tree composition unchanged until #172.

- [x] **Step 5: Verify focused GREEN**

  ```bash
  cd backend
  gofmt -w internal/teamusage/*.go
  go test ./internal/teamusage -run 'Trend|SummaryOverviewCacheIsolation|CacheMetrics' -count=2
  go test -race ./internal/readcache ./internal/teamusage -run 'Trend|CacheMetrics' -count=1
  git diff --check
  ```

  Expected: Trend cache/origin/isolation tests pass twice and focused race tests report no races.

  GREEN evidence (2026-07-18): focused Trend/cache/metrics tests passed, the broader Trend and Summary/Overview isolation set passed twice, focused `readcache`/`teamusage` race verification passed, and `git diff --check` was clean.

### Task 3: Wire Production Telemetry And Revalidate HTTP/Frontend Boundaries

**Files:**
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`
- Modify: `backend/cmd/server/main.go`
- Verify: `backend/internal/handler/team_usage.go`
- Verify: `backend/internal/handler/team_usage_test.go`
- Verify: `frontend/src/api/teamUsage.ts`
- Verify: `frontend/src/views/TeamOverviewView.vue`
- Verify: `frontend/src/__tests__/team-usage-api.test.ts`
- Verify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Production startup injects `team_usage_trend` into `SnapshotCacheOptions.TrendMetrics`.
- Existing authenticated Trend route and DTO remain source compatible.

- [x] **Step 1: Wire and verify `team_usage_trend` telemetry**

  Bind the stable recorder once in `newProductionCacheMetrics`, expose it through `recorders()`, inject it into the Team Usage cache constructor, and keep the label contract privacy-safe.

- [x] **Step 2: Run focused handler and frontend contract tests**

  ```bash
  cd backend
  go test ./internal/handler ./cmd/server -run 'TeamUsageTrend|ProductionCacheMetrics' -count=2
  cd ../frontend
  npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts
  npm run build
  ```

  Confirm the build still emits `TeamOverviewMemberTrendChart-*.js` separately and that delayed/failed/stale Trend states do not hide Summary or Members.

  Verification evidence (2026-07-18): handler/server tests passed twice, the focused Team Usage API/View suite passed 62/62, and the production build emitted `TeamOverviewMemberTrendChart-DE5PmGTa.js` separately from `TeamOverviewView-NDhRMd6b.js`.

### Task 4: Document, Verify, Review, And Deliver

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: `deploy/observability/README.md`
- Modify: this plan

- [x] **Step 1: Update current architecture and the active contract**

  Record the independent Trend origin/cache/value boundary, stable metric name, compatibility Overview separation, stale/failure behavior, and unchanged frontend async chart lifecycle. Do not rewrite historical Team Usage specs.

  Documentation evidence (2026-07-18): current architecture, the active 2026-07-14 performance contract, and the observability operator README now record the independent Trend lane and `team_usage_trend`; historical Team Usage specs remain unchanged.

- [ ] **Step 2: Run full verification**

  ```bash
  git diff --check
  cd backend
  go test ./... -count=1
  go vet ./...
  go test -race ./internal/readcache ./internal/teamusage ./internal/telemetry -count=1
  cd ../frontend
  npm test
  npm run build
  cd ../ae-cli
  go test ./... -count=1
  cd ..
  bash deploy/test/release-frontend-embed-test.sh
  ```

- [ ] **Step 3: Review exact branch against issue #167 and the active spec**

  Confirm the Trend endpoint never reads the Overview or Summary key, the cached payload contains no unrelated sections, every series bound/identity/unavailable reason is preserved, Summary remains independent in delay/error/stale/expiry scenarios, and all cache labels remain low-cardinality. Fix every finding and rerun affected verification.

- [ ] **Step 4: Deliver to the performance feature branch**

  Push `feat/team-trend-independent-167`, open a PR targeting `feat/platform-loading-performance`, wait for exact-head backend/frontend/ae-cli/deploy-static CI, squash-merge, close #167 with exact evidence, update #173, and record the integration state in this plan.
