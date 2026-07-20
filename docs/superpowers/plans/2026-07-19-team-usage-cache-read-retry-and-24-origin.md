# Team Usage Cache Read Retry And 24-Origin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Task 5 execution is complete with a FAIL verdict pending final independent review. PR #190 merged as `55302b62c795054c56a700c6cb817eac06c49a5b`, whose tree matches reviewed head `e7ab9e4c2a8f25071adbc1cc1861961488e73ea2`; that exact merge commit is published as the immutable staging image and staging revision 32 is Ready. The first 24-slot cold audit failed the nine-second gate at `12.363568s`; its immediate warm audit passed at `1.222148s`, and the required fail-fast path stopped before cold round 2. The origin limit remains 24. Temporary audit state is deleted, the application and Helm worktrees are clean and remote-aligned, and production remains unchanged at revision 68 on `v0.1.0-preview.72`.

**Goal:** Recover one transient Redis read failure without leaving the bounded command budget, and reduce large-team cold latency by raising the single provider-wide trend origin limit from sixteen to 24.

**Architecture:** Add one immediate retry only inside `readcache.RedisStore.Get`; both attempts share the caller context, while miss, cancellation, writes, leases, and global go-redis retry settings remain unchanged. Change the existing Relay worker/limiter capacity through its shared constant so one caller can fill 24 slots but all callers and credential generations still share one provider maximum. Keep response schemas, cache identities, lifetimes, and frontend behavior unchanged.

**Tech Stack:** Go 1.24, go-redis v9.18, miniredis, `net/http`, `httptest`, existing `readcache.Store`, existing `teamTrendCache`, and existing `teamTrendOriginLimiter`.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance` on `perf/team-usage-cache-read-retry-24`.
- Base and PR target remain `feat/platform-loading-performance`; do not target `main`.
- Follow `docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md` as the current contract.
- Do not modify Sub2API, frontend source, HTTP DTOs, Redis keys, cache schemas, cache lifetimes, cursors, or provider interfaces.
- Redis GET performs at most two attempts in the original caller context, without sleep or backoff.
- Redis success and `redis.Nil` perform one attempt. Cancellation or expiry after the first error prevents a second attempt.
- Keep runtime `redis.Options.MaxRetries = -1`; do not retry SET, SETNX, PTTL, Lua, lease, publish, or mutation commands.
- The trend origin limit is exactly 24 and remains fixed, internal, provider-wide, and shared across callers and credential generations.
- The per-user trend cache remains 60 seconds, 4096 entries, and a 20-second flight timeout.
- Team Usage response freshness remains 144-162 seconds and stale lifetime remains 240-270 seconds.
- Do not add directory or batch-stat primitive caches in this change.
- Tests, examples, request IDs, users, credentials, keys, and responses use synthetic values only.
- Update each checkbox and its evidence in the same work round that performs the action; never pre-check CI, merge, deployment, or audit steps.
- Production deployment is out of scope and production must remain unchanged.

---

### Task 1: Retry One Failed Redis GET Inside The Existing Budget

**Files:**
- Modify: `backend/internal/readcache/store.go:35-41`
- Modify: `backend/internal/readcache/store_test.go`
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md`

**Interfaces:**
- Preserves `readcache.Store` and `NewRedisStore(redis.UniversalClient) *RedisStore`.
- Changes only `(*RedisStore).Get(context.Context, string) ([]byte, error)` to make at most two attempts.
- Preserves `ErrMiss`, the supplied context, and every non-GET store method.

- [x] **Step 1: Verify the clean backend baseline**

  Run:

  ```bash
  cd backend
  go mod download
  go test ./internal/readcache ./internal/relay ./internal/teamusage -count=1
  cd ..
  git status --short --branch
  ```

  Expected: all three packages pass and the only branch commit is the approved spec plus this plan. If the baseline fails, stop and diagnose before changing tests.

  Evidence (2026-07-19): `go mod download` and `go test ./internal/readcache ./internal/relay ./internal/teamusage -count=1` exited 0; all three packages passed. `git status --short --branch` reported only `## perf/team-usage-cache-read-retry-24`.

- [x] **Step 2: Add a deterministic go-redis command hook for retry tests**

  Extend `store_test.go` imports with `net` and `sync`, then add this test-only hook:

  ```go
  type scriptedRedisCommandHook struct {
      mu       sync.Mutex
      failures map[string][]error
      calls    map[string]int
      after    func(command string, attempt int)
  }

  func newScriptedRedisCommandHook(failures map[string][]error) *scriptedRedisCommandHook {
      return &scriptedRedisCommandHook{
          failures: failures,
          calls:    make(map[string]int),
      }
  }

  func (h *scriptedRedisCommandHook) DialHook(next redis.DialHook) redis.DialHook {
      return func(ctx context.Context, network, addr string) (net.Conn, error) {
          return next(ctx, network, addr)
      }
  }

  func (h *scriptedRedisCommandHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
      return func(ctx context.Context, cmd redis.Cmder) error {
          command := cmd.Name()
          h.mu.Lock()
          attempt := h.calls[command]
          h.calls[command] = attempt + 1
          var failure error
          if attempt < len(h.failures[command]) {
              failure = h.failures[command][attempt]
          }
          after := h.after
          h.mu.Unlock()
          if after != nil {
              after(command, attempt)
          }
          if failure != nil {
              return failure
          }
          return next(ctx, cmd)
      }
  }

  func (h *scriptedRedisCommandHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
      return func(ctx context.Context, cmds []redis.Cmder) error {
          return next(ctx, cmds)
      }
  }

  func (h *scriptedRedisCommandHook) callCount(command string) int {
      h.mu.Lock()
      defer h.mu.Unlock()
      return h.calls[command]
  }
  ```

  Keep the hook private to the test file. It must not inspect keys, values, or credentials.

  Evidence (2026-07-19): added the private mutex-protected `scriptedRedisCommandHook` in `store_test.go`; it scripts failures and counts command names without inspecting command arguments.

