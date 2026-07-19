# Team Usage Cold Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Approved design and clean backend baseline are complete on `perf/team-usage-cold-loading`; implementation, review, PR CI, merge, and staging A/B remain pending.

**Goal:** Reduce large-team cold page completion by raising bounded per-user trend concurrency to a provider-wide sixteen slots and keep Team Usage response snapshots fresh for a three-minute pre-jitter maximum.

**Architecture:** Add a zero-value-safe origin limiter owned by each `sub2apiRelay` provider and route every per-user trend origin through it after cache/singleflight collapse. Increase caller workers to the same capacity so one large caller can fill the limiter. Change only the shared Team Usage read-model envelope fresh maximum from one minute to three minutes while preserving its existing jitter, stale deadline, Redis hard TTL, cache identities, and stale-if-error flow.

**Tech Stack:** Go 1.24, `net/http`, `httptest`, `sync`, existing `readcache.FlightGroup`, existing Redis read-model cache and miniredis tests.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance` on `perf/team-usage-cold-loading`.
- Base and PR target remain `feat/platform-loading-performance`; do not target `main`.
- Do not modify Sub2API, frontend source, HTTP DTOs, Redis keys, cursors, or provider interfaces.
- The provider-wide trend origin limit is exactly sixteen and is not configurable in this change.
- Cache hits and shared-flight waiters do not consume origin slots; all actual origins, including invalid-ID bypass origins, do.
- Credential invalidation does not replace the limiter or create another sixteen-slot pool.
- Team Usage fresh lifetime is `3m - 10-20% jitter`, exactly 144-162 seconds.
- Team Usage stale lifetime remains `5m - 10-20% jitter`, exactly 240-270 seconds after generation.
- The per-user trend cache remains 60 seconds with 4096 entries and a 20-second flight timeout.
- Redis GET retry, user-directory caching, batch-stat caching, concurrency above sixteen, and production deployment remain out of scope.
- Tests and examples use only synthetic users, IDs, keys, ranges, and responses.
- Update every checkbox immediately after the corresponding action completes.

---

### Task 1: Enforce Sixteen Provider-Wide Trend Origin Slots

**Files:**
- Create: `backend/internal/relay/sub2api_team_trend_limiter.go`
- Modify: `backend/internal/relay/sub2api.go:23-32`
- Modify: `backend/internal/relay/sub2api.go:2294-2326`
- Modify: `backend/internal/relay/sub2api_team_trend_cache_test.go:243-302`

**Interfaces:**
- Produces `const maxConcurrentTeamTrendOrigins = 16`.
- Produces zero-value-safe `teamTrendOriginLimiter` with `Do(context.Context, func(context.Context) ([]UsageTrendPoint, error)) ([]UsageTrendPoint, error)`.
- Adds `teamTrendOrigins teamTrendOriginLimiter` to `sub2apiRelay`.
- Preserves `TeamMemberTrendProvider.GetUsageTrendForUsers` and all existing cache/error contracts.

- [x] **Step 1: Add RED tests for one-caller saturation and the provider-wide bound**

  Replace `TestTeamTrendCachePreservesEightWorkerCallerLimit` with an integration test named `TestTeamTrendOriginsUseSixteenProviderWideSlots`. Use one provider backed by one blocking `httptest.Server`, two disjoint 32-user slices, and a mutex-protected active/request/max counter.

  Start the first caller and require sixteen origins to reach the handler before releasing anything:

  ```go
  for index := 0; index < maxConcurrentTeamTrendOrigins; index++ {
      select {
      case <-started:
      case <-time.After(time.Second):
          close(release)
          t.Fatalf("only %d origins started, want %d", index, maxConcurrentTeamTrendOrigins)
      }
  }
  ```

  Then start the disjoint second caller, wait 50 milliseconds, and require `requestCount == 16` and `maxActive == 16` while the first wave is blocked. Release the handler, require both callers to succeed with 32 results each, and require 64 total origins with `maxActive == 16`.

  This single test must fail on the current code because one caller starts only eight requests. A naive per-caller-only increase must fail its second assertion by starting more than sixteen origins.

- [x] **Step 2: Add RED tests for canceled slot wait and credential-generation sharing**

  Add a direct limiter test named `TestTeamTrendOriginLimiterDoesNotStartCanceledWaiter`:

  ```go
  var limiter teamTrendOriginLimiter
  release := make(chan struct{})
  started := make(chan struct{}, maxConcurrentTeamTrendOrigins+1)
  done := make(chan struct{}, maxConcurrentTeamTrendOrigins)
  for range maxConcurrentTeamTrendOrigins {
      go func() {
          defer func() { done <- struct{}{} }()
          _, _ = limiter.Do(context.Background(), func(context.Context) ([]UsageTrendPoint, error) {
              started <- struct{}{}
              <-release
              return nil, nil
          })
      }()
  }
  for range maxConcurrentTeamTrendOrigins {
      <-started
  }
  ctx, cancel := context.WithCancel(context.Background())
  cancel()
  _, err := limiter.Do(ctx, func(context.Context) ([]UsageTrendPoint, error) {
      started <- struct{}{}
      return nil, nil
  })
  if !errors.Is(err, context.Canceled) {
      t.Fatalf("Do() error = %v, want context.Canceled", err)
  }
  if len(started) != 0 {
      t.Fatal("canceled waiter started an origin")
  }
  close(release)
  for range maxConcurrentTeamTrendOrigins {
      <-done
  }
  ```

  Add `TestTeamTrendOriginLimiterSpansCredentialGenerations`. Saturate all sixteen slots with old-key origins, call `SetAdminAPIKey("new-admin-key")`, start a new-generation user request, and prove it does not reach the handler until one old origin is released. This locks down that invalidation never replaces the limiter.

- [x] **Step 3: Run the focused tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache.*(Sixteen|Credential)' -count=1 -v
  ```

  Expected: compile failures for the absent limiter symbols and/or the saturation test times out after seeing only eight origins. No unrelated fixture or syntax failure is acceptable.

  RED evidence (2026-07-19): the focused command failed only because
  `maxConcurrentTeamTrendOrigins` and `teamTrendOriginLimiter` were absent.
  The new tests compiled far enough to identify the planned production symbols
  without fixture, handler, or syntax failures.

