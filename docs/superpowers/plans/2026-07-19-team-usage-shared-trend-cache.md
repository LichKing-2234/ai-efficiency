# Team Usage Shared Trend Origin Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Ready for implementation. The design is approved and committed on `perf/team-usage-shared-trend-cache`; no production code has been changed.

**Goal:** Collapse duplicate per-user Relay trend reads across concurrent Team Usage lanes and reuse successful results for 60 seconds without coupling their response caches.

**Architecture:** Add a bounded process-local `teamTrendCache` owned by each `sub2apiRelay` provider instance. The cache uses normalized per-user range keys, the existing `readcache.FlightGroup` for concurrent collapse, credential generations for invalidation safety, defensive result cloning, a 60-second TTL, and a 4096-entry cap. `GetUsageTrendForUsers` keeps its public contract and eight-worker bound but delegates each origin read through this cache.

**Tech Stack:** Go 1.24, `net/http`, `httptest`, `sync`, existing `backend/internal/readcache.FlightGroup`, existing Relay provider interfaces.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance` on `perf/team-usage-shared-trend-cache`.
- Base and PR target remain `feat/platform-loading-performance`; do not target `main`.
- Do not modify Sub2API or require a new upstream endpoint.
- Preserve independent Summary, Trend, Members, and Organization response caches, HTTP DTOs, cursors, freshness, and stale-if-error behavior.
- Cache only successful per-user trend origin results for exactly 60 seconds.
- Keep at most 4096 entries per provider instance and keep the existing eight-worker caller bound.
- Errors, canceled operations, and values synthesized because of errors are never cached.
- Changed admin credentials increment the cache generation, separate new flights, clear entries, and prevent old flights from writing into the new generation.
- Reapplying the same normalized admin key does not invalidate the cache; model changes do not invalidate it.
- Redis GET retry behavior remains out of scope for this plan.
- Tests and examples use only synthetic IDs, domains, keys, and trend values.
- Update each checkbox immediately after completing that step.

---

### Task 1: Define The Bounded Trend Primitive Cache

**Files:**
- Create: `backend/internal/relay/sub2api_team_trend_cache.go`
- Create: `backend/internal/relay/sub2api_team_trend_cache_test.go`
- Modify: `backend/internal/relay/sub2api.go:26-37`

**Interfaces:**
- Produces `teamTrendCacheKey`, `teamTrendCacheEntry`, and `teamTrendCache`.
- Produces `(*teamTrendCache).GetOrLoad(context.Context, int64, TeamMemberTrendParams, func(context.Context) ([]UsageTrendPoint, error)) ([]UsageTrendPoint, error)`.
- Produces `(*teamTrendCache).Invalidate()` for credential-generation changes.
- Adds `teamTrends teamTrendCache` to `sub2apiRelay`.

- [ ] **Step 1: Add RED tests for normalized identity, TTL, cloning, and capacity**

  Create internal-package tests that use an injected clock and direct cache origin functions:

  ```go
  func TestTeamTrendCacheReusesNormalizedSuccessfulResult(t *testing.T) {
      now := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
      cache := teamTrendCache{now: func() time.Time { return now }}
      calls := 0
      load := func(context.Context) ([]UsageTrendPoint, error) {
          calls++
          tokens := int64(42)
          return []UsageTrendPoint{{Date: "2026-07-19", ActualCost: 1.5, TotalTokens: &tokens}}, nil
      }

      first, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
          StartDate: " 2026-07-01 ", EndDate: "2026-07-19", Granularity: " day ", Timezone: " Asia/Shanghai ",
      }, load)
      if err != nil {
          t.Fatal(err)
      }
      second, err := cache.GetOrLoad(context.Background(), 101, TeamMemberTrendParams{
          StartDate: "2026-07-01", EndDate: " 2026-07-19 ", Granularity: "day", Timezone: "Asia/Shanghai",
      }, load)
      if err != nil {
          t.Fatal(err)
      }
      if calls != 1 || len(first) != 1 || len(second) != 1 {
          t.Fatalf("calls/results = %d/%d/%d, want 1/1/1", calls, len(first), len(second))
      }
      *first[0].TotalTokens = 99
      if *second[0].TotalTokens != 42 {
          t.Fatalf("cached TotalTokens mutated through caller: %d", *second[0].TotalTokens)
      }
  }
  ```

  Add separate tests that advance the injected clock to exactly 60 seconds and require a second origin call, cache a successful nil/empty result, vary each identity dimension, prove a non-positive user ID bypasses both storage and flights, fill 4097 synthetic entries, and assert `len(cache.entries) <= teamTrendCacheCapacity` after every write.

- [ ] **Step 2: Run the focused tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/relay -run 'TeamTrendCache' -count=1 -v
  ```

  Expected: compile failures only for the absent `teamTrendCache` types, constants, and methods.