- [x] **Step 3: Add RED tests for success, miss, terminal error, and cancellation**

  Add tests using a fresh miniredis server and client per case:

  ```go
  func TestRedisStoreGetRetriesOneCommandError(t *testing.T) {
      server := miniredis.RunT(t)
      server.Set("value", "payload")
      firstErr := errors.New("synthetic first GET failure")
      hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
      client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
      client.AddHook(hook)
      t.Cleanup(func() { _ = client.Close() })

      value, err := NewRedisStore(client).Get(context.Background(), "value")
      if err != nil || string(value) != "payload" {
          t.Fatalf("Get() = %q, %v, want payload after one retry", value, err)
      }
      if got := hook.callCount("get"); got != 2 {
          t.Fatalf("GET attempts = %d, want 2", got)
      }
  }

  func TestRedisStoreGetRetryCanReturnMiss(t *testing.T) {
      server := miniredis.RunT(t)
      firstErr := errors.New("synthetic first GET failure")
      hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
      client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
      client.AddHook(hook)
      t.Cleanup(func() { _ = client.Close() })

      _, err := NewRedisStore(client).Get(context.Background(), "missing")
      if !errors.Is(err, ErrMiss) || hook.callCount("get") != 2 {
          t.Fatalf("Get(missing) error/attempts = %v/%d, want ErrMiss/2", err, hook.callCount("get"))
      }
  }

  func TestRedisStoreGetReturnsSecondCommandError(t *testing.T) {
      server := miniredis.RunT(t)
      firstErr := errors.New("synthetic first GET failure")
      secondErr := errors.New("synthetic second GET failure")
      hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr, secondErr}})
      client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
      client.AddHook(hook)
      t.Cleanup(func() { _ = client.Close() })

      _, err := NewRedisStore(client).Get(context.Background(), "value")
      if !errors.Is(err, secondErr) || hook.callCount("get") != 2 {
          t.Fatalf("Get() error/attempts = %v/%d, want second error/2", err, hook.callCount("get"))
      }
  }

  func TestRedisStoreGetDoesNotRetryAfterContextCancellation(t *testing.T) {
      server := miniredis.RunT(t)
      firstErr := errors.New("synthetic first GET failure")
      hook := newScriptedRedisCommandHook(map[string][]error{"get": {firstErr}})
      ctx, cancel := context.WithCancel(context.Background())
      hook.after = func(command string, attempt int) {
          if command == "get" && attempt == 0 {
              cancel()
          }
      }
      client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
      client.AddHook(hook)
      t.Cleanup(func() { _ = client.Close() })

      _, err := NewRedisStore(client).Get(ctx, "value")
      if !errors.Is(err, firstErr) || hook.callCount("get") != 1 {
          t.Fatalf("Get() error/attempts = %v/%d, want first error/1", err, hook.callCount("get"))
      }
  }
  ```

  Also add a pre-expired `context.WithDeadline` case that requires
  `context.DeadlineExceeded` and one GET attempt. Add a table proving an
  ordinary existing value and an ordinary missing key each execute one GET.
  Add a separate table with fresh clients whose hook returns a synthetic error
  for `set`, `pttl`, or `evalsha`; call `Set`, `TryAcquireLease`, `LeaseTTL`,
  and `ReleaseLease` and require exactly one matching command attempt.
  `TryAcquireLease` uses Redis command name `set`, so test it in a separate
  fresh case from `Set`.

  Evidence (2026-07-19): added fresh-client cases for retry-to-value, retry-to-miss, terminal second error, cancellation, pre-expired deadline, ordinary one-attempt hit/miss, and one-attempt `set`, `pttl`, and `evalsha` non-GET operations.

- [x] **Step 4: Run the focused tests and record RED**

  Run:

  ```bash
  cd backend
  gofmt -w internal/readcache/store_test.go
  go test ./internal/readcache -run 'RedisStoreGet|RedisStoreRetryIsGetOnly' -count=1 -v
  ```

  Expected: `TestRedisStoreGetRetriesOneCommandError`, the retry-to-miss case, and the terminal-second-error case fail because current code performs one GET. Ordinary hit/miss, cancellation, and non-GET characterization cases may pass. No fixture, hook, syntax, or miniredis failure is acceptable.

  RED evidence (2026-07-19): after `gofmt`, the focused command failed only the three expected retry cases; each observed one GET and the first synthetic error. Ordinary hit/miss, cancellation, pre-expired deadline, and all non-GET characterization cases passed.

