# Representative Scope Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace repeated full-directory representative-scope reconstruction with a versioned Redis read model while preserving authoritative token, actor-role, directory-run, and write authorization checks.

**Architecture:** Authentication continues to validate the JWT and `users.token_valid_after` before team-usage handlers run. `representativescope.Service` reads a lightweight guard containing the current actor row plus the latest successful Directory Sync apply snapshot, keys a reconstructible scope read model by deployment, actor, directory run, and current role, then re-reads the guard before returning so a concurrent role or directory change cannot expose the old scope. Redis and refresh-collapse failures bypass the optimization and rebuild from PostgreSQL; malformed cache entries are misses, and scope values are never served stale across a guard change.

**Tech Stack:** Go, Ent, PostgreSQL, go-redis v9, miniredis, Gin.

## Global Constraints

- Keep the platform a modular monolith and reuse `backend/internal/representativescope` as the scope interface.
- PostgreSQL remains authoritative; Redis is optional and must never become an availability dependency for authorization.
- Do not cache JWTs, credentials, passwords, API keys, token revocation state, or mutation decisions.
- Cache isolation includes deployment namespace, actor ID, current successful Directory Sync apply run ID, and current actor role.
- Representative scope has a 10-60 minute fresh-only lifetime with 10-20 percent TTL jitter and no stale reuse across a version change.
- Tests and examples use only synthetic users, domains, credentials, and group names.
- Every completed step updates this file in the same execution turn.

**Status:** Implementation and local verification complete. Baseline and final backend suites passed on the branch; commit, Draft PR, and required CI checks remain.

---

### Task 1: Expose the authoritative directory snapshot guard

**Files:**
- Modify: `backend/internal/directorysync/current.go`
- Modify: `backend/internal/directorysync/service_test.go`

**Interfaces:**
- Produces: `CurrentSnapshot(ctx context.Context, client *ent.Client) (CurrentDirectorySnapshot, bool, error)` where the snapshot contains `SourceID` and the globally unique successful apply `RunID`.
- Preserves: `CurrentSourceID(...)` as a compatibility projection over `CurrentSnapshot(...)`.

- [x] **Step 1: Add a failing test that requires the selected source and successful apply run ID**
- [x] **Step 2: Run `go test ./internal/directorysync -run 'TestCurrent(Snapshot|SourceID)' -count=1` and record the expected compile/test failure**
  - RED: `undefined: CurrentSnapshot`.
- [x] **Step 3: Implement the snapshot helper and keep current-source selection semantics unchanged**
- [x] **Step 4: Re-run the focused Directory Sync tests and confirm GREEN**
  - GREEN: current snapshot/source tests passed.

### Task 2: Add the shared cache store and cancellable flight primitives

**Files:**
- Create: `backend/internal/readcache/store.go`
- Create: `backend/internal/readcache/store_test.go`
- Create: `backend/internal/readcache/flight.go`
- Create: `backend/internal/readcache/flight_test.go`

**Interfaces:**
- Produces: `readcache.Store`, `readcache.RedisStore`, `readcache.ErrMiss`, `readcache.Sleep`, and generic `readcache.FlightGroup[T].Do(...)`.
- Compatibility: use the same interface and implementation already introduced independently by `perf/personal-usage-123`, so later integration does not create competing cache primitives.

- [x] **Step 1: Add store and flight tests for Redis values, token-protected leases, concurrent collapse, cancellation, and remaining waiters**
- [x] **Step 2: Run `go test ./internal/readcache -count=1` and record the expected missing-symbol/compile failure**
  - RED: `FlightGroup`, `NewRedisStore`, and `ErrMiss` were undefined.
- [x] **Step 3: Implement the minimal shared primitives**
- [x] **Step 4: Re-run `go test ./internal/readcache -count=1` and confirm GREEN**
  - GREEN: Redis store and cancellable flight tests passed.

### Task 3: Cache representative scope behind an authoritative guard

**Files:**
- Create: `backend/internal/representativescope/cache.go`
- Create: `backend/internal/representativescope/cache_test.go`
- Modify: `backend/internal/representativescope/service.go`
- Modify: `backend/internal/representativescope/service_test.go`