- [x] **Step 4: Implement the minimal zero-value-safe limiter and wire it after singleflight**

  Create the limiter module:

  ```go
  package relay

  import (
      "context"
      "sync"
  )

  const maxConcurrentTeamTrendOrigins = 16

  type teamTrendOriginLimiter struct {
      once  sync.Once
      slots chan struct{}
  }

  func (l *teamTrendOriginLimiter) Do(
      ctx context.Context,
      load func(context.Context) ([]UsageTrendPoint, error),
  ) ([]UsageTrendPoint, error) {
      l.once.Do(func() {
          l.slots = make(chan struct{}, maxConcurrentTeamTrendOrigins)
      })
      select {
      case l.slots <- struct{}{}:
          defer func() { <-l.slots }()
          return load(ctx)
      case <-ctx.Done():
          return nil, ctx.Err()
      }
  }
  ```

  Add `teamTrendOrigins teamTrendOriginLimiter` beside `teamTrends`. Change `GetUsageTrendForUsers` to use `maxConcurrentTeamTrendOrigins` for its worker count. Wrap only the cache loader's actual HTTP origin:

  ```go
  trend, err := s.teamTrends.GetOrLoad(trendCtx, relayUserID, params, func(loadCtx context.Context) ([]UsageTrendPoint, error) {
      return s.teamTrendOrigins.Do(loadCtx, func(originCtx context.Context) ([]UsageTrendPoint, error) {
          return s.getTeamMemberTrend(originCtx, relayUserID, params)
      })
  })
  ```

  Do not reinitialize the limiter in `SetAdminAPIKey` or provider invalidation.