- [x] **Step 5: Implement the minimal bounded GET loop**

  Replace only `RedisStore.Get`:

  ```go
  func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
      for attempt := 0; attempt < 2; attempt++ {
          value, err := s.client.Get(ctx, key).Bytes()
          if err == nil {
              return value, nil
          }
          if errors.Is(err, redis.Nil) {
              return nil, ErrMiss
          }
          if ctx.Err() != nil || attempt == 1 {
              return nil, err
          }
      }
      panic("unreachable")
  }
  ```

  Do not change `Store`, add options, reset the context, sleep, log, or touch another method.

  Evidence (2026-07-19): replaced only `RedisStore.Get` with the approved two-attempt loop; both attempts use the supplied context, and success, miss, cancellation, and terminal-error exits remain explicit.

- [x] **Step 6: Run GREEN, scope regressions, race checks, and commit**

  Run:

  ```bash
  cd backend
  gofmt -w internal/readcache/store.go internal/readcache/store_test.go
  go test ./internal/readcache -run 'RedisStore' -count=2
  go test ./internal/teamusage ./internal/personalusage ./internal/representativescope ./internal/relayruntime ./internal/repo ./internal/workitems -count=1
  go test -race ./internal/readcache ./internal/teamusage -run 'RedisStore|Cache|FlightGroup' -count=1
  go test ./cmd/server -run 'RedisClientOptions' -count=1
  cd ..
  git diff --check
  ```

  Expected: retry tests pass twice, all `readcache.RedisStore` consumers retain their contracts, race checks pass, and `TestRedisClientOptionsBoundReadCacheLatency` proves `MaxRetries = -1` remains unchanged.

  GREEN evidence (2026-07-19): the focused retry/non-GET suite passed after implementation. `go test ./internal/readcache -run 'RedisStore' -count=2`, the six listed consumer suites, the focused `-race` command, and `go test ./cmd/server -run 'RedisClientOptions' -count=1` all exited 0; `git diff --check` was clean.

  Record RED/GREEN evidence in this plan, then commit only the Redis behavior, tests, and current ledger update:

  ```bash
  git add backend/internal/readcache/store.go \
    backend/internal/readcache/store_test.go \
    docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md
  git commit -m "fix(readcache): retry one failed Redis get"
  ```

### Task 2: Raise The Single Provider-Wide Trend Bound To 24

**Files:**
- Modify: `backend/internal/relay/sub2api_team_trend_limiter.go:8`
- Modify: `backend/internal/relay/sub2api_team_trend_cache_test.go:243-500`
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md`

**Interfaces:**
- Changes `maxConcurrentTeamTrendOrigins` from `16` to `24`.
- Preserves `teamTrendOriginLimiter.Do` and `TeamMemberTrendProvider.GetUsageTrendForUsers`.
- Keeps one provider-owned limiter after cache/singleflight collapse.

- [x] **Step 1: Add a numeric RED assertion and remove sixteen from test names**

  Rename `TestTeamTrendOriginsUseSixteenProviderWideSlots` to
  `TestTeamTrendOriginsUseTwentyFourProviderWideSlots` and add this assertion
  before the server setup:

  ```go
  if maxConcurrentTeamTrendOrigins != 24 {
      t.Fatalf("maxConcurrentTeamTrendOrigins = %d, want 24", maxConcurrentTeamTrendOrigins)
  }
  ```

  Keep the current two disjoint 32-user callers. Its existing dynamic checks
  must then prove that the first caller fills 24 slots, the second caller does
  not create a second pool, all 64 origins complete, and `maxActive == 24`.
  Update failure wording in the credential-generation test from generic old
  wording to explicitly report the current constant, without changing its
  dynamic setup.

  Evidence (2026-07-19): renamed the provider-wide capacity test for 24 slots,
  added the exact numeric assertion before server setup, and made the
  credential-generation timeout report `maxConcurrentTeamTrendOrigins` while
  retaining its constant-driven setup.

- [x] **Step 2: Run the focused test and record RED**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/sub2api_team_trend_cache_test.go
  go test ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache.*(Credential|Shared|Concurrent)' -count=1 -v
  ```

  Expected: the renamed capacity test fails only with
  `maxConcurrentTeamTrendOrigins = 16, want 24`; cancellation,
  credential-generation, cache-hit, and shared-flight regressions remain
  green.

  RED evidence (2026-07-19): after `gofmt`, the focused command failed only
  `TestTeamTrendOriginsUseTwentyFourProviderWideSlots` with
  `maxConcurrentTeamTrendOrigins = 16, want 24`; all selected cancellation,
  credential-generation, and cache regressions passed.

- [x] **Step 3: Change the one shared capacity constant**

  In `sub2api_team_trend_limiter.go` change only:

  ```go
  const maxConcurrentTeamTrendOrigins = 24
  ```

  Do not add another limiter, change the HTTP transport maximum, or modify
  `SetAdminAPIKey`.

  Evidence (2026-07-19): changed only the shared
  `maxConcurrentTeamTrendOrigins` declaration from 16 to 24; limiter
  ownership and provider interfaces remain unchanged.

