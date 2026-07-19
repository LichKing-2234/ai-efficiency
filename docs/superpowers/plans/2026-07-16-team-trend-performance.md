# Team Trend Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #126 by moving Team Overview trend charts to an independently authorized endpoint and frontend lifecycle while reusing the shared #125 snapshot cache.

**Architecture:** `teamusage.Service.Trend` projects bounded trend fields from the same scope/provider/range-isolated snapshot used by summary and the compatibility overview. Department comparisons are ranked and capped at 12 while the independent team-total series remains complete. The Vue view starts summary, trend, and compatibility member requests independently and defines the chart component asynchronously so its code is requested only when trend data renders.

**Tech Stack:** Go, Ent, Gin, Redis read model from issue #125, Vue 3, TypeScript, Vitest, Vite.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/perf-team-trend-126` on `perf/team-trend-126`.
- Keep PostgreSQL, current representative scope, and current provider configuration authoritative on every request.
- Reuse the #125 snapshot cache and key; do not add a second trend cache, Relay calculation path, or internal HTTP call.
- Fresh lifetime remains 48-54 seconds and hard stale lifetime remains 4-4.5 minutes, below the 60-second/five-minute maxima.
- Return at most 12 top members and 12 department comparisons, while team total remains complete and independent.
- Stable series identities are positive local user ID or directory member external ID for members and department external ID for comparisons.
- Keep member ranking and organization rendering on the compatibility overview only until issues #127 and #128.
- Do not merge, release, tag, deploy, or run Helm.
- Tests and examples use synthetic identities only.
- Update every checkbox immediately after the action is completed.

**Status:** The original split Trend implementation is integrated into `feat/platform-loading-performance` through PR #154 at `93b7bcf9`; independent-origin and compatibility-adapter remediations #167/#172 are also present at the exact head `d2bc2694`. Stacked-Draft-PR details below are historical delivery evidence. This work is not merged to `main` or production-verified; #136 remains open, and #137 still owns later compatibility removal.

---

### Task 1: Bound Department Comparisons And Add The Trend Projection

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/scope_policy.go`
- Modify: `backend/internal/teamusage/scope_policy_test.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- Produces `TrendResponse` with common snapshot freshness, `scope_version`, request-local `request_id`, `window`, `top_members`, `top_member_trend`, and `department_trend`.
- Extends `DepartmentTrendState` with `comparison_total_count` and `comparison_truncated`.
- Produces `Service.Trend(context.Context, int, OverviewParams) (*TrendResponse, error)` over `readOverviewSnapshot`.

- [x] **Step 1: Add RED department comparison bound tests**

  Build 14 synthetic comparison buckets with token/cost ties and assert one complete `team_total`, exactly 12 comparisons, token-descending/cost-descending/external-id-ascending order, stable ranks, `comparison_total_count=14`, and `comparison_truncated=true`.

- [x] **Step 2: Run focused policy tests and record RED**

  Run: `cd backend && go test ./internal/teamusage -run 'DepartmentTrend.*Bound|TrendProjection' -count=1 -v`

  Expected: compile/assertion failures for absent comparison metadata and cap.

  RED evidence (2026-07-16): the focused command failed only because `comparison_total_count` and `comparison_truncated` were absent.

- [x] **Step 3: Implement bounded comparison selection and Trend projection**

  Preserve the team-total row before sorting/truncation, sort comparisons by selected-window token total descending, billed usage descending, then department external ID ascending, assign ranks after sorting, and project the cached snapshot without mutation.

- [x] **Step 4: Add and run shared-snapshot service tests**

  Assert summary/trend/overview requests under one key trigger one Relay generation; all three recheck scope/provider configuration guards; trend preserves stale/expired/error behavior from the shared cache; unavailable/empty trend states remain explicit.

  GREEN evidence (2026-07-16): bounded policy tests and shared summary/trend/overview projection tests pass, including eligible stale, hard expiry, and explicit `scope_too_large` empty series.

