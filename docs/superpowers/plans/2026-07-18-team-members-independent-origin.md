# Team Members Independent Origin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Complete. PR #181 was squash-merged into `feat/platform-loading-performance` at `c5566cb9d62f37dd7f0ff5acc4dd9cca8d40e3ee`; exact PR head `67925f4689646982241562ac7317e3dcad377842` passed all required CI jobs, issue #168 is closed, and #173 has the dependency update.

**Goal:** Complete issue #168 by serving Team Usage member pages from a ranking-specific immutable snapshot without building Trend DTOs or a recursive organization tree before pagination.

**Architecture:** Add a `MembersSnapshot` lane beside Summary, Trend, and compatibility Overview in `teamusage.SnapshotCache`. `Service.Members` will authorize and version the current scope, load only Relay identities plus complete selected-window batch usage stats, rank immutable member rows once, and page them through the existing HMAC cursor contract. Compatibility Overview and Organization remain on their current lane until #170/#172.

**Tech Stack:** Go 1.24, Ent, Gin, Redis/readcache, Prometheus, Vue 3, TypeScript, Vitest, Vite.

## Global Constraints

- Keep the current `GET /api/v1/user/team-usage/members` request and response fields unchanged.
- Keep default limit 50, maximum limit 100, deterministic total-token/stable-subject ordering, and complete authorized-scope ranking.
- Keep provider configuration version, actor, scope version/hash, range, granularity, and timezone in every Members cache key.
- Members cache values contain only normalized window and ranked member rows; no summary, trend series, department comparisons, or `member_tree`.
- Use `TeamUsageSummaryProvider.GetBatchUserUsageStats` with at most 100 Relay user IDs per call and require complete selected-window totals.
- Never acquire or invoke `TeamMemberTrendProvider` from the Members service lane.
- Preserve the existing HMAC cursor, snapshot identity, actor/scope/range isolation, expiration, Redis fallback, and response-size contracts.
- Add the stable production cache name `team_usage_members` with only `cache` and `outcome` labels and the existing closed outcome set.
- Keep Organization and compatibility Overview on `team_usage_overview` until #170/#172.
- Use synthetic identities and domains in all tests and examples.
- Update this plan checkbox immediately after each completed step.

---

### Task 1: Define The Members Snapshot And Cache Contract

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/summary_cache_test.go`
- Modify: `backend/internal/teamusage/cache_metrics_test.go`
- Modify: `backend/cmd/server/cache_metrics_test.go`

**Interfaces:**
- Produces `MembersSnapshot`, `MembersOriginLoadResult`, `MembersOriginLoader`, and `MembersCacheResult`.
- Produces `SnapshotCache.GetMembersOrLoad(context.Context, SnapshotCacheKey, MembersOriginLoader)`.
- Extends `SnapshotCacheOptions` with `MembersMetrics readcache.Metrics`.

- [x] **Step 1: Add RED cache-isolation and payload-validation tests**

  Require a distinct `team-usage-members` key, cold/fresh/stale/expired behavior, strict schema/window/ranked-row validation, and serialized values without `summary`, Trend state/series, department series, or `member_tree`.

- [x] **Step 2: Add RED metrics tests**

  Require a distinct `MembersMetrics` recorder, exactly one outcome per logical read, and production recorder name `team_usage_members` with labels exactly `cache` and `outcome`.

- [x] **Step 3: Run focused RED and record the expected failures**

  ```bash
  cd backend
  go test ./internal/teamusage ./cmd/server -run 'Members.*(Cache|Metrics)|ProductionCacheMetrics' -count=1 -v
  ```

  Expected: compile failures for missing Members snapshot/cache interfaces, `MembersMetrics`, and production recorder wiring.

  RED evidence (2026-07-18): the focused command failed only on missing `membersCacheKey`, Members snapshot/load/result types, `GetMembersOrLoad`, `MembersMetrics`, and the absent `team_usage_members` production recorder.

### Task 2: Implement The Independent Ranking Origin

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/summary_cache.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/members.go`
- Modify: `backend/internal/teamusage/members_test.go`
- Modify: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- `readMembersSnapshot(context.Context, int, OverviewParams) (*MembersCacheResult, string, error)` owns Members authorization/version/cache flow.
- `generateMembersSnapshot(context.Context, *representativescope.Scope, relay.Provider, OverviewParams) (*MembersSnapshot, error)` owns the bounded ranking origin.

- [x] **Step 1: Add RED 500-member origin and isolation tests**

  Require five stats calls of at most 100 Relay user IDs, zero direct Trend calls, complete deterministic ranking, and no Overview/Trend key population on a first-page request.

- [x] **Step 2: Add RED failure and cursor lifecycle tests**

  Require Trend failure not to affect Members, Redis outage recovery to preserve same-content cursors, summary/overview failure isolation, and a changed `RangeTotalTokens` generation to expire an existing cursor.

  RED evidence (2026-07-18): focused Members tests reproduced the compatibility Overview key write, direct Trend call, ignored range-stats mutation, Redis-outage Trend fan-out, incorrect ranking during transient Trend failure, and Members failure after a compatibility Overview Trend credential error.

- [x] **Step 3: Add the Members cache lane and bounded origin**

  Add schema version 1, prefix `team-usage-members`, strict validation, metrics, common lease/stale/fallback policy, 100-user stats chunks with `RequireCompleteRange=true`, and ranked immutable rows built from selected-window totals.

