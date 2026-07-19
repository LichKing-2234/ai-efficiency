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

**Status:** Complete. PR #177 was merged into `feat/platform-loading-performance` at `b8a5450c`; issue #164 is closed, and the exact integration head `d2bc2694` includes the later Team Usage remediations through #172. The ticket-local role E2E gap is superseded by #173's fresh exact-head 16/16 role regression. No `main` merge, release, or production verification is claimed.

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

- [x] **Step 1: Write RED service tests for independent work**

  Add tests that use one fake provider implementing both capabilities and assert:

  ```go
  summary, err := svc.Summary(ctx, actorID, params)
  if err != nil { t.Fatal(err) }
  if provider.trendCalls != 0 { t.Fatalf("trend calls = %d, want 0", provider.trendCalls) }
  if provider.summaryParams != wantParams { t.Fatalf("summary params = %#v", provider.summaryParams) }
  ```

  Cover complete range values, missing/incomplete range fields producing null range totals plus `range_aggregation_unavailable`, correct today/historical comparison totals, canonical member and connected-member counts, a delayed trend provider not delaying Summary, a failed trend provider not failing Summary, warm Summary guards plus cache hit, Summary/Overview cache isolation, and Redis outage fallback.

- [x] **Step 2: Write a RED real-HTTP regression**

  Mount `TeamUsageHandler` with a real `teamusage.Service`, synthetic auth context, miniredis cache, a summary response, and a trend fake blocked on a channel. Issue `GET /api/v1/user/team-usage/summary` and assert HTTP 200 with summary cards' DTO fields before releasing the trend channel; assert `trendCalls == 0`.

- [x] **Step 3: Run RED commands**

  Run:

  ```bash
  cd backend
  go test ./internal/teamusage -run 'Summary.*Independent|Summary.*Range|Summary.*CacheIsolation' -count=1
  go test ./internal/handler -run 'TeamUsageSummary.*Independent' -count=1
  ```

  Expected: failures because `Summary` still calls `readOverviewSnapshot` and therefore reaches trend work.

  RED evidence (2026-07-17):

  ```text
  $ cd backend && go test ./internal/teamusage -run 'Summary.*Independent|Summary.*Range|Summary.*CacheIsolation' -count=1
  --- FAIL: TestSummaryRangeIndependentFromTrendAndPreservesComparisonTotals (0.31s)
      service_test.go:465: summary range_actual_cost = (*float64)(0x1400000f0a0), want selected-window 45
  --- FAIL: TestSummaryRangeUnavailableWhenProviderFieldsIncomplete (0.52s)
      --- FAIL: TestSummaryRangeUnavailableWhenProviderFieldsIncomplete/range_fields_missing (0.27s)
          service_test.go:529: summary unavailable = false/(*string)(nil), want range_aggregation_unavailable
      --- FAIL: TestSummaryRangeUnavailableWhenProviderFieldsIncomplete/one_member_incomplete (0.25s)
          service_test.go:529: summary unavailable = false/(*string)(nil), want range_aggregation_unavailable
  --- FAIL: TestSummaryIndependentFromDelayedTrendProvider (0.30s)
      service_test.go:573: Summary reached delayed trend provider
  --- FAIL: TestSummaryIndependentFromFailedTrendProvider (0.23s)
      service_test.go:595: summary = {Unavailable:true UnavailableReason:0x140001c8ae0 MemberCount:2 RelayMemberCount:2 RangeActualCost:<nil> RangeTotalTokens:<nil> TodayActualCost:0x140002a6950 TotalActualCost:0x140002a6958 UnitLabel:USD}, want complete range despite failed trend capability
  --- FAIL: TestSummaryOverviewCacheIsolation (0.22s)
      service_test.go:658: summary range cost = (*float64)(0x14000356a28), want summary-batch 45
  --- FAIL: TestSummaryIndependentRedisOutageFallsBackAuthoritatively (0.22s)
      service_test.go:681: Summary() call 1 = &{SnapshotFreshness:{AsOf:2026-07-17 13:00:28.430628 +0000 UTC FreshUntil:2026-07-17 13:01:22.430628 +0000 UTC StaleUntil:2026-07-17 13:04:58.430628 +0000 UTC CacheStatus:miss SourceStatus:ok} ScopeVersion:scope-version-summary RequestID: Window:{StartDate:2026-07-01 EndDate:2026-07-07 Granularity:day Today:2026-07-17 RollingDays:7 Timezone:Asia/Shanghai} Summary:{Unavailable:false UnavailableReason:<nil> MemberCount:2 RelayMemberCount:2 RangeActualCost:0x1400019b210 RangeTotalTokens:<nil> TodayActualCost:0x1400019b260 TotalActualCost:0x1400019b268 UnitLabel:USD}}, want authoritative miss with range 45
  FAIL
  FAIL github.com/ai-efficiency/backend/internal/teamusage 2.723s

  $ cd backend && go test ./internal/handler -run 'TeamUsageSummary.*Independent' -count=1
  --- FAIL: TestTeamUsageSummaryIndependentFromTrendOverRealHTTP (0.30s)
      team_usage_test.go:379: summary HTTP request reached trend provider
  FAIL
  FAIL github.com/ai-efficiency/backend/internal/handler 1.042s
  ```

