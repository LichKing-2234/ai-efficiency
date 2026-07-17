# Independent Team Summary Cold Path Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #164 by making `GET /api/v1/user/team-usage/summary` use a summary-only origin and cache lane that never waits for Team Usage trend, ranking, or organization projection.

**Architecture:** Keep the existing representative-scope and provider-version guards, but give Summary a distinct typed snapshot and Redis key space inside `SnapshotCache`. Extend the existing `relay.TeamUsageSummaryProvider` request/response contract with optional selected-window fields; unsupported providers return useful comparison totals and an explicit range-only unavailable state instead of falling back to trend work.

**Tech Stack:** Go, Gin, Ent, Redis/go-redis, miniredis, Vue 3, TypeScript, Vitest.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-team-summary-independent-164` on `perf/team-summary-independent-164`.
- Current code, issue #164, and `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` are the implementation contract.
- PostgreSQL, the current Directory Sync apply run, and the enabled primary Relay provider configuration remain authoritative.
- Do not modify Sub2API source or introduce direct database coupling to Sub2API.
- Every Summary request revalidates authentication, representative scope, actor role, Directory Sync run, and provider configuration before cache access.
- Summary cache isolation includes deployment namespace, provider ID/configuration version, actor ID, scope version/hash, start date, end date, granularity, and timezone, with a key space distinct from overview/trend.
- Preserve the existing 48-54 second fresh window, 4-4.5 minute hard stale window, cancellation semantics, invalid-credential exclusion, Redis fallback, and token-protected lease behavior.
- Preserve the one-release compatibility overview response and headers; do not remove or redesign the legacy route in this ticket.
- Keep frontend section state independent; do not add a new page, store, or cross-section fallback.
- Tests and examples use only synthetic identities and credentials.
- Update each checkbox immediately after the action is actually complete.

**Status:** In progress. The active performance spec was updated and committed as `88b22871`; implementation and verification remain open below.

---

### Task 1: Add A Typed Summary Cache Lane And Relay Range Contract

**Files:**
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/summary_cache_test.go`

**Interfaces:**
- Extends `relay.TeamUsageSummaryParams` with `StartDate`, `EndDate`, and `Granularity` while retaining `Timezone`.
- Extends `relay.TeamUserUsageStats` with optional `RangeActualCost *float64` and `RangeTotalTokens *int64`; absent fields mean the provider cannot authoritatively supply the selected window.
- Produces `teamusage.SummarySnapshot{Window OverviewWindow; Summary OverviewSummary}`.
- Produces `SnapshotCache.GetSummaryOrLoad(context.Context, SnapshotCacheKey, SummaryOriginLoader) (*SummaryCacheResult, error)` using Redis prefix `ae:<namespace>:team-usage-summary:v1:<digest>`.

- [x] **Step 1: Write RED Relay contract tests**

  Extend `TestSub2APIGetBatchUserUsageStatsPostsUserIDs` to call:

  ```go
  relay.TeamUsageSummaryParams{
      StartDate: "2026-07-01", EndDate: "2026-07-07",
      Granularity: "day", Timezone: "Asia/Shanghai",
  }
  ```

  Assert the JSON body contains all four normalized fields and that response fields `range_actual_cost` and `range_total_tokens` decode without reusing `total_actual_cost` or `total_tokens`.

- [x] **Step 2: Write RED summary-lane cache tests**

  Add focused tests proving:

  ```go
  overviewKey, _ := snapshotCacheKey("test", testSnapshotCacheKey())
  summaryKey, _ := summaryCacheKey("test", testSnapshotCacheKey())
  if overviewKey == summaryKey { t.Fatal("summary and overview cache keys must be isolated") }
  ```

  Cover summary cold miss/warm hit, eligible stale fallback, hard stale rejection, malformed or overview-shaped value rejection, Redis outage authoritative fallback, caller cancellation, and process-local collapse. Assert the cached value contains only `window` and `summary` plus freshness envelope fields.

