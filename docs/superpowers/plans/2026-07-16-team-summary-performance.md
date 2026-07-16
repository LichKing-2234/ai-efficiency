# Team Summary Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #125 by adding an independently authorized, safely cached Team Overview summary while keeping the historical overview route as a one-release adapter over the same snapshot service.

**Architecture:** `representativescope.Scope` exposes an opaque version derived from its authoritative actor/role/directory guard. `teamusage.Service` normalizes the requested window, resolves the current provider configuration, hashes the effective scope, and reads one reconstructible overview snapshot through a bounded Redis stale-if-error cache; the split summary projects that snapshot while the legacy overview returns its historical shape. The frontend starts summary and compatibility-section requests independently so a delayed or failed overview cannot remove successful summary cards.

**Tech Stack:** Go, Ent, Gin, Redis/go-redis, miniredis, Vue 3, TypeScript, Vitest.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-team-summary-125` on `perf/team-summary-125`.
- PostgreSQL, Directory Sync, and the current Relay provider configuration remain authoritative; Redis is an optional read optimization.
- Cache only reconstructible team usage read models. Never cache JWT/token revocation state, credentials, quota decisions, mutation authorization, or rate-multiplier writes.
- Every request revalidates authentication, representative scope, actor role, Directory Sync run, and provider configuration before using a cached snapshot.
- Cache isolation includes deployment namespace, provider ID/configuration version, actor ID, opaque scope version, deterministic effective scope hash, start date, end date, granularity, and timezone.
- Fresh lifetime is 30-60 seconds with 10-20 percent jitter; stale-if-error never exceeds five minutes from generation.
- Caller cancellation, authorization failure, provider configuration failure, invalid credentials, and invalid input never use stale data.
- The old overview response retains its complete historical JSON shape and accepted query parameters for one compatibility release.
- Do not change Sub2API source, merge, release, tag, deploy, or run Helm.
- Tests and examples use only synthetic identities and credentials.
- Update each checkbox immediately after its action is actually complete.

**Status:** In progress. Dependency branches `perf/personal-usage-123` and `perf/team-scope-124` are merged in commit `6c48cf0`; implementation and verification remain.

---

### Task 1: Version Representative Scope And Define The Shared Snapshot Contract

**Files:**
- Modify: `backend/internal/representativescope/service.go`
- Modify: `backend/internal/representativescope/cache.go`
- Modify: `backend/internal/representativescope/service_test.go`
- Modify: `backend/internal/representativescope/cache_test.go`
- Modify: `backend/internal/teamusage/types.go`
- Create: `backend/internal/teamusage/summary_cache_test.go`

**Interfaces:**
- Produces `representativescope.Scope.Version string`, derived from actor ID, actor role, directory source ID, directory run ID, and scope schema version.
- Produces `teamusage.SnapshotCacheKey{ProviderID int; ProviderVersion int64; ActorID int; ScopeVersion, ScopeHash string; Params OverviewParams}`.
- Produces `teamusage.SnapshotFreshness{AsOf, FreshUntil, StaleUntil time.Time; CacheStatus, SourceStatus string}`.
- Produces `teamusage.SummaryResponse` with top-level freshness metadata, `scope_version`, request-local `request_id`, `window`, and `summary`.

- [x] **Step 1: Add RED scope-version tests**

  Assert the same authoritative guard returns the same non-empty opaque version, while actor, role, source, or applied run changes return a different version; cached and authoritative scopes must expose the same version.

- [x] **Step 2: Run the focused representative-scope tests and record RED**

  Run: `cd backend && go test ./internal/representativescope -run 'Version|Cache' -count=1`

  Expected: compile or assertion failure because `Scope.Version` is absent.

  RED evidence (2026-07-16): the focused command failed only on the intended missing `scopeVersion` function and `Scope.Version` field.

- [x] **Step 3: Implement the opaque scope version**

  Hash canonical JSON containing `schema_version`, `actor_user_id`, `actor_role`, `directory_source_id`, and `directory_run_id` with SHA-256. Assign the version after every successful authoritative or cached resolution, and require an exact version in cache-envelope validation.

  GREEN evidence (2026-07-16): focused version/cache tests passed; the Redis envelope and key are now v2, and old unversioned values fail validation.

- [x] **Step 4: Add RED cache contract tests**

  Cover cold miss, warm hit, fresh-window jitter endpoints, eligible stale fallback, hard stale expiry, malformed/schema-mismatched values, actor/provider/scope/range/granularity/timezone isolation, deterministic effective-scope hashing, 50-call local collapse, two-instance Redis lease collapse, caller cancellation, invalid-credential rejection, Redis outage authoritative fallback, and current request metadata not being serialized.

- [x] **Step 5: Run the cache tests and record RED**

  Run: `cd backend && go test ./internal/teamusage -run 'SnapshotCache|ScopeHash|SummaryResponse' -count=1 -v`

  Expected: compile failure because the snapshot cache contracts do not exist.

  RED evidence (2026-07-16): the focused command failed only on the intended missing snapshot key/cache/result contracts.

### Task 2: Implement The Shared Team Usage Snapshot And Summary Projection

**Files:**
- Create: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/service_test.go`
- Modify: `backend/internal/teamusage/types.go`

