# Work Items Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Task 1 is complete. Task 2 is next; this branch is stacked on `docs/performance-contracts-116`.

**Goal:** Make the protected-navigation work-item badge and administrator offboarding list fast and bounded while preserving authoritative quota, credential, Directory Sync, Relay disable, and token-revocation behavior.

**Architecture:** Keep candidate ownership in `directorysync`, where a shared SQL anti-join powers both a count query and a stable paginated page query. Wrap the existing `workitems.Service` authoritative calculation in a workitems-owned Redis read model keyed by deployment namespace, PostgreSQL revision, actor, and effective role; use process singleflight plus a token-protected distributed lease, and make every relevant mutation advance the persisted revision before returning success. Keep browser freshness in the existing Pinia store and make the offboarding view consume the bounded page contract.

**Tech Stack:** Go 1.23+, Gin, Ent/PostgreSQL, go-redis v9, `golang.org/x/sync/singleflight`, Vue 3, Pinia, TypeScript, Vitest.

## Global Constraints

- Work-item counts have a fresh window of 20-30 seconds, no stale window, and 10-20 percent TTL jitter that never exceeds 30 seconds.
- Cache isolation includes an explicit deployment namespace, persisted revision, actor ID, and effective role; cache keys and values are never logged.
- Redis failure bypasses Redis cache and lease behavior and falls back to an authoritative load under a bounded context.
- Identical cold requests collapse in one process and across replicas; distributed lease release uses token compare-and-delete and cancellation cannot deadlock waiters.
- Production defaults use a 100 ms Redis command budget, a 15-second authoritative refresh budget, a 20-second lease TTL, and 25 ms waiter polling; tests inject shorter deterministic durations.
- Mutation invalidation is PostgreSQL-backed so a Redis outage cannot resurrect an old value after Redis recovers.
- Work-item badge reads never materialize offboarding candidates; offboarding count and page use the same anti-join predicate.
- Offboarding pages default to 20 rows, accept at most 100, and order by username then user ID.
- Exact normalized-email confirmation, current full-company source resolution, current membership recheck, Relay disable, and token revocation remain authoritative and uncached.
- The quota approver GIN index must match `resolved_approver_user_ids::jsonb @> ...` using `jsonb_path_ops`.
- Five protected-route navigations and repeated mobile-sidebar mounts issue at most one browser request in a 20-second client freshness window.
- Tests, fixtures, examples, cache values, and documentation use synthetic identities only.
- Update `docs/architecture.md` only for behavior that actually lands in this slice.

---

### Task 1: Bounded Offboarding Query And Approver Index

**Files:**
- Modify: `backend/internal/directorysync/service.go`
- Modify: `backend/internal/directorysync/service_test.go`
- Modify: `backend/internal/handler/directory.go`
- Modify: `backend/internal/handler/directory_test.go`
- Modify: `backend/internal/workitems/service.go`
- Modify: `backend/internal/workitems/service_test.go`
- Modify: `backend/ent/schema/quota_reset_request.go`
- Modify generated migration metadata under `backend/ent/migrate/`

**Interfaces:**
- Produces: `directorysync.OffboardingCandidateListParams{SourceID, Query, Page, PageSize}`.
- Produces: `directorysync.OffboardingCandidatePage{Items, Page, PageSize, Total}`.
- Produces: `(*directorysync.Service).ListOffboardingCandidates(context.Context, OffboardingCandidateListParams) (*OffboardingCandidatePage, error)`.
- Produces: `(*directorysync.Service).CountOffboardingCandidates(context.Context, int) (int, error)`.
- Produces: a narrow workitems `offboardingCounter` dependency so `Counts` calls `CountOffboardingCandidates` and never constructs or lists a directory service.