- [ ] **Step 3: Implement the minimal cache data model and cloning**

  Create the cache file with these constants and shapes:

  ```go
  const (
      teamTrendCacheTTL       = time.Minute
      teamTrendCacheCapacity  = 4096
      teamTrendFlightTimeout  = 20 * time.Second
  )

  type teamTrendCacheKey struct {
      RelayUserID int64
      StartDate   string
      EndDate     string
      Granularity string
      Timezone    string
  }

  type teamTrendCacheEntry struct {
      Points    []UsageTrendPoint
      ExpiresAt time.Time
  }

  type teamTrendCache struct {
      mu         sync.Mutex
      entries    map[teamTrendCacheKey]teamTrendCacheEntry
      generation uint64
      flights    readcache.FlightGroup[[]UsageTrendPoint]
      now        func() time.Time
  }
  ```

  Implement key normalization with `strings.TrimSpace`, a canonical flight key using `url.Values.Encode()` plus the decimal credential generation, lazy map initialization, read-time addressed expiry removal, write-time full expiry pruning, earliest-expiration eviction, and deep cloning of `TotalTokens` pointers.

  `GetOrLoad` must:

  ```go
  func (c *teamTrendCache) GetOrLoad(
      ctx context.Context,
      relayUserID int64,
      params TeamMemberTrendParams,
      load func(context.Context) ([]UsageTrendPoint, error),
  ) ([]UsageTrendPoint, error)
  ```

  Bypass cache and flights for non-positive IDs. For valid IDs, capture the current generation, check the cache, enter `FlightGroup.Do` on a miss, double-check inside the flight, store only on success and unchanged generation, then clone the flight result separately for every waiter.

- [ ] **Step 4: Run focused GREEN and commit the primitive**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/sub2api_team_trend_cache.go internal/relay/sub2api_team_trend_cache_test.go internal/relay/sub2api.go
  go test ./internal/relay -run 'TeamTrendCache' -count=2
  go test -race ./internal/readcache ./internal/relay -run 'TeamTrendCache|FlightGroup' -count=1
  cd ..
  git diff --check
  ```

  Expected: both focused runs pass, the race detector reports no races, and the diff is clean.

  Commit:

  ```bash
  git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_team_trend_cache.go backend/internal/relay/sub2api_team_trend_cache_test.go docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md
  git commit -m "perf(relay): cache team usage trend origins"
  ```

### Task 2: Route Team Usage Trend Fan-Out Through The Cache

**Files:**
- Modify: `backend/internal/relay/sub2api.go:108-112`
- Modify: `backend/internal/relay/sub2api.go:2288-2354`
- Modify: `backend/internal/relay/sub2api_team_trend_cache_test.go`
- Verify: `backend/internal/relay/sub2api_test.go`

**Interfaces:**
- Consumes `teamTrendCache.GetOrLoad` and `teamTrendCache.Invalidate` from Task 1.
- Preserves `TeamMemberTrendProvider.GetUsageTrendForUsers` and `TeamUsageSummaryProvider.GetBatchUserUsageStats` signatures and response semantics.

- [ ] **Step 1: Add RED HTTP tests for four-lane collapse and error behavior**

  Build an `httptest.Server` that handles `/api/v1/admin/dashboard/trend`, records `user_id`, and blocks the first wave until four callers have started. Run four concurrent `GetUsageTrendForUsers` calls over synthetic IDs 1 through 235 with the same range. Require:

  ```go
  if got := upstreamCalls.Load(); got != 235 {
      t.Fatalf("upstream trend calls = %d, want 235 for four identical 235-user callers", got)
  }
  ```

  Require all four results to contain 235 users. Add a separate single-caller test with 16 blocked synthetic users and an atomic active/max-active counter to prove that one `GetUsageTrendForUsers` call still runs at most eight upstream requests concurrently.

  Add focused tests requiring:

  - one failed upstream response followed by success produces two upstream calls;
  - one successful empty trend followed by another read produces one upstream call;
  - canceling one of two waiters does not cancel the remaining waiter;
  - canceling the only waiter leaves no cached result;
  - changing the admin API key during a blocked origin lets a new request use a separate flight and prevents the old flight from storing;
  - reapplying the same trimmed key keeps the existing entry.
  - changing only the inference model keeps the existing entry.

- [ ] **Step 2: Run the integration tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/relay -run 'TeamTrendCache.*(Fanout|Error|Cancel|Credential)' -count=1 -v
  ```

  Expected: the four-lane test observes 940 upstream calls before integration, while cache reuse and credential-generation assertions fail because `GetUsageTrendForUsers` and `SetAdminAPIKey` do not yet use the cache.