**Interfaces:**
- Consumes Task 1 cache key, freshness, and scope version contracts.
- Produces `teamusage.NewSnapshotCache(readcache.Store, SnapshotCacheOptions) (*SnapshotCache, error)`.
- Produces `Service.Summary(context.Context, int, OverviewParams) (*SummaryResponse, error)`.
- Preserves `Service.Overview(context.Context, int, OverviewParams) (*OverviewResponse, error)` as a projection of the same snapshot load.

- [x] **Step 1: Implement the strict snapshot cache envelope**

  Store only schema version, generation/fresh/stale timestamps, and `OverviewResponse`. Use a SHA-256 key under `ae:<namespace>:team-usage-snapshot:v1:<digest>`, 100ms Redis commands, a 25-second shared refresh budget, a 30-second lease, 25ms polling, and token-protected lease release.

  GREEN evidence (2026-07-16): focused cache tests cover all key dimensions, deterministic scope hashing, jitter endpoints, stale hard deadline, hard-error exclusion, malformed values, 50-call local collapse, two-instance lease collapse, and Redis outage fallback.

- [x] **Step 2: Add RED service tests for shared loading and summary semantics**

  Assert summary and overview reuse one generated snapshot, selected-range totals come from trend points, today/historical comparison totals come from batch stats, the summary preserves explicit unavailable state, scope over the cap is never silently truncated, current scope/provider guards run on warm hits, and hard failures do not use stale data.

- [x] **Step 3: Run service tests and record RED**

  Run: `cd backend && go test ./internal/teamusage -run 'Summary|Overview.*Snapshot|ComparisonTotals' -count=1 -v`

  Expected: failures because `Service.Summary` and the shared snapshot path are absent.

  RED evidence (2026-07-16): the focused command failed only on the intended missing shared-cache constructor, `Summary` method, and invalid-window sentinel.

- [x] **Step 4: Refactor the monolithic overview calculation into one snapshot loader**

  Normalize and validate dates, granularity, and timezone once. Resolve the current representative scope and enabled primary provider row before cache access, calculate a canonical scope hash, use batch stats only for today/historical comparison totals, and continue deriving selected-window totals from the requested trend points.

  GREEN evidence (2026-07-16): summary and legacy overview reuse one snapshot generation, warm hits still resolve scope/provider guards, and selected-window versus comparison-total tests pass.