- [x] **Step 1: Write failing directorysync tests for a shared bounded contract**

  Add PostgreSQL-backed tests with at least 500 relay-bound synthetic users. Assert that count and the union of pages agree; present directory members and succeeded disable actions are excluded; failed and partial actions remain; default and maximum page sizes are enforced; ties use username then ID; and page action metadata is batch-populated. Use Ent query logging to prove query count stays constant when the fixture grows and add service-level cases proving a mismatched confirmation email and a newly re-added directory member cause no Relay or revocation side effect.

- [x] **Step 2: Run the focused directorysync tests and record the expected RED result**

  Run: `cd backend && go test ./internal/directorysync -run 'Offboarding|CurrentSource' -count=1`

  Expected: compilation or assertion failures because the page/count interfaces and bounded SQL behavior do not exist.

- [x] **Step 3: Implement the shared anti-join, stable page, and count-only query**

  Resolve the requested or current successful full-company snapshot once. Build a shared Ent user predicate with `NOT EXISTS` probes against `directory_members(source_id,email_normalized)` and succeeded `directory_offboarding_actions(source_id,user_id,action,status)`. Count with `COUNT(*)`; list only one bounded page ordered by `username ASC, id ASC`; load action rows for page user IDs in one query; and preserve the existing candidate DTO and current-source semantics.

- [x] **Step 4: Add failing handler and workitems tests for the new contract**

  Assert that `GET /api/v1/admin/directory/offboarding-candidates?page=2&page_size=25&q=bob` returns `items`, `page`, `page_size`, and `total`, rejects non-positive numeric values, and clamps page size to 100. Assert that an admin badge calls only the injected counter and never a list method.

- [x] **Step 5: Run handler and workitems tests and record the expected RED result**

  Run: `cd backend && go test ./internal/handler ./internal/workitems -run 'DirectoryHandlerListOffboarding|Counts.*Offboarding' -count=1`

  Expected: failures because the handler still returns only `{items}` and workitems still materializes the full list.

- [x] **Step 6: Wire the page DTO and count dependency**

  Parse page parameters in the handler, return the page DTO through the existing success envelope, and inject the already-created `directorysync.Service` into `workitems.Service` as `offboardingCounter`. Do not create a directorysync service inside the count path.

- [x] **Step 7: Add the matching JSONB GIN index test, then generate and implement it**

  First add a PostgreSQL test that inspects `pg_indexes` and uses `EXPLAIN` with sequential scans disabled to prove the `@>` predicate can select a `USING gin (... jsonb_path_ops)` index. Run it and observe RED, then add the Ent index annotation to `resolved_approver_user_ids`, run `cd backend && go generate ./ent`, and rerun the plan test.

- [x] **Step 8: Run focused Task 1 verification**

  Run: `cd backend && go test ./internal/directorysync ./internal/handler ./internal/workitems -count=1`

  Expected: PASS with constant-query scale assertions and the new page/count contracts.

- [x] **Step 9: Commit Task 1 and update this ledger immediately**

  Commit: `perf(backend): bound offboarding candidate reads`

---

### Task 2: Work-Item Redis Read Model And Runtime Wiring

**Files:**
- Create: `backend/internal/workitems/cache.go`
- Create: `backend/internal/workitems/cache_test.go`
- Create: `backend/internal/workitems/revision.go`
- Create: `backend/internal/workitems/revision_test.go`
- Modify: `backend/internal/workitems/service.go`
- Modify: `backend/internal/workitems/service_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/config/writable_config.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/.env.example`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.bootstrap.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: `deploy/docker-compose.external.yml`
- Modify: `deploy/docker-compose.local.yml`

**Interfaces:**
- Produces: `workitems.CountsCache.GetOrLoad(ctx, actorID, effectiveRole, loader)` with a versioned value envelope.
- Produces: `workitems.CountsInvalidator.InvalidateWorkItemCounts(ctx) error` and transaction-aware invalidation for local PostgreSQL mutations.
- Produces: `redis.namespace` / `AE_REDIS_NAMESPACE` as the explicit deployment namespace.
- Consumes: Task 1's `offboardingCounter` for the authoritative loader.