- [x] **Step 5: Verify Task 1 GREEN and checkpoint**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/*.go
  go test ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/teamusage -count=1
  git diff --check
  ```

  Commit: `perf(backend): project bounded team trends`

  GREEN evidence (2026-07-16): `go test ./internal/teamusage -count=2`, race tests for `internal/readcache` and `internal/teamusage`, and `git diff --check` all passed.

### Task 2: Expose The Authenticated Trend Route

**Files:**
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/team_usage_test.go`
- Modify: `backend/internal/handler/router.go`

**Interfaces:**
- Produces authenticated `GET /api/v1/user/team-usage/trend`.
- Generates a new `X-Request-ID` and matching response `request_id` in the handler, never in Redis.

- [x] **Step 1: Add RED HTTP tests**

  Cover normalized query forwarding, freshness/scope/request metadata, bounded response fields, unique request IDs, auth protection, 403 no-scope behavior, and 400 invalid input.

- [x] **Step 2: Run focused handler tests and record RED**

  Run: `cd backend && go test ./internal/handler -run 'TeamUsageTrend' -count=1 -v`

  Expected: compile/route failure because the trend handler is absent.

  RED evidence (2026-07-16): the focused command failed only because `TeamUsageHandler.Trend` was absent.

- [x] **Step 3: Implement route and request-local projection**

  Reuse the summary query parser and error mapping; do not expose compatibility headers on the new route.

- [x] **Step 4: Verify Task 2 GREEN and checkpoint**

  Run: `cd backend && go test ./internal/teamusage ./internal/handler ./cmd/server -count=2 && git diff --check`

  Commit: `feat(backend): expose split team usage trend`

  GREEN evidence (2026-07-16): focused trend handler tests passed, then `go test ./internal/teamusage ./internal/handler ./cmd/server -count=2` and `git diff --check` passed.

### Task 3: Load Trend And Chart Code Independently

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/teamUsage.ts`
- Modify: `frontend/src/views/TeamOverviewView.vue`
- Modify: `frontend/src/__tests__/team-usage-api.test.ts`
- Modify: `frontend/src/__tests__/team-overview-view.test.ts`

**Interfaces:**
- Produces `getTeamUsageTrend(params)` with the existing 45-second trend budget.
- Produces `TeamUsageTrendResponse` matching the backend DTO.
- Uses `defineAsyncComponent(() => import('@/components/team-usage/TeamOverviewMemberTrendChart.vue'))` and renders it only after trend data exists.

- [x] **Step 1: Add RED API/view tests**

  Cover delayed, empty, stale, failed, and successful trend responses; prove summary and member content survive trend failure; prove compatibility failure does not hide trend; and prove the chart is absent before trend success.

- [x] **Step 2: Run focused frontend tests and record RED**

  Run: `cd frontend && npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts`

  Expected: failures for the missing trend API and independent trend state.

  RED evidence (2026-07-16): 9 focused failures showed the missing trend API, absent independent request/state, stale legacy trend rendering, and missing local loading/error/freshness states.

- [x] **Step 3: Implement independent trend lifecycle and async chart**

  Keep separate request sequence, loading, data, and error refs for summary, trend, and compatibility members. Use only `TeamUsageTrendResponse` for chart props and ignore trend fields in the legacy response.

- [x] **Step 4: Verify Task 3 GREEN and checkpoint**

  Run: `cd frontend && npm test && npm run build`

  Confirm the build emits the chart component as an async chunk separate from the Team Overview route chunk.

  Commit: `perf(frontend): load team trends independently`

  GREEN evidence (2026-07-16): focused API/view tests passed 42/42, the full frontend suite passed 467/467, and the production build emitted `TeamOverviewMemberTrendChart-*.js` separately from `TeamOverviewView-*.js`.

### Task 4: Document, Verify, Review, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [x] **Step 1: Update current architecture**

  Record the split trend route, bounded comparison selection, shared snapshot reuse, independent frontend lifecycle, and async chart loading.

  Documentation evidence (2026-07-16): `docs/architecture.md` now records the split trend route, 12-comparison bound with complete team total, shared authorized snapshot, request-local metadata, three independent frontend lifecycles, and async chart chunk.

- [x] **Step 2: Run static and full verification**

  Run:

  ```bash
  git diff --check
  cd backend && go vet ./internal/teamusage ./internal/handler ./cmd/server && go test ./...
  cd ../frontend && npm test && npm run build
  cd ../ae-cli && go test ./...
  ```

  GREEN evidence (2026-07-16): `git diff --check`, changed-package `go vet`, backend `go test ./...`, race-enabled `readcache/teamusage`, frontend 39 files/467 tests plus production build, and ae-cli `go test ./...` all passed. The build emitted separate Team Overview route and trend chart chunks.

- [x] **Step 3: Review against issue #126 and the active performance spec**

  Audit every endpoint field, bound, stable identity, cache/freshness behavior, failure lifecycle, and lazy-import requirement; fix every finding and rerun affected tests.

  Review result (2026-07-16): one Important cache-migration finding was fixed. Because #126 adds comparison metadata and bounds the cached overview generation, the envelope schema is now v2 and rejects valid pre-bound v1 values before an authoritative rebuild. Its RED/GREEN regression, full backend suite, changed-package vet, and race tests passed. No unresolved standards or spec findings remain.

- [x] **Step 4: Push and open a Draft PR**

  Target `perf/team-summary-125`, list Draft PR #153 as the direct dependency, preserve the worktree, and do not merge or release.

  Draft PR: https://github.com/LichKing-2234/ai-efficiency/pull/154. It is OPEN and Draft, targets `perf/team-summary-125`, and records Draft PR #153 as its direct dependency.

- [x] **Step 5: Wait for required CI and record final state**

  Record the exact latest-head run and all backend/frontend/ae-cli/deploy-static conclusions.

  CI evidence: https://github.com/LichKing-2234/ai-efficiency/actions/runs/29474631505 passed for implementation head `4c95f5b`: backend (3m25s), frontend (55s), ae-cli (41s), and deploy-static (17s) all completed successfully.