- [x] **Step 4: Run GREEN, high-contention regressions, race checks, and commit**

  Run:

  ```bash
  cd backend
  gofmt -w internal/relay/sub2api_team_trend_limiter.go internal/relay/sub2api_team_trend_cache_test.go
  go test ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache|UsageTrendForUsers|GetBatchUserUsageStats' -count=2
  go test ./internal/relay -run 'TeamTrendOriginsUseTwentyFourProviderWideSlots' -count=20
  go test -race ./internal/readcache ./internal/relay -run 'TeamTrendOrigin|TeamTrendCache|FlightGroup' -count=1
  cd ..
  git diff --check
  ```

  Expected: one caller fills 24, concurrent callers never exceed 24, all
  cancellation and credential-generation regressions pass, the saturation
  case passes 20 times, and the race detector reports no race.

  GREEN evidence (2026-07-19): the focused Relay regression suite passed
  twice, `TestTeamTrendOriginsUseTwentyFourProviderWideSlots` passed 20 times,
  and the focused `readcache`/Relay race command passed with no race reports;
  `git diff --check` was clean.

  Record RED/GREEN evidence in this plan, then commit:

  ```bash
  git add backend/internal/relay/sub2api_team_trend_limiter.go \
    backend/internal/relay/sub2api_team_trend_cache_test.go \
    docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md
  git commit -m "perf(relay): raise bounded team trend origins to 24"
  ```