- [ ] **Step 1: Write failing cache concurrency, isolation, and outage tests**

  With a deterministic fake Redis store and clock, cover: 50 concurrent same-key calls invoking one loader; two cache instances invoking one loader through the distributed lease; actor, role, deployment, and revision isolation; TTLs always between 20 and 30 seconds; malformed/schema-mismatched values causing refresh; Redis read/lease/write errors falling back to authoritative data; cancelled waiters returning promptly; lease-holder timeout recovery; and token-checked release that cannot delete another holder's lease.

- [ ] **Step 2: Run cache tests and record the expected RED result**

  Run: `cd backend && go test ./internal/workitems -run 'CountsCache|Revision' -count=1`

  Expected: compilation failures because the cache, store adapter, lease, and revision types do not exist.

- [ ] **Step 3: Implement the cache and PostgreSQL UUID revision**

  Store revision key `work_items_counts_revision_v1` in `system_settings` and generate a new UUID for every invalidation. Build keys as `ae:<namespace>:work-items:counts:v1:rev:<revision>:actor:<id>:role:<role>`. Use `singleflight.Group.DoChan`, a bounded refresh context independent from a cancelled first waiter, a short Redis command budget, `SET NX` lease acquisition, bounded result polling, token compare-and-delete Lua release, revision recheck before cache write, JSON schema validation, and authoritative fallback on any Redis failure. Do not return stale values.

- [ ] **Step 4: Add failing service and configuration tests**

  Assert that warm `Service.Counts` reuses a cached response, role and actor changes miss, a revision change refreshes immediately, and 50 concurrent cold calls produce one authoritative calculation. Assert `AE_REDIS_NAMESPACE` loads and writable/example config serialization retains it.

- [ ] **Step 5: Run service/configuration tests and record the expected RED result**

  Run: `cd backend && go test ./internal/workitems ./internal/config -count=1`

  Expected: failures because service and runtime configuration are not wired to the cache.

- [ ] **Step 6: Wire the existing Redis client and cached service**

  Construct one store/cache/invalidator from the existing `*redis.Client` in `main.go`, pass the same directory service from Task 1 into the authoritative workitems service, and inject the cached service into the router. Set go-redis context timeout behavior and bounded command settings so an unavailable Redis cannot add multi-second badge latency. Keep readiness degradation independent from data-plane fallback.

- [ ] **Step 7: Run focused Task 2 verification**

  Run: `cd backend && go test ./internal/workitems ./internal/config ./internal/handler ./cmd/server -count=1`

  Expected: PASS, including concurrency, fallback, and runtime wiring tests.

- [ ] **Step 8: Commit Task 2 and update this ledger immediately**

  Commit: `perf(backend): cache work item counts across replicas`

---

### Task 3: Authoritative Backend Mutation Invalidation