- [ ] **Step 3: Wire per-user reads and credential invalidation**

  Replace the worker origin call with:

  ```go
  trend, err := s.teamTrends.GetOrLoad(trendCtx, relayUserID, params, func(loadCtx context.Context) ([]UsageTrendPoint, error) {
      return s.getTeamMemberTrend(loadCtx, relayUserID, params)
  })
  ```

  Refactor `SetAdminAPIKey` so it compares normalized values under `s.mu`, releases that mutex, and calls `s.teamTrends.Invalidate()` only when the value changed. `Invalidate` increments the generation and replaces the entry map while leaving old flights isolated by their generation-specific keys.

- [ ] **Step 4: Run focused GREEN, broader Relay regressions, and race checks**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/sub2api.go internal/relay/sub2api_team_trend_cache.go internal/relay/sub2api_team_trend_cache_test.go
  go test ./internal/relay -run 'TeamTrendCache|UsageTrendForUsers|GetBatchUserUsageStats' -count=2
  go test -race ./internal/readcache ./internal/relay -run 'TeamTrendCache|UsageTrendForUsers|GetBatchUserUsageStats|FlightGroup' -count=1
  cd ..
  git diff --check
  ```

  Expected: all focused tests pass twice, race checks pass, and no existing Relay contract test changes its expected HTTP path or DTO.

- [ ] **Step 5: Commit the adapter integration**

  ```bash
  git add backend/internal/relay/sub2api.go backend/internal/relay/sub2api_team_trend_cache.go backend/internal/relay/sub2api_team_trend_cache_test.go docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md
  git commit -m "perf(teamusage): collapse duplicate trend reads"
  ```

### Task 3: Synchronize Current Architecture And Verify The Backend

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md`
- Verify: `docs/superpowers/specs/2026-07-19-team-usage-shared-trend-cache-design.md`
- Verify: `backend/internal/teamusage`
- Verify: `backend/internal/handler`
- Verify: `backend/cmd/server`

**Interfaces:**
- Records the 60-second provider-local primitive cache as current runtime behavior.
- Does not change HTTP or frontend contracts.

- [ ] **Step 1: Update the current architecture description**

  Extend the Representative scope and Team Usage module description in `docs/architecture.md` to state that split response caches remain independent while the Sub2API Relay adapter shares successful per-user trend origins for 60 seconds inside one provider instance, with bounded capacity and configuration/credential invalidation.

- [ ] **Step 2: Run service and handler compatibility verification**

  Run:

  ```bash
  cd backend
  go test ./internal/teamusage ./internal/handler ./cmd/server -run 'TeamUsage|ProductionCacheMetrics|RuntimeHTTPClients' -count=2
  go test -race ./internal/readcache ./internal/relay ./internal/teamusage -run 'TeamTrendCache|TeamUsage|CacheMetrics|FlightGroup' -count=1
  ```

  Expected: split endpoint cache/isolation tests, compatibility Overview tests, production metrics wiring, and races all pass without expectation changes.

- [ ] **Step 3: Run the full backend verification gate**

  Run:

  ```bash
  cd backend
  go test ./... -count=1
  go vet ./...
  go build ./...
  cd ..
  git diff --check
  ```

  Expected: every command exits zero. No frontend build is required because no frontend source or API contract changes; GitHub CI remains the complete repository gate.

- [ ] **Step 4: Record evidence and commit documentation**

  Update this plan immediately with exact focused, race, and full-backend results. Set the top Status to implementation complete and awaiting PR CI, without claiming staging or production verification.

  Commit:

  ```bash
  git add docs/architecture.md docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md
  git commit -m "docs(teamusage): record shared trend cache behavior"
  ```