- [x] **Step 3: Run RED commands**

  Run:

  ```bash
  cd backend
  go test ./internal/relay -run '^TestSub2APIGetBatchUserUsageStatsPostsUserIDs$' -count=1
  go test ./internal/teamusage -run 'SummaryCache|SummaryLane|CacheKey' -count=1
  ```

  Expected: compile/assertion failures for the missing range fields, summary snapshot types, cache key, and `GetSummaryOrLoad` method.

  RED evidence (2026-07-17):

  ```text
  $ cd backend && go test ./internal/relay -run '^TestSub2APIGetBatchUserUsageStatsPostsUserIDs$' -count=1
  internal/relay/sub2api_test.go:3736:3: unknown field StartDate in struct literal of type relay.TeamUsageSummaryParams
  internal/relay/sub2api_test.go:3736:28: unknown field EndDate in struct literal of type relay.TeamUsageSummaryParams
  internal/relay/sub2api_test.go:3737:3: unknown field Granularity in struct literal of type relay.TeamUsageSummaryParams
  internal/relay/sub2api_test.go:3761:15: got[1001].RangeActualCost undefined (type relay.TeamUserUsageStats has no field or method RangeActualCost)
  internal/relay/sub2api_test.go:3762:13: got[1001].RangeTotalTokens undefined (type relay.TeamUserUsageStats has no field or method RangeTotalTokens)
  FAIL github.com/ai-efficiency/backend/internal/relay [build failed]

  $ cd backend && go test ./internal/teamusage -run 'SummaryCache|SummaryLane|CacheKey' -count=1
  internal/teamusage/summary_cache_test.go:337:21: undefined: summaryCacheKey
  internal/teamusage/summary_cache_test.go:356:35: undefined: SummaryOriginLoadResult
  internal/teamusage/summary_cache_test.go:361:22: cache.GetSummaryOrLoad undefined (type *SnapshotCache has no field or method GetSummaryOrLoad)
  internal/teamusage/summary_cache_test.go:660:46: undefined: SummarySnapshot
  FAIL github.com/ai-efficiency/backend/internal/teamusage [build failed]
  ```

- [x] **Step 4: Implement the Relay DTO and typed cache lanes**

  Refactor the existing cache mechanics into an internal generic lane owned by `SnapshotCache`:

  ```go
  type readModelCache[T any] struct {
      store readcache.Store
      options SnapshotCacheOptions
      keyPrefix string
      schemaVersion int
      validate func(T) bool
      flights readcache.FlightGroup[*readModelCacheResult[T]]
  }

  type SnapshotCache struct {
      overview *readModelCache[*OverviewResponse]
      summary  *readModelCache[*SummarySnapshot]
  }
  ```

  Keep public overview methods and key format unchanged. Use the same lease, stale, timeout, and strict envelope validation code for both lanes, and provide thin typed wrappers for overview and summary origin/result types.

- [x] **Step 5: Verify Task 1 GREEN and commit**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/types.go internal/relay/sub2api*.go internal/teamusage/types.go internal/teamusage/summary_cache*.go
  go test ./internal/relay ./internal/teamusage -run 'Sub2APIGetBatchUserUsageStats|SummaryCache|SummaryLane|SnapshotCache' -count=1
  go test -race ./internal/readcache ./internal/teamusage -run 'SummaryCache|SnapshotCache' -count=1
  git diff --check
  ```

  GREEN evidence (2026-07-17, final pre-commit diff):

  ```text
  $ cd backend && gofmt -w internal/relay/types.go internal/relay/sub2api*.go internal/teamusage/types.go internal/teamusage/summary_cache*.go
  exit 0

  $ cd backend && go test ./internal/relay ./internal/teamusage -run 'Sub2APIGetBatchUserUsageStats|SummaryCache|SummaryLane|SnapshotCache' -count=1
  ok  github.com/ai-efficiency/backend/internal/relay 0.325s
  ok  github.com/ai-efficiency/backend/internal/teamusage 0.740s

  $ cd backend && go test -race ./internal/readcache ./internal/teamusage -run 'SummaryCache|SnapshotCache' -count=1
  ok  github.com/ai-efficiency/backend/internal/readcache 1.315s [no tests to run]
  ok  github.com/ai-efficiency/backend/internal/teamusage 1.867s

  $ git diff --check
  exit 0
  ```

  Review-fix GREEN evidence (2026-07-17):

  ```text
  $ cd backend && go test ./internal/teamusage -run '^TestSummaryCacheCollapsesProcessLocalLoads$' -count=20
  ok  github.com/ai-efficiency/backend/internal/teamusage 0.703s

  $ cd backend && go test ./internal/relay ./internal/teamusage -run 'Sub2APIGetBatchUserUsageStats|SummaryCache|SummaryLane|SnapshotCache' -count=1
  ok  github.com/ai-efficiency/backend/internal/relay 0.583s
  ok  github.com/ai-efficiency/backend/internal/teamusage 0.788s

  $ cd backend && go test -race ./internal/readcache ./internal/teamusage -run 'SummaryCache|SnapshotCache' -count=1
  ok  github.com/ai-efficiency/backend/internal/readcache 1.558s [no tests to run]
  ok  github.com/ai-efficiency/backend/internal/teamusage 1.892s

  $ git diff --check
  exit 0
  ```

  Commit: `perf(teamusage): isolate summary cache lane`

### Task 2: Move Summary Onto Summary-Only Origin Work

**Files:**
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/service_test.go`
- Modify: `backend/internal/handler/team_usage_test.go`

**Interfaces:**
- `Service.Summary` calls a new private `readSummarySnapshot` path; `Trend` and `Overview` keep `readOverviewSnapshot` unchanged.
- The summary origin may call `relay.TeamUsageSummaryProvider` but never `relay.TeamMemberTrendProvider`.
- Stable partial reason: `range_aggregation_unavailable`.