**Files:**
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/service_test.go`
- Modify: `backend/internal/directorysync/service.go`
- Modify: `backend/internal/directorysync/service_test.go`
- Modify: `backend/internal/usersetup/service.go`
- Modify: `backend/internal/usersetup/service_test.go`
- Modify: `backend/internal/handler/provider.go`
- Modify: `backend/internal/handler/provider_test.go`
- Modify: `backend/internal/handler/admin_users.go`
- Modify relevant admin subscription job files and tests if the affected add/remove path runs asynchronously.
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: Task 2's concrete invalidator through package-local interfaces named `InvalidateWorkItemCounts(context.Context) error` and `InvalidateWorkItemCountsTx(context.Context, *ent.Tx) error`; mutation packages must not import `workitems` solely to name the interface.
- Produces: successful quota, credential, provider, Directory apply, offboarding, and access-group mutations that advance the persisted revision before returning success.

- [ ] **Step 1: Add failing mutation invalidation tests**

  Use a recording invalidator to assert exactly one invalidation after each state-changing success and zero invalidations after validation/conflict/upstream failures. Cover quota create, cancel, approve, reject, retry/final reset status; successful Directory apply; successful offboarding disable; group credential creation; provider create/update/delete; and subscription add/remove paths that change available access groups.

- [ ] **Step 2: Run mutation tests and record the expected RED result**

  Run: `cd backend && go test ./internal/quotareset ./internal/directorysync ./internal/usersetup ./internal/handler -run 'Invalidat|Apply|Offboarding|Credential|Provider|Subscription' -count=1`

  Expected: failures because these services do not accept or invoke the invalidator.

- [ ] **Step 3: Inject local invalidator interfaces and invalidate before success**

  Keep each service's dependency narrow. For mutations already committed in a local Ent transaction, advance the UUID revision in that transaction where feasible. For Relay-backed writes, advance it synchronously after the authoritative write and before returning success. Preserve the current quota decision checks, exact-email confirmation, current membership recheck, Relay disable, and token revocation; none may consult cached counts.

- [ ] **Step 4: Add and pass an immediate post-mutation integration test**

  Warm an actor's work-item cache, execute each representative mutation family, then call counts and assert the new authoritative value is visible immediately while the old Redis key remains unreachable by revision. Include a Redis-outage-during-invalidation case followed by Redis recovery to prove no old value resurrects.

- [ ] **Step 5: Run focused Task 3 verification**

  Run: `cd backend && go test ./internal/quotareset ./internal/directorysync ./internal/usersetup ./internal/workitems ./internal/handler -count=1`

  Expected: PASS with no mutation path using cache state for authorization or decisions.

- [ ] **Step 6: Commit Task 3 and update this ledger immediately**

  Commit: `perf(backend): invalidate work item counts on mutations`

---

### Task 4: Frontend Freshness, Paginated Offboarding, And Mutation Refresh

**Files:**
- Modify: `frontend/src/stores/workItems.ts`
- Modify: `frontend/src/__tests__/work-items-store.test.ts`
- Modify: `frontend/src/__tests__/app-layout.test.ts`
- Modify: `frontend/src/api/directory.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/views/admin/DirectoryOffboardingView.vue`
- Modify: `frontend/src/__tests__/directory-offboarding-view.test.ts`
- Modify: `frontend/src/views/QuotaResetView.vue`
- Modify: `frontend/src/__tests__/quota-reset-view.test.ts`
- Modify: `frontend/src/views/UserView.vue`
- Modify its existing focused tests.
- Modify Directory Sync settings/admin subscription views only where a successful mutation needs current-actor badge invalidation.

**Interfaces:**
- Produces: `workItems.loadCounts({force?})` with a 20-second success freshness window, one queued forced follow-up, and generation-safe response handling.
- Produces: `workItems.invalidateCounts()` that preserves the displayed badge but expires freshness and prevents a pre-invalidation response from writing back.
- Consumes: Task 1's `{items,page,page_size,total}` offboarding response.

- [ ] **Step 1: Write failing store freshness and race tests**

  With fake timers and deferred responses, assert: repeated loads through 19,999 ms make one request; 20,000 ms makes a second; force bypasses freshness; invalidation causes a normal refresh; many force callers during one inflight request queue exactly one forced follow-up; a pre-invalidation response is ignored; a post-invalidation response wins; and auth reset clears values, freshness, queued force state, and generations.

- [ ] **Step 2: Run store tests and record the expected RED result**

  Run: `cd frontend && npm test -- src/__tests__/work-items-store.test.ts`

  Expected: failures because only inflight deduplication exists.

- [ ] **Step 3: Implement generation-safe 20-second Pinia freshness**

  Start freshness at successful response completion. Return immediately while fresh unless forced; share one active promise per generation; share one queued forced follow-up; compare both request identity and session/freshness generation before writing or clearing state; never mark errors fresh; and keep the existing badge value during invalidation.

- [ ] **Step 4: Add failing protected-navigation and mobile-remount integration tests**

  Mount a real `RouterView` with five distinct view component identities, each containing the real `AppLayout`, and navigate across all five with one Pinia instance; assert one counts call in the freshness window and a second after expiry. Separately resolve the desktop sidebar request, open/close mobile navigation at least five times, and assert the same request bound before and after expiry.

- [ ] **Step 5: Run layout tests and record the expected RED result**

  Run: `cd frontend && npm test -- src/__tests__/app-layout.test.ts`

  Expected: repeated completed sidebar mounts issue repeated requests before the store implementation is applied.

- [ ] **Step 6: Add failing paginated offboarding and mutation-refresh tests**

  Assert the offboarding view requests page 1 with page size 20, renders total-aware next/previous controls, resets to page 1 on search, and after a successful disable awaits both the page reload and a forced work-item refresh. Add focused tests that successful quota and credential mutations invalidate then await a badge refresh without allowing an older inflight response to win.

- [ ] **Step 7: Implement the page contract and current-actor mutation refreshes**

  Update API/type boundaries, add stable pager state without changing the existing exact-email confirmation UX, and call `invalidateCounts()` followed by an awaited `loadCounts({force:true})` after affected mutations. Do not trigger an extra request merely by mounting a settings section with an already-completed historical run.

- [ ] **Step 8: Run focused Task 4 verification**

  Run: `cd frontend && npm test -- src/__tests__/work-items-store.test.ts src/__tests__/app-layout.test.ts src/__tests__/directory-offboarding-view.test.ts src/__tests__/quota-reset-view.test.ts`

  Expected: PASS with one request per freshness window and immediate post-mutation refresh.

- [ ] **Step 9: Commit Task 4 and update this ledger immediately**

  Commit: `perf(frontend): bound work item badge refreshes`

---

### Task 5: Architecture, Review, And Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan as each verification checkbox is completed.

**Interfaces:**
- Consumes: Tasks 1-4 behavior and test evidence.
- Produces: current architecture documentation, independent review evidence, a pushed stacked branch, and a draft PR targeting `docs/performance-contracts-116` until PR #138 merges.

- [ ] **Step 1: Update current architecture documentation**

  Document the landed workitems-owned Redis read model, PostgreSQL revision invalidation boundary, shared directorysync count/page anti-join, deployment namespace, and Pinia freshness owner. Do not describe unrelated future performance tickets as implemented.

- [ ] **Step 2: Run formatting and focused race verification**

  Run Go formatting on changed Go files, then run: `cd backend && go test -race ./internal/workitems ./internal/directorysync ./internal/quotareset ./internal/usersetup`

  Expected: PASS with no data race in singleflight, lease, revision, or mutation wiring.

- [ ] **Step 3: Run full repository verification**

  Run separately:

  - `cd backend && go test ./...`
  - `cd ae-cli && go test ./...`
  - `cd frontend && npm test`
  - start Vite, then `cd frontend && npm run test:e2e:role`

  Expected: all suites pass; environment-sensitive role E2E evidence is reported separately.

- [ ] **Step 4: Perform independent spec and code-quality reviews**

  Generate a full branch diff package from base `5f6c58e6821dfcd95eefff14ea3426d454ae86cd`. Ask independent reviewers to verify every #119 acceptance criterion and repository standards. Resolve every Critical or Important finding, rerun covering tests, and re-review until clean.

- [ ] **Step 5: Commit documentation and review fixes**

  Commit: `docs(architecture): document work item read model`

- [ ] **Step 6: Push and open the stacked draft PR**

  Push `perf/work-items-119` and create a draft PR whose base is `docs/performance-contracts-116`, body links `#119`, states the stacked dependency on PR #138, and reports exact test evidence. Confirm the PR head/base/state and CI checks from GitHub after creation.