### Task 4: Deliver A Small PR To The Performance Integration Branch

**Files:**
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md`

**Interfaces:**
- Produces one non-Draft PR from `perf/team-usage-shared-trend-cache` to `feat/platform-loading-performance`.
- Keeps staging and production unchanged until review and merge.

- [ ] **Step 1: Review the exact branch diff and commit state**

  Run:

  ```bash
  git status --short --branch
  git diff --check feat/platform-loading-performance...HEAD
  git log --oneline feat/platform-loading-performance..HEAD
  git diff --stat feat/platform-loading-performance...HEAD
  ```

  Expected: a clean branch containing only the spec, plan, bounded cache implementation/tests, and architecture update.

- [ ] **Step 2: Push and open the PR**

  ```bash
  git push -u origin perf/team-usage-shared-trend-cache
  gh pr create \
    --repo LichKing-2234/ai-efficiency \
    --base feat/platform-loading-performance \
    --head perf/team-usage-shared-trend-cache \
    --title "perf(teamusage): reuse per-user trend origins" \
    --body-file /tmp/ai-efficiency-team-trend-cache-pr.md
  ```

  Compose the PR body in the command process or session-temporary `/tmp` file only. It must summarize the staging baseline, 60-second/4096-entry contract, credential-generation safety, exact verification commands, no-Sub2API boundary, and separate Redis-retry follow-up.

- [ ] **Step 3: Wait for exact-head CI and record the result**

  Run:

  ```bash
  gh pr checks --repo LichKing-2234/ai-efficiency --watch --interval 10
  ```

  Require backend, frontend, ae-cli, and deploy-static success on the exact PR head. Update this plan with the PR number, head SHA, run ID, and result, then commit and push that ledger update if it changes tracked content.

- [ ] **Step 4: Stop at the merge gate**

  Do not merge the PR automatically. Report the exact PR and CI state and wait for explicit user confirmation or observed merge before staging deployment.

### Task 5: Re-Audit Staging After Integration

**Files:**
- Modify after merge: `/Users/admin/helm/ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify after merge: `docs/superpowers/plans/2026-07-19-team-usage-shared-trend-cache.md`

**Interfaces:**
- Produces an immutable `staging-<full-commit>` multi-architecture image for the merged integration head.
- Advances only Helm release `ai-efficiency-staging` in namespace `la3-ai-efficiency-prod`.
- Produces comparable sanitized Team Usage audit evidence for the same 251-member scope.

- [ ] **Step 1: Verify merge state and publish the exact integration image**

  After the PR is merged, fast-forward `feat/platform-loading-performance`, set `COMMIT="$(git rev-parse HEAD)"`, build and push `ghcr.io/lichking-2234/ai-efficiency:staging-${COMMIT}` for `linux/amd64,linux/arm64`, and verify the manifest with `docker buildx imagetools inspect`.

- [ ] **Step 2: Update only staging selectors and perform the two-phase Helm rollout**

  Update the staging image tag and restore snapshot ID `${COMMIT:0:12}` in the Helm secret override, commit the selector change, run Phase A with the application scaled down, confirm the old Pod is gone, then run Phase B restore with `--atomic --wait --wait-for-jobs --timeout 20m`. Production must remain revision 68 on `v0.1.0-preview.72` unless a later explicit production release changes that source of truth.

- [ ] **Step 3: Repeat cold, warm, and dependency-log sampling**

  Use the same SSO account without persisting credentials. Request Summary, Trend, Members limit 50, and Organization limits 25/50 concurrently for `2026-06-20..2026-07-19`, then repeat immediately. Record only status, seconds, response bytes, cache/source status, counts, request IDs, dependency calls, dependency duration, and cache/Redis counters.

  Acceptance evidence:

  - all four endpoint response contracts remain HTTP 200 and available;
  - four concurrent cold lanes produce no more than one upstream trend request per unique Relay user/range generation;
  - total Relay request count is materially below the `b03cf0a8` baseline of 607 to 629;
  - normal warm reads remain dependency-free;
  - production image, readiness, and Helm revision remain unchanged.

- [ ] **Step 4: Record staging evidence and leave the plan honest**

  Update the top Status with exact image digest, staging revision, request measurements, and remaining Redis-retry follow-up. Do not mark production verification or issue #136 complete.