- [x] **Step 5: Run focused GREEN, integration regressions, and race checks**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/sub2api.go internal/relay/sub2api_team_trend_limiter.go internal/relay/sub2api_team_trend_cache_test.go
  go test ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache|UsageTrendForUsers|GetBatchUserUsageStats' -count=2
  go test -race ./internal/readcache ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache|FlightGroup' -count=1
  cd ..
  git diff --check
  ```

  Expected: one caller fills sixteen slots, disjoint callers remain globally bounded at sixteen, shared callers still collapse, cancellation and credential generation remain safe, the focused suite passes twice, the race detector reports no races, and the diff is clean.

  GREEN evidence (2026-07-19): the focused Relay suite passed twice with one
  caller filling sixteen slots, two disjoint callers remaining globally
  bounded at sixteen, and shared callers retaining one origin per user.
  Canceled slot waits and credential generation changes remained bounded. The
  race-enabled `internal/readcache` and `internal/relay` run passed, and
  `git diff --check` was clean.

- [x] **Step 6: Record evidence and commit the limiter**

  Update this plan immediately with RED/GREEN evidence and commit:

  ```bash
  git add backend/internal/relay/sub2api.go \
    backend/internal/relay/sub2api_team_trend_limiter.go \
    backend/internal/relay/sub2api_team_trend_cache_test.go \
    docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md
  git commit -m "perf(relay): raise bounded team trend concurrency"
  ```

### Task 2: Extend Team Usage Response Freshness To Three Minutes

**Files:**
- Modify: `backend/internal/teamusage/summary_cache.go:21-26`
- Modify: `backend/internal/teamusage/summary_cache.go:469-523`
- Modify: `backend/internal/teamusage/summary_cache_test.go`
- Modify: `backend/internal/teamusage/cache_metrics_test.go:130-156`

**Interfaces:**
- Produces `teamUsageSnapshotFreshMaxAge = 3 * time.Minute` and retains `teamUsageSnapshotStaleMaxAge = 5 * time.Minute`.
- Preserves all four `SnapshotCache.Get*OrLoad` interfaces and the `SnapshotFreshness` DTO.
- Applies the same fresh/stale envelope validation to Summary, Trend, Members, and Organization.

- [ ] **Step 1: Change the contract tests to RED for 144-162 second freshness**

  Change `TestSummaryCacheColdMissWarmHitAndJitterBounds` cases to:

  ```go
  {
      {name: "minimum jitter", random: 0, freshWindow: 2*time.Minute + 42*time.Second, staleWindow: 4*time.Minute + 30*time.Second},
      {name: "maximum jitter", random: 1, freshWindow: 2*time.Minute + 24*time.Second, staleWindow: 4 * time.Minute},
  }
  ```

  Change valid stale-transition tests for Summary, Trend, Members, Organization, and cache metrics from `now.Add(55 * time.Second)` to `now.Add(2*time.Minute + 43*time.Second)`. Keep hard-expiry assertions beyond their current stale deadline. Update manually encoded valid envelopes from a 54-second fresh window to 162 seconds; wrong-schema fixtures may retain arbitrary old timing because schema rejection is the assertion.

- [ ] **Step 2: Run the focused cache tests and record RED**

  Run:

  ```bash
  cd backend
  go test ./internal/teamusage -run 'Cache.*(Jitter|Stale|Metrics)|SummaryCacheColdMissWarmHitAndJitterBounds' -count=1 -v
  ```

  Expected: the jitter test reports 54 seconds instead of 162 seconds, and the new stale-transition tests receive `fresh` because the current implementation has not yet reached the requested window. Failures must be freshness-specific.

- [ ] **Step 3: Implement named fresh/stale maxima and envelope validation**

  Add shared constants beside the schema constants:

  ```go
  const (
      teamUsageSnapshotFreshMaxAge = 3 * time.Minute
      teamUsageSnapshotStaleMaxAge = 5 * time.Minute
  )
  ```

  Update `newEnvelope`:

  ```go
  freshWindow := teamUsageSnapshotFreshMaxAge - time.Duration(jitter*float64(teamUsageSnapshotFreshMaxAge))
  staleWindow := teamUsageSnapshotStaleMaxAge - time.Duration(jitter*float64(teamUsageSnapshotStaleMaxAge))
  ```

  Update `validEnvelope` to accept only fresh windows from 144 through 162 seconds and stale windows from 240 through 270 seconds, with `staleWindow > freshWindow`. Do not change key versions, payload schema versions, Redis TTL assignment, or cache statuses.

- [ ] **Step 4: Run focused GREEN and all Team Usage cache regressions**

  Run:

  ```bash
  cd backend
  gofmt -w internal/teamusage/summary_cache.go internal/teamusage/summary_cache_test.go internal/teamusage/cache_metrics_test.go
  go test ./internal/teamusage -run 'Cache|Summary|Trend|Members|Organization' -count=2
  go test -race ./internal/readcache ./internal/teamusage -run 'Cache|FlightGroup' -count=1
  cd ..
  git diff --check
  ```

  Expected: all four lanes retain fresh/miss/stale/error semantics, jitter bounds are 144-162 and 240-270 seconds, Redis TTL remains the stale deadline, focused tests pass twice, race checks pass, and the diff is clean.

- [ ] **Step 5: Record evidence and commit the freshness change**

  Update this plan immediately with RED/GREEN evidence and commit:

  ```bash
  git add backend/internal/teamusage/summary_cache.go \
    backend/internal/teamusage/summary_cache_test.go \
    backend/internal/teamusage/cache_metrics_test.go \
    docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md
  git commit -m "perf(teamusage): extend response cache freshness"
  ```

### Task 3: Synchronize Current Documentation And Verify The Branch

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-19-team-usage-cold-loading-design.md`
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md`

**Interfaces:**
- Records the provider-wide sixteen-slot limiter and 144-162 second Team Usage fresh window as current runtime behavior.
- Does not change HTTP, frontend, provider, Redis key, or deployment interfaces.

- [ ] **Step 1: Update current architecture and spec status**

  In `docs/architecture.md`, replace the eight-worker per-caller description with a provider-wide sixteen-origin bound after cache/singleflight collapse. Replace Team Usage's 48-54 second fresh description with 144-162 seconds while keeping the 240-270 second stale deadline and current stale-if-error behavior.

  Set the approved spec status to implemented on the exact branch commit only after both code tasks are green. Do not modify the historical shared-trend-cache spec.

- [ ] **Step 2: Run full backend verification**

  Run:

  ```bash
  cd backend
  go test ./internal/relay ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/relay ./internal/teamusage -run 'TeamTrendOrigin|TeamTrendCache|Cache|FlightGroup' -count=1
  go test ./... -count=1
  go vet ./...
  go build ./...
  cd ..
  git diff --check
  ```

  Expected: every command exits zero. No frontend build is required locally because no frontend source or HTTP contract changes; GitHub CI remains the complete repository gate.

- [ ] **Step 3: Review the exact branch diff**

  Run:

  ```bash
  git status --short --branch
  git diff --check feat/platform-loading-performance...HEAD
  git diff --stat feat/platform-loading-performance...HEAD
  git log --oneline feat/platform-loading-performance..HEAD
  ```

  Review every changed limiter, worker, cache lifetime, validation, test, spec, and architecture line for cancellation leaks, double capacity, stale-deadline regression, real-data leakage, unrelated refactors, and contradictions. Fix every Critical or Important finding and rerun affected verification.

- [ ] **Step 4: Record verification and commit documentation**

  Update this plan with exact focused, race, full-backend, and review evidence. Set the top Status to implementation complete and awaiting PR CI, without claiming staging or production verification.

  ```bash
  git add docs/architecture.md \
    docs/superpowers/specs/2026-07-19-team-usage-cold-loading-design.md \
    docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md
  git commit -m "docs(teamusage): record cold loading behavior"
  ```

### Task 4: Deliver The PR And Stop At The Merge Gate

**Files:**
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md`