- [x] **Step 4: Implement the summary-only loader**

  Add `readSummarySnapshot` with the same normalization, current scope/provider guard, scope hash, cache key, hard-error classification, and Redis fallback used by overview. Extract and reuse only the narrow helpers needed by both paths:

  ```go
  func (s *Service) resolveOverviewSubjects(ctx context.Context, scope *representativescope.Scope, provider relay.Provider) ([]representativescope.Subject, []int64, error)
  func (s *Service) loadTeamUsageStats(ctx context.Context, provider relay.TeamUsageSummaryProvider, relayUserIDs []int64, params relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error)
  ```

  Aggregate range values only when every connected member has both range fields. Always preserve member counts and comparison totals when the batch stats call succeeds. Do not call or type-assert `TeamMemberTrendProvider` from this path.

- [x] **Step 5: Verify Task 2 GREEN and commit**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/service*.go internal/handler/team_usage_test.go
  go test ./internal/teamusage ./internal/handler -run 'Summary|Overview.*Snapshot|TeamUsageSummary' -count=1
  go test -race ./internal/teamusage -run 'Summary|SnapshotCache' -count=1
  git diff --check
  ```

  Review-fix RED evidence (2026-07-17):

  ```text
  $ cd backend && go test ./internal/teamusage -run '^TestSummaryRangeDeduplicatesSharedRelayBindings$' -count=1
  --- FAIL: TestSummaryRangeDeduplicatesSharedRelayBindings (0.24s)
      service_test.go:578: summary range totals = (*float64)(0x1400000f000)/(*int64)(0x1400000f008), want deduplicated 15/1500
  FAIL
  FAIL github.com/ai-efficiency/backend/internal/teamusage 0.911s

  $ cd backend && go test ./internal/handler -run '^TestTeamUsageSummaryIndependentFromTrendOverRealHTTP$' -count=1
  --- FAIL: TestTeamUsageSummaryIndependentFromTrendOverRealHTTP (0.25s)
      team_usage_test.go:417: incomplete range body = {"code":200,"data":{"as_of":"2026-07-17T13:11:01.96503Z","fresh_until":"2026-07-17T13:11:54.658668104Z","stale_until":"2026-07-17T13:15:25.43322052Z","cache_status":"miss","source_status":"ok","scope_version":"scope-http-summary","request_id":"","window":{"start_date":"2026-07-01","end_date":"2026-07-08","granularity":"day","today":"2026-07-17","rolling_days":8,"timezone":"Asia/Shanghai"},"summary":{"unavailable":true,"unavailable_reason":"range_aggregation_unavailable","member_count":2,"relay_member_count":2,"range_actual_cost":null,"today_actual_cost":3,"total_actual_cost":109,"unit_label":"USD"}}}, want "range_total_tokens":null
  FAIL
  FAIL github.com/ai-efficiency/backend/internal/handler 1.016s
  ```

  GREEN evidence (2026-07-17, final pre-commit diff):

  ```text
  $ cd backend && gofmt -w internal/teamusage/service*.go internal/teamusage/types.go internal/handler/team_usage_test.go
  exit 0

  $ cd backend && go test ./internal/teamusage ./internal/handler -run 'Summary|Overview.*Snapshot|TeamUsageSummary' -count=1
  ok  github.com/ai-efficiency/backend/internal/teamusage 4.147s
  ok  github.com/ai-efficiency/backend/internal/handler 3.502s

  $ cd backend && go test -race ./internal/teamusage -run 'Summary|SnapshotCache' -count=1
  ok  github.com/ai-efficiency/backend/internal/teamusage 5.123s

  $ git diff --check
  exit 0
  ```

  Review-fix GREEN evidence (2026-07-17):

  ```text
  $ cd backend && go test ./internal/teamusage -run '^TestSummaryRangeDeduplicatesSharedRelayBindings$' -count=1
  ok  github.com/ai-efficiency/backend/internal/teamusage 0.911s

  $ cd backend && go test ./internal/handler -run '^TestTeamUsageSummaryIndependentFromTrendOverRealHTTP$' -count=1
  ok  github.com/ai-efficiency/backend/internal/handler 1.043s

  $ cd backend && go test ./internal/teamusage -count=1
  ok  github.com/ai-efficiency/backend/internal/teamusage 11.589s

  $ cd backend && go test ./internal/handler -run '^TestTeamUsageSummaryIndependentFromTrendOverRealHTTP$' -count=20
  ok  github.com/ai-efficiency/backend/internal/handler 5.373s
  ```

  Self-review: initial review found duplicate Relay bindings could double-count selected-window totals and nil `range_total_tokens` was omitted on the wire. Both Important findings were fixed with RED/GREEN regressions. Re-review: PASS, no remaining Critical/Important findings.

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

- [x] **Step 1: Add the focused frontend regression**

  Return a Summary response with `unavailable_reason: 'range_aggregation_unavailable'`, null range fields, and valid member/comparison fields while the trend promise remains unresolved. Assert the Summary section and available values render, the local unavailable message is visible, and the trend loading state remains separate.

- [x] **Step 2: Run the focused frontend test**

  Run:

  ```bash
  cd frontend
  npm test -- src/__tests__/team-overview-view.test.ts
  ```

  Expected: PASS after the backend-only lifecycle change because the existing UI already owns section-local state; if it fails, make only the minimal view/copy correction required by the committed contract.

  Evidence (2026-07-17):

  ```text
  $ cd frontend && npm test -- src/__tests__/team-overview-view.test.ts
  Test Files  1 passed (1)
  Tests       51 passed (51)
  ```

- [x] **Step 3: Update architecture and current plan status**

  Replace the claim that Summary and compatibility reuse one snapshot generation with the independent Summary origin/cache contract, incomplete-range degradation, and unchanged compatibility path. Record completed RED/GREEN commands immediately in this plan and keep unrun full/environment checks unchecked.

- [x] **Step 4: Run full verification**

  Run:

  ```bash
  cd backend && go test ./... -count=1 && go vet ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  git diff --check
  ```

  Also run focused race tests for `readcache`, `teamusage`, and the new handler selection. Report environment-sensitive role E2E separately; do not mark it complete unless actually run.

  Evidence (2026-07-17):

  ```text
  $ cd backend && go test ./... -count=1 && go vet ./...
  PASS (all backend packages); go vet exit 0

  $ cd frontend && npm test && npm run build
  Test Files  47 passed (47)
  Tests       673 passed (673)
  vite: 204 modules transformed, built in 2.31s

  $ cd ae-cli && go test ./... -count=1
  PASS (all ae-cli packages)

  $ cd backend && go test -race ./internal/readcache ./internal/teamusage ./internal/handler -run 'Summary|SnapshotCache|TeamUsageSummary' -count=1
  ok  github.com/ai-efficiency/backend/internal/readcache
  ok  github.com/ai-efficiency/backend/internal/teamusage
  ok  github.com/ai-efficiency/backend/internal/handler

  $ bash deploy/test/release-frontend-embed-test.sh
  PASS: TestHasEmbeddedFrontendForReleaseBuilds
  PASS: TestEmbeddedFrontendReleaseBuildHTTPPolicy

  $ git diff --check
  exit 0
  ```

  Environment-sensitive role E2E was not run for this ticket.

- [x] **Step 5: Review, commit documentation, and publish**

  Run independent Standards and Spec reviews against issue #164 and the active performance spec. Fix every Critical/Important finding and rerun affected tests. Commit the plan/architecture/test evidence with Conventional Commits, push `perf/team-summary-independent-164`, create a non-Draft PR targeting `feat/platform-loading-performance` with `Closes #164`, mark #164 `in-review`, and wait for exact-head backend/frontend/ae-cli/deploy-static CI.

  Review evidence (2026-07-17):

  - Initial Standards and Spec reviews found one Important compatibility issue: removing `omitempty` from the shared `OverviewSummary.RangeTotalTokens` changed the legacy overview wire shape from an absent field to `null`.
  - RED: `go test ./internal/handler -run '^TestTeamOverviewOmitsUnavailableRangeTokenTotalForCompatibility$' -count=1` failed because the compatibility response contained `"range_total_tokens":null`.
  - Fix: restore the legacy `OverviewSummary` tag and use a Summary-only `SummaryAggregate` DTO whose range token field remains explicitly nullable.
  - GREEN: the compatibility regression, Summary HTTP regressions, focused Team Usage tests, focused race tests, final backend suite, `go vet`, and `git diff --check` pass.
  - Standards re-review: PASS, no Critical/Important findings; the split DTO is a necessary wire-contract boundary rather than speculative generality.
  - Spec re-review: PASS, no Critical/Important findings.
  - PR #177 is non-Draft and targets `feat/platform-loading-performance`; issue #164 is labeled `in-review`.
  - Exact implementation head `6ef5658e` CI: ae-cli PASS (26s), backend PASS (8m43s), deploy-static PASS (16s), frontend PASS (1m11s).