### Task 3: Synchronize Current Documentation And Verify The Branch

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md`
- Modify: `docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md`

**Interfaces:**
- Records the bounded Redis GET retry as shared `readcache.RedisStore` behavior.
- Records 24 as the current provider-wide trend origin maximum.
- Preserves historical specs and documents this spec as their follow-up.

- [x] **Step 1: Update current architecture and spec status**

  In `docs/architecture.md`:

  - update the shared Redis/readcache description to say one failed idempotent
    GET receives one immediate retry inside the original command context,
    while misses, writes, and leases are not retried, and explicitly record
    that the runtime go-redis client retains `MaxRetries = -1` so library-level
    command retries stay disabled;
  - replace the current Team Usage provider-wide sixteen-slot description with
    24 slots, retaining the cache/singleflight, cancellation, credential, and
    HTTP-contract wording.

  In the new spec, set Status to implemented on the exact two behavior commits
  and awaiting full verification/PR CI. Do not rewrite either historical
  `2026-07-19` predecessor spec.

  Evidence (2026-07-19): updated the current `readcache` architecture contract
  with the one immediate failed-GET retry inside the original context and the
  unchanged miss/write/lease boundary. Follow-up review made the binding
  runtime constraint explicit: go-redis retains `MaxRetries = -1`, so it does
  not add implicit command retries. Updated both current Team Usage architecture
  descriptions from sixteen to 24 provider-wide origin slots. The follow-up
  spec now records behavior commits `bade4a33` and `26907c85` as implemented
  while full local verification and PR CI remain pending. The two predecessor
  specs were not modified.

- [x] **Step 2: Run the complete backend verification gate**

  Run fresh from `backend`:

  ```bash
  go test ./internal/readcache ./internal/relay ./internal/teamusage -count=2
  go test -race ./internal/readcache ./internal/relay ./internal/teamusage -run 'RedisStore|TeamTrendOrigin|TeamTrendCache|Cache|FlightGroup' -count=1
  go test ./... -count=1
  go vet ./...
  go build ./...
  cd ..
  git diff --check
  ```

  Expected: every command exits zero. No frontend build is required locally
  because frontend source and HTTP contracts are unchanged; GitHub CI remains
  the full repository gate.

  Evidence (2026-07-19): all commands were run fresh from `backend` and exited
  0. The `-count=2` readcache/relay/teamusage suites passed in `0.850s`,
  `18.233s`, and `26.090s`; the focused race suites passed in `1.845s`,
  `3.491s`, and `3.660s` with no race report. `go test ./... -count=1`
  passed every backend package, including `internal/handler` in `91.104s`,
  `internal/repo` in `58.549s`, and `internal/teamusage` in `36.535s`.
  `go vet ./...`, `go build ./...`, and the repository-root
  `git diff --check` all exited 0 with no output. No frontend command was run;
  repository PR CI remains pending.

- [x] **Step 3: Review the exact branch diff against the integration base**

  Run:

  ```bash
  git status --short --branch
  git diff --check feat/platform-loading-performance...HEAD
  git diff --stat feat/platform-loading-performance...HEAD
  git log --oneline feat/platform-loading-performance..HEAD
  ```

  Review every changed line against both Standards and Spec. In particular,
  reject any retry of `redis.Nil`, retry after context cancellation, new
  context or backoff, global `MaxRetries` change, non-GET retry, per-caller
  limiter, credential-generation double capacity, real-data fixture, frontend
  change, Sub2API change, or relaxed staging criterion. Fix every Critical or
  Important finding and rerun affected RED/GREEN plus full verification.

  Evidence (2026-07-19): reviewed the complete diff against
  `feat/platform-loading-performance` on behavior head `26907c85` and the
  current three-file documentation update. The required branch commands all
  exited 0; the committed diff contained six files with 1,065 insertions and
  seven deletions across commits `4ccd781f`, `565883db`, `bade4a33`, and
  `26907c85`. The current worktree diff contained only the expected seven files
  after adding `docs/architecture.md`.

  Standards review: no findings. Go changes stay inside the existing
  `readcache` and Relay boundaries, test hooks and values are synthetic, all
  commit subjects follow Conventional Commits, the plan was updated as a live
  ledger, and neither historical predecessor spec changed. There is no
  frontend or Sub2API source change and no real-data fixture.

  Initial spec review found no runtime behavior defect. `RedisStore.Get`
  returns success and `redis.Nil` without retry, stops after cancellation,
  reuses the supplied context with no sleep/backoff, and makes at most two GET
  attempts; runtime
  `redis.Options.MaxRetries` remains `-1`, and tests prove writes and leases
  make one matching command attempt. The only Relay behavior change is the
  shared constant from 16 to 24; the provider-owned limiter still runs after
  cache/flight collapse, remains shared across callers and credential
  generations, and preserves cancellation before HTTP origin start. Cache
  identities, TTLs, HTTP contracts, and the nine-second cold / 1.5-second warm
  staging criteria are unchanged.

  Follow-up review found one Important documentation omission: the current
  `Read-model coordination` architecture row described the immediate GET retry
  and unchanged miss/write/lease behavior without binding that behavior to the
  runtime go-redis `MaxRetries = -1` constraint already implemented in
  `backend/cmd/server/main.go` and specified by the follow-up design. Fixed the
  row to state that the runtime client retains `MaxRetries = -1`, so
  library-level command retries stay disabled. No historical spec or runtime
  code changed. The focused runtime-options test and `git diff --check` both
  exited 0 after the fix. Exact test output was
  `ok github.com/ai-efficiency/backend/cmd/server 1.034s`; `git diff --check`
  produced no output.

  Required post-fix verification (2026-07-19): reran the affected and complete
  Task 3 gate after the Important documentation fix. The two-count
  `readcache`, `relay`, and `teamusage` suites passed in `0.883s`, `18.298s`,
  and `25.930s`. The focused race suites passed in `1.589s`, `3.803s`, and
  `3.835s` with no race report. `go test ./... -count=1` passed every backend
  package, including `cmd/server` in `1.474s`, `internal/handler` in `91.717s`,
  `internal/repo` in `55.150s`, and `internal/teamusage` in `36.279s`.
  `go vet ./...`, `go build ./...`, and the repository-root
  `git diff --check` all exited 0 with no output. PR CI, delivery, merge, and
  staging verification remain pending.

- [x] **Step 4: Record verification and commit current documentation**

  Update this plan with exact test, race, vet, build, and review evidence. Set
  the spec and plan status to implementation complete and awaiting PR CI,
  without claiming merge or staging verification.

  ```bash
  git add docs/architecture.md \
    docs/superpowers/specs/2026-07-19-team-usage-cache-read-retry-and-24-origin-design.md \
    docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md
  git commit -m "docs(teamusage): record cache retry and 24-origin behavior"
  ```

  Evidence (2026-07-19): the current architecture, follow-up spec, and this
  live ledger record the implemented behavior and fresh local test, race, vet,
  build, diff, and two-axis self-review evidence. Their statuses state that
  implementation and full local verification are complete while PR CI is
  pending; no PR delivery, merge, image publish, Helm action, staging audit, or
  production action is claimed. Follow-up review evidence records the one
  Important architecture omission, its documentation-only fix, and the focused
  runtime-options regression plus the required post-fix affected, race, full
  backend, vet, build, and clean diff verification.

### Task 4: Deliver A Reviewed PR And Stop At The Merge Gate

**Files:**
- Modify as the execution ledger: `docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md`

**Interfaces:**
- Produces one non-Draft PR from `perf/team-usage-cache-read-retry-24` to `feat/platform-loading-performance`.
- Keeps staging and production unchanged until review, exact-head CI, and observed merge.

- [x] **Step 1: Push and open the follow-up PR**

  Push the branch and open a non-Draft PR titled
  `perf(teamusage): recover cache reads and raise trend concurrency` against
  `feat/platform-loading-performance`. The body must include:

  - revision-30 cold `13.47s/10.73s` and warm `7.69s/5.99s` evidence;
  - read-only single retry inside the original context and unchanged
    `MaxRetries = -1` boundary;
  - one provider-wide 24-slot bound with 50-connection transport headroom;
  - exact RED/GREEN, race, full backend, vet, and build evidence;
  - no Sub2API, frontend, DTO, key, TTL, lease, or production change;
  - independent rollback commits and the unchanged staging acceptance gate.

  Delivery evidence (2026-07-19): pushed
  `perf/team-usage-cache-read-retry-24` and opened non-Draft PR
  [#190](https://github.com/LichKing-2234/ai-efficiency/pull/190) with the
  exact title, base `feat/platform-loading-performance`, and initial head
  `24ac1b806440a37a971a9e0df0e4b13f1fc9b9ca`. The PR body records revision-30
  cold `13.47s/10.73s` and warm `7.69s/5.99s`, the bounded GET retry and
  unchanged `MaxRetries = -1`, one shared 24-slot provider limit with
  50-connection headroom, exact RED/GREEN/race/full-backend/vet/build
  evidence, unchanged scope boundaries, independent behavior commits
  `bade4a33` and `26907c85`, and the unchanged cold `9s` / warm `1.5s`
  staging gate.

- [x] **Step 2: Request code review and close actionable findings**

  Use the requesting-code-review workflow against merge-base
  `feat/platform-loading-performance`. Review Standards and Spec separately.
  Add RED regressions before any behavioral fix, rerun the complete affected
  ladder, and record findings without adding unrelated refactors. No subagent
  is required unless the user explicitly requests delegation.

  Review evidence (2026-07-19): independent whole-branch review covered exact
  merge-base range `0ab84076..24ac1b80` and reported Critical/Important/Minor
  `0/0/0`. Standards review found the changes confined to the established
  `readcache`, Relay, current architecture, and current follow-up-spec
  boundaries with synthetic tests and Conventional Commits. Spec review found
  the original-context two-attempt GET ceiling, miss/cancellation/non-GET
  behavior, runtime `MaxRetries = -1`, provider-wide 24-slot ownership,
  credential sharing, transport headroom, cache/DTO/TTL/lease boundaries, and
  staging gate all conformant. The verdict was ready subject to exact-head CI;
  no actionable finding or behavioral follow-up remained.

- [x] **Step 3: Require exact-final-head repository CI**

  Require backend, frontend, ae-cli, and deploy-static success on the exact
  reviewed PR head. If a plan-only ledger commit records an earlier CI run,
  require the same four jobs again on that final ledger head. Do not treat a
  superseded run as sufficient.

  Reviewed-head CI evidence (2026-07-19): CI run `29688885139` completed
  successfully on exact reviewed PR head
  `24ac1b806440a37a971a9e0df0e4b13f1fc9b9ca`: backend job `88197968943`,
  frontend job `88197968948`, ae-cli job `88197968938`, and deploy-static job
  `88197968930` all succeeded. After completion, the live PR head still matched
  the reviewed head and PR #190 was OPEN, non-Draft, MERGEABLE/CLEAN against
  `feat/platform-loading-performance`. The following plan-only ledger commit
  becomes the final PR head and must receive the same four green jobs before
  Task 4 is reported complete externally; this evidence does not pre-claim
  that final-ledger result.

  Ledger-timing correction (2026-07-19): plan-ledger commit
  `653bb5b3cab0abafff55401efb299bd974e9c7bf` was committed at
  `2026-07-19T13:33:27Z` with Task 4 Steps 3 and 4 already checked, while its
  CI run `29689122313` was not created until `2026-07-19T13:33:53Z`. Although
  that run later completed successfully, the committed ledger pre-checked
  exact-head CI and violated this plan's ordering rule. Steps 3 and 4 are
  reopened. This corrective plan commit must be pushed as the new exact PR
  head, and backend, frontend, ae-cli, and deploy-static must all be observed
  successful on that unchanged head before either step is checked again.

  Corrective-head CI evidence (2026-07-19): after the corrective commit was
  pushed, CI run `29689664751` completed successfully on exact PR head
  `e7ab9e4c2a8f25071adbc1cc1861961488e73ea2`: backend job `88200039500`,
  frontend job `88200039495`, ae-cli job `88200039482`, and deploy-static job
  `88200039485` all succeeded. Local HEAD, the remote branch, and the live PR
  head still matched that exact SHA after completion; the head was not changed
  while the run was pending. This evidence is recorded only in the intentional
  uncommitted live-ledger carry-forward, so it does not invalidate the verified
  PR head.

- [x] **Step 4: Stop at the merge gate**

  Do not merge automatically. Report PR URL, base/head branches, reviewed head
  SHA, mergeability, review state, and exact-head CI. Wait for user merge or
  observe the merge before any image publish or Helm action.

  Final merge-gate evidence (2026-07-19): after exact-head run `29689664751`
  completed, PR #190 remained OPEN and non-Draft with base
  `feat/platform-loading-performance`, head
  `perf/team-usage-cache-read-retry-24`, and exact head
  `e7ab9e4c2a8f25071adbc1cc1861961488e73ea2`. GitHub reported
  MERGEABLE/CLEAN. `reviewDecision` remained empty with no formal GitHub review;
  the required independent Standards and Spec review remained complete and
  clean. No merge, image publish, release, Helm, staging, or production command
  was run. Task 4 is stopped at the merge gate pending user merge or an
  observed merge; Task 5 and staging remain pending.

  **Live-ledger carry-forward (intentionally uncommitted):** this final status,
  SHA, run, job, and mergeability evidence stays only in the working tree. A
  further commit would create an unverified PR head and repeat the timing defect
  corrected by `e7ab9e4c`.

### Task 5: Publish Staging And Re-run The 16/24 Comparison

**Files:**
- Modify after merge: `/Users/admin/helm/ai-efficiency/.secrets/ai-efficiency-staging-upgrade.json`
- Modify after audit: `docs/superpowers/plans/2026-07-19-team-usage-cache-read-retry-and-24-origin.md`

**Interfaces:**
- Produces immutable image `ghcr.io/lichking-2234/ai-efficiency:staging-<full-merge-commit>` for `linux/amd64` and `linux/arm64`.
- Advances only `ai-efficiency-staging` in `la3-ai-efficiency-prod` through the existing two-phase restore rollout.
- Produces two comparable 24-slot cold/warm audits against revision 30.

- [x] **Step 1: Verify merge and publish the exact multi-architecture image**

  Verify the PR is merged into `feat/platform-loading-performance`, the merge
  tree matches the reviewed PR tree, and the temporary PR branch is eligible
  for cleanup. Switch this worktree back to the integration branch, fetch, and
  fast-forward it to the observed remote merge commit before deriving the image
  tag:

  ```bash
  git fetch --prune origin
  git switch feat/platform-loading-performance
  git merge --ff-only origin/feat/platform-loading-performance
  ```

  Delete only the merged local/remote PR branch after proving the merge tree;
  preserve this integration worktree for staging. Use the existing staging
  builder only after confirming both required platforms:

  ```bash
  APP_WORKTREE=/Users/admin/ai-efficiency/.worktrees/feat-platform-loading-performance
  COMMIT="$(git -C "${APP_WORKTREE}" rev-parse HEAD)"
  IMAGE_TAG="staging-${COMMIT}"
  IMAGE="ghcr.io/lichking-2234/ai-efficiency:${IMAGE_TAG}"
  BUILDER=static-spaces-release-builder

  BUILDER_PLATFORMS="$(docker buildx inspect "${BUILDER}" --bootstrap | awk -F': ' '/^Platforms:/ {print $2}')"
  grep -q 'linux/amd64' <<<"${BUILDER_PLATFORMS}"
  grep -q 'linux/arm64' <<<"${BUILDER_PLATFORMS}"

  docker buildx build \
    --builder "${BUILDER}" \
    --platform linux/amd64,linux/arm64 \
    --file "${APP_WORKTREE}/deploy/Dockerfile" \
    --tag "${IMAGE}" \
    --build-arg APP_VERSION="staging-${COMMIT:0:7}" \
    --build-arg APP_COMMIT="${COMMIT}" \
    --build-arg APP_BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --push "${APP_WORKTREE}"

  docker buildx imagetools inspect "${IMAGE}"
  ```

  Record the exact manifest digest and both platform manifests. Do not publish
  a platform tag or GitHub Release.

  Evidence (2026-07-20): GitHub reported PR #190 MERGED at
  `55302b62c795054c56a700c6cb817eac06c49a5b`; both that merge commit and
  reviewed PR head `e7ab9e4c2a8f25071adbc1cc1861961488e73ea2` resolve to tree
  `2e4d02c4b3d8b2ec1a85745552052557862eaf06`. The integration worktree was
  fast-forwarded to the exact merge commit. Stash
  `2f0c1176ec68703d3a47a242ddcb9578de3ab1c8` applied cleanly and changed only
  this plan, then only that stash and the merged local/remote PR branch were
  removed; the two unrelated user stashes remain. Builder
  `static-spaces-release-builder` advertised `linux/amd64` and `linux/arm64`
  and pushed
  `ghcr.io/lichking-2234/ai-efficiency:staging-55302b62c795054c56a700c6cb817eac06c49a5b`.
  Remote inspection returned index digest
  `sha256:e51c69cd476bceb580e088e309f266f24e951ecda124a46610c30ccac75c6aa4`,
  amd64 manifest
  `sha256:ff4163101be8006778ac4117c0b3d025c04476aa577f858dc72a04278d6da3e2`,
  and arm64 manifest
  `sha256:5c8b9f4b7d1b13404d3cd1f5e5266394073d7062d23dd1122884331fa4372103`.
  No platform tag or GitHub Release was created.

- [x] **Step 2: Update only staging selectors and execute the two-phase rollout**

  In `/Users/admin/helm`, update only `.image.tag` and the 12-character restore
  snapshot selector without printing secrets, verify the metadata-only diff,
  and commit with `chore(ai-efficiency): publish staging <short-commit>`.

  Run the Phase A and Phase B commands from
  `/Users/admin/helm/docs/staging-playbook.md` exactly:

  - Phase A: server-side hidden-secret dry-run, `replicaCount=0`, restore
    disabled, atomic wait, then confirm the old application Pod is absent.
  - Phase B: server-side hidden-secret dry-run, followed by
    `--atomic --wait --wait-for-jobs --timeout 20m` with normal staging values.

  Verify the restore Job succeeds `1/1`, PostgreSQL StatefulSet and application
  Deployment are Ready, staging reports the exact merge commit, and database,
  Redis, and Relay checks are `up`. Verify production is still Ready on the
  pre-audit revision and image before proceeding.

  Evidence (2026-07-20): Helm commit
  `3f09c5d5dd9bd39fdfdfa806774a8435a912fc9e` updated only `.image.tag` to
  `staging-55302b62c795054c56a700c6cb817eac06c49a5b` and the 12-character
  restore selector to `55302b62c795`; replacing those two paths with fixed
  placeholders left the before/after JSON at the same normalized SHA-256
  `327c868eab3c6891366845e34dc97cc1b7fe78d24f7a4a07c1ab9a750bb30dd8`.
  The commit was pushed to `origin/main` without exposing secret values.
  Phase A server dry-run passed, revision 31 deployed with zero application
  replicas, and the old application Pod was observed deleted. Phase B server
  dry-run passed and the atomic restore rollout deployed revision 32. Restore
  Job `ai-efficiency-staging-postgres-restore-55302b62c795` completed `1/1`
  with zero failures; PostgreSQL was `1/1` Ready and the application Deployment
  was `1/1` Ready on the exact staging image. Ready and live health reported
  commit `55302b62c795054c56a700c6cb817eac06c49a5b`; database, Redis, and Relay
  were all `up`. Production remained Ready at revision 68 on
  `ghcr.io/lichking-2234/ai-efficiency:v0.1.0-preview.72`, with commit
  `1d3a8c6cbf755774860ff44c8dd466d1115f3890` and all three checks `up`.

- [x] **Step 3: Run two exact cold/warm audits**

  Use a session-only temporary audit script under `/tmp`; never write the
  supplied account, password, SSO token, cookie, or response bodies into the
  repository. Against
  `https://ai-efficiency-staging.la3.agoralab.co`, request these four lanes
  concurrently for `2026-06-20..2026-07-19`, `day`, and `Asia/Shanghai`:

  ```text
  /api/v1/user/team-usage/summary
  /api/v1/user/team-usage/trend
  /api/v1/user/team-usage/members?limit=50
  /api/v1/user/team-usage/organization?department_limit=25&member_limit=50
  ```

  Do not delete keys or flush the shared Redis database. After rollout, wait
  300 seconds without issuing a Team Usage request for the audit actor. This
  exceeds the 270-second maximum response hard TTL and the unchanged 60-second
  primitive trend TTL, so cold round 1 cannot reuse a prior value. Capture
  pre/post cache and Redis counters, use unique bounded request IDs, and
  aggregate only sanitized status class, duration, response bytes,
  cache/source status, 251/235 counts, Relay call count/status/duration, Redis
  timeout/wait/stale-connection deltas, and Team Usage cache outcome deltas.
  Run warm round 1 immediately after cold round 1. Then wait another 300 seconds
  without Team Usage traffic for that actor before cold round 2, followed
  immediately by warm round 2.

  Acceptance requires both cold rounds at or below nine seconds, no more than
  255 Relay calls per cold round, zero Relay 429/5xx/transport/timeouts, both
  warm rounds at or below 1.5 seconds with zero Relay calls, unchanged Team
  Usage cache-error/Redis-timeout/wait/lease-failure counters, HTTP 200, and
  preserved 251/235 response counts. If any condition fails, record it and
  stop without increasing beyond 24 or weakening the contract.

  Evidence (2026-07-20): after a measured 300-second quiet window beginning at
  `2026-07-20T01:42:41Z`, cold round 1 issued the four required lanes
  concurrently against revision 32 and completed in `12.363568s`, which failed
  the `<=9s` gate. Summary, trend, members, and organization completed in
  `11.580609s`, `12.363568s`, `11.887330s`, and `11.563641s`; all returned HTTP
  200 with matching bounded request IDs, `cache=miss`, `source=ok`, and the
  preserved 251-member / 235-Relay-member scope. The cold round made exactly
  255 Relay calls, all 2xx, with `101.907s` aggregate dependency duration and
  zero 4xx, 5xx, transport errors, or timeouts. Each response cache recorded
  `miss +1`, `refresh +1`, and `lease_acquired +1`, with zero error, stale,
  lease-wait, or lease-failure delta; Redis wait, timeout, and stale-connection
  deltas were also zero.

  Immediate warm round 1 completed in `1.222148s`; its four lane durations
  were `0.751889s`, `1.222148s`, `0.951021s`, and `0.896057s`. Every response
  was HTTP 200 with `cache=fresh`, `source=ok`, matching request IDs, and the
  same 251/235 scope. The warm round made zero Relay calls, each response cache
  recorded `fresh +1`, and all cache-error, lease-failure, and Redis pool
  deltas remained zero. Because cold round 1 failed the contract, the second
  300-second wait was interrupted before cold round 2; cold round 2 and warm
  round 2 were not requested. This is the required fail-fast path, not a
  successful two-round acceptance result. The provider-wide origin limit
  remains 24 and must not be raised further.