- [x] **Step 5: Verify Task 2 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/representativescope/*.go internal/teamusage/*.go
  go test ./internal/representativescope ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/teamusage -count=1
  git diff --check
  ```

  Commit: `perf(backend): cache shared team usage snapshots`

  GREEN evidence (2026-07-16): representative-scope and teamusage suites passed twice; race-enabled readcache/teamusage tests passed; `git diff --check` reported no findings.

### Task 3: Add The Summary Route And Legacy Compatibility Adapter

**Files:**
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/router_work_items_cache_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`

**Interfaces:**
- Produces authenticated `GET /api/v1/user/team-usage/summary`.
- Extends `handler.RouterRuntimeOptions` with `TeamUsageSnapshotCache *teamusage.SnapshotCache`.
- Adds legacy headers `Deprecation: @1783987200`, `Sunset: Tue, 15 Sep 2026 00:00:00 GMT`, and `Link: </api/v1/user/team-usage/summary>; rel="successor-version"`.

- [x] **Step 1: Add RED HTTP tests**

  Assert query forwarding, top-level freshness/scope/request metadata, a new request ID on every response, auth protection, generic 403 no-scope behavior, 400 invalid-window behavior, bounded Redis fallback, and all three compatibility headers on both successful and failed legacy responses.

- [x] **Step 2: Run focused handler/runtime tests and record RED**

  Run: `cd backend && go test ./internal/handler ./cmd/server -run 'TeamUsageSummary|TeamOverviewDeprecation|TeamUsageSnapshotCache' -count=1`

  Expected: compile or route-not-found failure.

  RED evidence (2026-07-16): focused handler tests failed only because `TeamUsageHandler.Summary` did not exist.

- [x] **Step 3: Implement route, request metadata, headers, and runtime injection**

  Keep request ID generation in the HTTP layer so it is never cached. Construct one snapshot cache from the existing shared bounded Redis store and namespace, inject it into `teamusage.Service`, and leave the authenticated middleware/token-revocation path unchanged.

  GREEN evidence (2026-07-16): focused handler tests pass for metadata, unique request IDs, auth, scoped/input failures, and compatibility headers; server compilation includes the shared Redis cache construction.

- [x] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/handler/team_usage*.go internal/handler/router*.go cmd/server/*.go
  go test ./internal/teamusage ./internal/handler ./cmd/server -count=2
  git diff --check
  ```

  Commit: `feat(backend): expose split team usage summary`

  GREEN evidence (2026-07-16): teamusage, handler, and server package suites passed twice; `git diff --check` reported no findings.

### Task 4: Render Summary Independently In The Frontend

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/teamUsage.ts`
- Modify: `frontend/src/views/TeamOverviewView.vue`
- Modify: `frontend/src/__tests__/team-usage-api.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Produces `getTeamUsageSummary(params)` using the standard 15-second client timeout.
- Produces `TeamUsageSummaryResponse` matching the split endpoint.
- Keeps trend/member/organization content on the compatibility overview only for issue #125, with section-local loading/error state.

- [x] **Step 1: Add RED API and view tests**

  Assert both requests start with the same normalized range, summary cards render while overview is still pending, overview failure leaves summary visible with a local sections error, summary failure does not hide a successful legacy section, a summary 403 shows the no-scope state, stale freshness is visible, and superseded range results cannot overwrite the current selection.

- [x] **Step 2: Run focused frontend tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts`

  Expected: failures because `getTeamUsageSummary` and independent state do not exist.

  RED evidence (2026-07-16): five focused failures cover the missing summary API, independent early render, both section-local failure directions, and stale marker.

- [x] **Step 3: Implement independent summary and legacy section lifecycles**

  Keep separate request sequence counters, data refs, loading refs, and error refs. Render summary cards from `TeamUsageSummaryResponse.summary` only; render the trend/member tree from the legacy response only; disable a range button while either current request is pending without clearing successful prior section data.

  GREEN evidence (2026-07-16): focused tests prove early summary render, both failure directions, independent stale marker, shared normalized params, and preserved existing Team Overview interactions.

- [x] **Step 4: Verify Task 4 GREEN and checkpoint**

  Run:

  ```bash
  cd frontend
  npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts
  npm test
  npm run build
  ```

  Commit: `perf(frontend): render team summary independently`

  GREEN evidence (2026-07-16): focused API/view tests passed 37/37; full frontend suite passed 39 files and 462 tests; `vue-tsc` and Vite production build passed.

### Task 5: Document, Verify, Review, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` only if implementation clarifies the active contract
- Modify: this plan

- [x] **Step 1: Update current architecture and plan status**

  Document authoritative per-request guards, scope/provider/range cache isolation, selected-window versus comparison-total semantics, stale eligibility, Redis fallback, frontend section independence, and the one-release legacy adapter.

  Documentation evidence (2026-07-16): `docs/architecture.md` now records the split lifecycle, shared snapshot cache contract, opaque scope version, comparison-total semantics, compatibility headers, and authoritative fallback boundaries.

- [x] **Step 2: Run formatting and static checks**

  Run:

  ```bash
  git diff --check
  cd backend && go vet ./internal/readcache ./internal/representativescope ./internal/teamusage ./internal/handler ./cmd/server
  ```

  GREEN evidence (2026-07-16): `git diff --check` and changed-package `go vet` passed.

- [x] **Step 3: Run full repository verification**

  Run:

  ```bash
  cd backend && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

  GREEN evidence (2026-07-16): backend `go test ./...`, frontend 39 files/462 tests plus production build, and ae-cli `go test ./...` all passed. Race-enabled readcache/representativescope/teamusage tests also passed.

- [x] **Step 4: Review the complete diff against issue #125 and the active performance spec**

  Check every acceptance criterion, cache-key dimension, stale exclusion, legacy response field, deprecation header, and independent frontend state. Resolve every finding and rerun affected verification.

  Review result (2026-07-16): no unresolved findings. The review caught and fixed unnecessary provider-origin resolution on warm hits and for explicit `scope_too_large` generations; focused regressions and the full verification suite passed afterward.

- [ ] **Step 5: Commit documentation, push, and open a Draft PR**

  Use Conventional Commits, target `perf/team-scope-124`, state that the branch includes the explicit #123 dependency merge, and list Draft PRs #148 and #149 as dependencies. Do not merge or release.

- [ ] **Step 6: Wait for required PR checks and record final state**

  Record the exact GitHub Actions run and backend/frontend/ae-cli/deploy-static results in this plan.