- [x] **Step 4: Switch `Service.Members` without changing the cursor contract**

  Replace the compatibility Overview read with `readMembersSnapshot`, keep `memberSnapshotIdentity`, HMAC cursor decoding/encoding, limits, freshness, scope version, request ID, and response fields unchanged.

- [x] **Step 5: Verify focused GREEN**

  ```bash
  cd backend
  gofmt -w internal/teamusage/*.go cmd/server/*.go
  go test ./internal/teamusage -run 'Members|SummaryOverviewCacheIsolation|CacheMetrics' -count=2
  go test -race ./internal/readcache ./internal/teamusage -run 'Members|CacheMetrics' -count=1
  git diff --check
  ```

  GREEN evidence (2026-07-18): focused Members/cache/isolation tests passed twice, focused `readcache`/`teamusage` race verification passed, and `git diff --check` was clean.

### Task 3: Wire Production Telemetry And Revalidate HTTP/Frontend Boundaries

**Files:**
- Modify: `backend/cmd/server/cache_metrics.go`
- Modify: `backend/cmd/server/main.go`
- Verify: `backend/internal/handler/team_usage.go`
- Verify: `backend/internal/handler/team_usage_test.go`
- Verify: `frontend/src/api/teamUsage.ts`
- Verify: `frontend/src/views/TeamOverviewView.vue`
- Verify: `frontend/src/__tests__/team-usage-api.test.ts`
- Verify: `frontend/src/__tests__/team-overview-view.test.ts`

- [x] **Step 1: Wire `team_usage_members` telemetry and HTTP integration coverage**

  Inject the production recorder into `SnapshotCacheOptions.MembersMetrics` and prove a real Service + Redis first-page HTTP request never creates Overview or Trend keys.

  Integration evidence (2026-07-18): production wiring exposes `team_usage_members`; a real Gin + Service + Redis 500-member request returned 50 rows, used five 100-user complete-range stats batches, made zero direct Trend calls, and wrote only the Members key.

- [x] **Step 2: Revalidate the 500-member frontend viewport contract**

  ```bash
  cd backend
  go test ./internal/handler ./cmd/server -run 'TeamUsageMembers|ProductionCacheMetrics' -count=2
  cd ../frontend
  npm test -- src/__tests__/team-usage-api.test.ts src/__tests__/team-overview-view.test.ts
  npm run build
  ```

  Confirm a 500-member fixture renders only the requested 50 ranking rows and does not duplicate hidden row subtrees.

  Verification evidence (2026-07-18): handler/server tests passed twice, the focused Team Usage API/View suite passed 62/62 including the 500-member 50-row assertion, and the production frontend build completed successfully.

### Task 4: Document, Verify, Review, And Deliver

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: `deploy/observability/README.md`
- Modify: this plan

- [x] **Step 1: Update current architecture and the active performance contract**

  Record the independent Members origin/cache/value boundary, 100-user chunk bound, stable metric name, failure isolation, cursor lifecycle, and compatibility Overview/Organization separation. Do not rewrite historical Team Usage specs.

  Documentation evidence (2026-07-18): current architecture, the active 2026-07-14 performance contract, and the observability operator README now record the independent Members lane and `team_usage_members`; historical Team Usage specs remain unchanged.

- [x] **Step 2: Run full verification**

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

  Verification evidence (2026-07-18): `git diff --check`; backend `go test ./... -count=1`, `go vet ./...`, and race-enabled `readcache`/`teamusage`/`telemetry`; frontend 46 files / 680 tests and production build; ae-cli `go test ./... -count=1`; and the embedded release frontend policy all passed. The embed script emitted its known synthetic unresolved-hook warning before completing successfully.

- [x] **Step 3: Review exact branch against issue #168 and the active spec**

  Confirm bounded origin calls, no Trend provider call or unrelated cache warming, strict cached payload validation, deterministic cursor pages, snapshot expiry on ranking changes, section-local failure behavior, low-cardinality metrics, and no Critical or Important findings. Fix every finding and rerun affected verification.

  Review evidence (2026-07-18): the first two-axis review found two Important documentation/ledger drifts and three Minor implementation observations. Commits `c246307b` and `70ae3455` align both architecture locations and the live plan status, reject negative cached user IDs with RED/GREEN coverage, and add contextual `%w` wrapping on the new Members path. Focused and full verification pass afterward; both Standards and Spec re-reviews report no remaining Critical, Important, or actionable Minor findings. The repeated typed-lane orchestration remains an existing maintainability observation and is not expanded into a generic abstraction in this focused ticket.

- [x] **Step 4: Deliver to the performance feature branch**

  Push `feat/team-members-independent-168`, open a PR targeting `feat/platform-loading-performance`, wait for exact-head backend/frontend/ae-cli/deploy-static CI, squash-merge, close #168 with exact evidence, update #173, and record the integration state in this plan.

  Delivery evidence (2026-07-18): PR #181 targeted `feat/platform-loading-performance`; exact head `67925f4689646982241562ac7317e3dcad377842` passed backend, frontend, ae-cli, and deploy-static in run `29655717466`. The PR was squash-merged at `c5566cb9d62f37dd7f0ff5acc4dd9cca8d40e3ee`; issue #168 was closed manually because the base is non-default, and #173 received the dependency evidence.