- [ ] **Step 1: Write RED service tests for independent work**

  Add tests that use one fake provider implementing both capabilities and assert:

  ```go
  summary, err := svc.Summary(ctx, actorID, params)
  if err != nil { t.Fatal(err) }
  if provider.trendCalls != 0 { t.Fatalf("trend calls = %d, want 0", provider.trendCalls) }
  if provider.summaryParams != wantParams { t.Fatalf("summary params = %#v", provider.summaryParams) }
  ```

  Cover complete range values, missing/incomplete range fields producing null range totals plus `range_aggregation_unavailable`, correct today/historical comparison totals, canonical member and connected-member counts, a delayed trend provider not delaying Summary, a failed trend provider not failing Summary, warm Summary guards plus cache hit, Summary/Overview cache isolation, and Redis outage fallback.

- [ ] **Step 2: Write a RED real-HTTP regression**

  Mount `TeamUsageHandler` with a real `teamusage.Service`, synthetic auth context, miniredis cache, a summary response, and a trend fake blocked on a channel. Issue `GET /api/v1/user/team-usage/summary` and assert HTTP 200 with summary cards' DTO fields before releasing the trend channel; assert `trendCalls == 0`.

- [ ] **Step 3: Run RED commands**

  Run:

  ```bash
  cd backend
  go test ./internal/teamusage -run 'Summary.*Independent|Summary.*Range|Summary.*CacheIsolation' -count=1
  go test ./internal/handler -run 'TeamUsageSummary.*Independent' -count=1
  ```

  Expected: failures because `Summary` still calls `readOverviewSnapshot` and therefore reaches trend work.

- [ ] **Step 4: Implement the summary-only loader**

  Add `readSummarySnapshot` with the same normalization, current scope/provider guard, scope hash, cache key, hard-error classification, and Redis fallback used by overview. Extract and reuse only the narrow helpers needed by both paths:

  ```go
  func (s *Service) resolveOverviewSubjects(ctx context.Context, scope *representativescope.Scope, provider relay.Provider) ([]representativescope.Subject, []int64, error)
  func (s *Service) loadTeamUsageStats(ctx context.Context, provider relay.TeamUsageSummaryProvider, relayUserIDs []int64, params relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error)
  ```

  Aggregate range values only when every connected member has both range fields. Always preserve member counts and comparison totals when the batch stats call succeeds. Do not call or type-assert `TeamMemberTrendProvider` from this path.

- [ ] **Step 5: Verify Task 2 GREEN and commit**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/service*.go internal/handler/team_usage_test.go
  go test ./internal/teamusage ./internal/handler -run 'Summary|Overview.*Snapshot|TeamUsageSummary' -count=1
  go test -race ./internal/teamusage -run 'Summary|SnapshotCache' -count=1
  git diff --check
  ```

  Commit: `perf(teamusage): make summary cold path independent`

### Task 3: Lock Frontend Behavior, Document Current Architecture, And Publish

**Files:**
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` only if implementation clarifies the committed #164 contract
- Modify: `docs/superpowers/plans/2026-07-17-independent-team-summary-cold-path.md`

**Interfaces:**
- No new frontend API or state abstraction.
- The existing Summary section renders counts/comparison data and `teamUsage.summaryUnavailable` while trend, members, or organization are independently pending/failed.

- [ ] **Step 1: Add the focused frontend regression**

  Return a Summary response with `unavailable_reason: 'range_aggregation_unavailable'`, null range fields, and valid member/comparison fields while the trend promise remains unresolved. Assert the Summary section and available values render, the local unavailable message is visible, and the trend loading state remains separate.

- [ ] **Step 2: Run the focused frontend test**

  Run:

  ```bash
  cd frontend
  npm test -- src/__tests__/team-overview-view.test.ts
  ```

  Expected: PASS after the backend-only lifecycle change because the existing UI already owns section-local state; if it fails, make only the minimal view/copy correction required by the committed contract.

- [ ] **Step 3: Update architecture and current plan status**

  Replace the claim that Summary and compatibility reuse one snapshot generation with the independent Summary origin/cache contract, incomplete-range degradation, and unchanged compatibility path. Record completed RED/GREEN commands immediately in this plan and keep unrun full/environment checks unchecked.

- [ ] **Step 4: Run full verification**

  Run:

  ```bash
  cd backend && go test ./... -count=1 && go vet ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  git diff --check
  ```

  Also run focused race tests for `readcache`, `teamusage`, and the new handler selection. Report environment-sensitive role E2E separately; do not mark it complete unless actually run.

- [ ] **Step 5: Review, commit documentation, and publish**

  Run independent Standards and Spec reviews against issue #164 and the active performance spec. Fix every Critical/Important finding and rerun affected tests. Commit the plan/architecture/test evidence with Conventional Commits, push `perf/team-summary-independent-164`, create a non-Draft PR targeting `feat/platform-loading-performance` with `Closes #164`, mark #164 `in-review`, and wait for exact-head backend/frontend/ae-cli/deploy-static CI.