- [x] **Step 4: Record evidence, verify isolation, and clean temporary state**

  Record image digest, Helm revisions, restore result, health, cold/warm
  timings, sanitized dependency/counter deltas, and the acceptance verdict in
  this plan. Verify `ai-efficiency-prod` remains Ready on the same revision and
  image as before the staging rollout. Delete the temporary audit script and
  session artifacts, confirm the application and Helm worktrees are clean and
  aligned with their remotes, and preserve unrelated main-checkout changes.

  Evidence (2026-07-20): remote image inspection reconfirmed index digest
  `sha256:e51c69cd476bceb580e088e309f266f24e951ecda124a46610c30ccac75c6aa4`
  with the recorded amd64 and arm64 manifests. After the audit evidence commit,
  the application worktree was clean and local/remote-aligned at `6cdce15b`;
  the Helm worktree was clean and local/remote-aligned at
  `3f09c5d5dd9bd39fdfdfa806774a8435a912fc9e`. Staging remained deployed at
  revision 32 with the exact `staging-55302b62...` image, application and
  PostgreSQL `1/1` Ready, restore Job `1/1`, and database, Redis, and Relay
  checks `up`. Production remained deployed at revision 68 with
  `v0.1.0-preview.72`, application `1/1` Ready, commit `1d3a8c6c...`, and all
  three dependency checks `up`.

  The session-only token, audit script, and sanitized temporary artifact were
  deleted and independently confirmed absent. No Redis key was deleted or
  flushed, no response body or credential was persisted, and no Team Usage
  request was sent after the failed cold round and its immediate warm
  measurement. The main application checkout still contains only its unrelated
  pre-existing `CLAUDE.md` modification and untracked `docs/agents/` directory;
  neither was touched. The final acceptance verdict is FAIL solely because
  cold round 1 exceeded nine seconds; the observed warm, Relay, response-scope,
  cache-error, lease-failure, and Redis-pool guards all passed.