**Interfaces:**
- Produces one non-Draft PR from `perf/team-usage-cold-loading` to `feat/platform-loading-performance`.
- Keeps staging and production unchanged until review and merge.

- [ ] **Step 1: Push and open the PR**

  Push the branch and open a non-Draft PR titled `perf(teamusage): reduce cold trend latency`. The body must include the 12.34-13.69 second baseline, 235/8 bottleneck, provider-wide sixteen-slot safety, 144-162 second freshness contract, exact test matrix, explicit no-Sub2API/no-frontend boundary, rollback by reverting either behavior commit, and staging acceptance target.

- [ ] **Step 2: Wait for exact-final-head CI**

  Require backend, frontend, ae-cli, and deploy-static success on the exact final head. Record the PR number, head SHA, run ID, and results in this plan; push a ledger-only commit if tracked content changes, then verify that final head as well.

- [ ] **Step 3: Stop at the merge gate**

  Do not merge automatically. Report the exact PR and CI state and wait for explicit user confirmation or observed merge before staging deployment.

### Task 5: Publish Staging And Run The 8/16 Comparison

**Files:**
- Modify after merge: `/Users/admin/helm/ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify after merge: `docs/superpowers/plans/2026-07-19-team-usage-cold-loading.md`

**Interfaces:**
- Produces one immutable `staging-<full-merge-commit>` multi-architecture image.
- Advances only `ai-efficiency-staging` in `la3-ai-efficiency-prod` through the existing two-phase restore rollout.
- Produces two comparable 16-slot cold/warm audits against the recorded 8-slot revision-28 baseline.

- [ ] **Step 1: Verify merge, publish, and inspect the exact image**

  Fast-forward `feat/platform-loading-performance`, build and push the exact commit for `linux/amd64,linux/arm64`, and verify its remote manifest digest and both platforms.

- [ ] **Step 2: Update only staging selectors and execute the two-phase rollout**

  Commit only the staging image tag and 12-character restore snapshot selector in `/Users/admin/helm`. Run server-side dry-runs, Phase A at zero replicas with restore disabled, confirm no application Pod, then Phase B with `--atomic --wait --wait-for-jobs --timeout 20m`.

- [ ] **Step 3: Repeat two cold/warm audits and enforce acceptance**

  Use the same 251/235 scope, range, granularity, timezone, four concurrent endpoints, limits, request IDs, and dependency-log aggregation as revision 28. Require both cold rounds at or below nine seconds, no more than 255 Relay calls, zero 429/5xx/transport/timeouts, warm at or below 1.5 seconds with zero Relay, and no Redis/cache error regression.

- [ ] **Step 4: Record evidence and verify production isolation**

  Record the exact image digest, staging revision, cold/warm timings, dependency counts/durations, cache/Redis counters, and acceptance result. Confirm production revision 68 and `v0.1.0-preview.72` remain ready and unchanged unless a later explicit production release supersedes that baseline. If the nine-second target fails, record the failure and stop without raising concurrency to 24.