**Interfaces:**
- Produces: `NewCache(store readcache.Store, options CacheOptions) (*Cache, error)` and `NewWithCache(client *ent.Client, cache *Cache) *Service`.
- Internal guard: current actor ID/role plus current directory source/run; the service reads it before cache access and again before return.
- Cache behavior: strict schema envelope, exact guard binding, process-local flight, Redis lease, fresh-only jittered TTL, and authoritative fallback on every Redis error.

- [x] **Step 1: Add failing cache tests for cold/fresh reads, multiple actors/departments, run and role key changes, malformed values, concurrent collapse, TTL bounds, and Redis outage fallback**
- [x] **Step 2: Run the focused cache tests and record RED**
  - RED: `Cache`, `scopeGuard`, and `scopeCacheKey` were undefined.
- [x] **Step 3: Implement the cache and strict decode/validation path**
- [x] **Step 4: Add failing service tests proving a cache hit skips the full scope loader and a concurrent/new directory run or role change cannot reuse the old result**
  - RED: `NewWithCache` was undefined.
- [x] **Step 5: Refactor `Service.Resolve` into lightweight guard and authoritative scope-build paths, with a post-read guard check**
- [x] **Step 6: Run `go test ./internal/representativescope ./internal/teamusage -count=1` and confirm GREEN**
  - GREEN: representative scope and team usage module suites passed.
  - Scale coverage: a 500-member represented scope warm hit is required to execute only the two lightweight guards and no full directory table reads.
- [x] **Step 7: Run `go test -race ./internal/readcache ./internal/representativescope -count=1` and confirm GREEN**
  - GREEN: shared cache and representative scope race suites passed.

### Task 4: Wire the cache into the server without changing handler contracts

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/redis_runtime_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/team_usage.go`
- Modify: `backend/internal/handler/router_work_items_cache_test.go`

**Interfaces:**
- `handler.RouterRuntimeOptions` receives `RepresentativeScopeCache *representativescope.Cache`.
- `newTeamUsageService(...)` injects `representativescope.NewWithCache(...)`; routes and JSON contracts remain unchanged.

- [x] **Step 1: Add a failing router/runtime test proving the injected cache is used and Redis options remain bounded**
- [x] **Step 2: Run focused server/handler tests and record RED**
  - RED: `RouterRuntimeOptions.RepresentativeScopeCache` was undefined.
- [x] **Step 3: Construct one shared Redis store/client at startup and inject the representative-scope cache**
- [x] **Step 4: Re-run focused server/handler tests and confirm GREEN**
  - GREEN: bounded Redis options and router representative-scope injection tests passed.

### Task 5: Document, verify, review, and publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` only if implementation clarifies an active contract
- Modify: this plan

- [x] **Step 1: Update the current architecture with the authoritative guard, cache key, Redis fallback, and no-stale authorization semantics**
- [x] **Step 2: Run `gofmt` and `git diff --check`**
  - GREEN: all changed Go files are formatted and `git diff --check` is clean.
- [x] **Step 3: Run `cd backend && go test ./...`**
  - GREEN: the final backend suite passed after the 500-member scale test was added.
- [x] **Step 4: Run targeted race tests and `go vet` for changed backend packages**
  - GREEN: `go test -race ./internal/readcache ./internal/representativescope -count=1` and changed-package `go vet` passed.
- [x] **Step 5: Review the exact diff against issue #124 and the active performance spec**
  - Review result: no unresolved findings. Shared `readcache` files are byte-identical to PR #148, and the issue's guard, isolation, version, Redis outage, scale, and no-stale requirements are covered.
- [ ] **Step 6: Commit with Conventional Commits, push the branch, and open a Draft PR against `docs/performance-contracts-116`**
- [ ] **Step 7: Wait for all required PR checks and record their final state**

## Verification Notes

- `cd backend && go test ./...`: passed.
- `cd backend && go test -race ./internal/readcache ./internal/representativescope -count=1`: passed.
- `cd backend && go vet ./internal/readcache ./internal/representativescope ./internal/directorysync ./internal/teamusage ./internal/handler ./cmd/server`: passed.
- `cd ae-cli && go test ./...`: passed.
- `cd frontend && npm test`: 39/39 files and 451/451 tests passed.
- `cd frontend && npm run build`: passed.
- Role E2E: 16/16 passed against this worktree's Vite server on `127.0.0.1:5174`; port `5173` was owned by another worktree and was not reused.
