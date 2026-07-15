# Repository Inventory Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans and superpowers:test-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** In progress. Issue #132 is isolated on `perf/repos-132`; clean baseline verification passed for backend `go test ./...` and frontend 39 files / 451 tests.

**Goal:** Render repository list/detail core content without browser waterfalls while making inventory work bounded, deployment-isolated, and immediately versioned after repository mutations.

**Architecture:** Keep repository ownership in `backend/internal/repo`. Replace full-row inventory materialization with one dialect-aware SQL aggregate that returns provider/scope groups, expose a stable server-selected list scope when the browser has no explicit selection, and wrap only the aggregate inventory read model in a fresh-only Redis cache keyed by deployment namespace plus a PostgreSQL UUID revision. The Vue list starts its list and inventory requests concurrently, the detail route treats provider options as supplemental, and each repository/PR record owns one responsive DOM subtree.

**Tech Stack:** Go 1.23+, Ent/PostgreSQL, go-redis v9, miniredis, Gin, Vue 3, Pinia, TypeScript, Vitest.

## Global Constraints

- Inventory is authoritative SQL data first; Redis contains only a reconstructible aggregate read model.
- Cache keys are `ae:<namespace>:repos:inventory:v1:rev:<uuid>` and contain no credentials, raw query strings, repo names, user data, or cached values.
- Fresh TTL is 48-54 seconds, representing a 60-second maximum minus 10-20 percent jitter; there is no stale window.
- Redis command failure bypasses cache/lease behavior and performs one bounded SQL aggregate load.
- Identical cold reads collapse in-process and across replicas with a token-protected Redis lease; cancellation cannot leave an unbounded refresh or durable lock.
- Repository create, metadata update, delete, provider bind/unbind, auto-bind metadata/status writes, and webhook repair status/metadata writes advance the PostgreSQL inventory revision in the same transaction as the local repository mutation.
- Failed/no-op external SCM work that does not change a local repo row does not advance the revision.
- Default list selection prefers a bound GitHub provider, then bound Bitbucket Server, then other bound providers, ordered by provider name and ID; scope ties are alphabetical. Unbound is the fallback only when no bound scope exists.
- An explicit provider/scope/binding query remains authoritative and is never replaced by the default.
- Frontend provider-option failures cannot redirect, hide, or replace repository/PR core content.
- List and detail pages render one repository/PR row subtree per API item at every viewport.
- Tests, fixtures, cache payloads, and docs use synthetic identities and domains only.

---

### Task 1: SQL Aggregate Inventory And Stable List Selection

**Files:**
- Create: `backend/internal/repo/inventory.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/handler/repo.go`
- Modify: `backend/internal/handler/repo_inventory_test.go`

**Interfaces:**
- Produces: `repo.ListSelection{ProviderKey, ProviderID, ProviderName, ProviderType, Scope, BindingState}`.
- Produces: `repo.ListPage{Items, Total, Page, PageSize, Selection}`.
- Produces: `(*repo.Service).ListPage(context.Context, repo.ListOpts) (*repo.ListPage, error)` while retaining `List` as the existing compatibility wrapper.
- Produces: `(*repo.Service).loadInventory(context.Context) ([]repo.InventoryProviderSummary, error)` as the authoritative aggregate loader consumed by Task 2.

- [x] **Step 1: Write failing aggregate and scale tests**

  Add repository tests with at least 1,000 synthetic repos spread across two providers, three scopes, bound/unbound state, and all statuses. Capture Ent SQL logs and assert inventory executes one grouped query, contains `GROUP BY`, returns only provider/scope aggregate groups, preserves duplicate provider names by provider ID, sorts scopes deterministically, and produces the current totals without loading `RepoConfig` entities.

- [x] **Step 2: Run aggregate tests and record RED**

  Run: `cd backend && go test ./internal/repo -run 'Inventory.*(Aggregate|Scale|Duplicate)' -count=1`

  Expected RED: current `Inventory` selects every repository with `WithScmProvider` and aggregates in Go.

  RED evidence: the 1,000-row scale test observed two full-entity/edge queries and no `GROUP BY` instead of one aggregate query.

- [x] **Step 3: Implement the dialect-aware aggregate loader**

  Use one `RepoConfig.Query().Modify(func(*sql.Selector))` projection with a left join to `scm_providers`, a dialect-specific scope expression, and portable `SUM(CASE WHEN ... THEN 1 ELSE 0 END)` counters. Scan only rows shaped like:

  ```go
  type inventoryAggregateRow struct {
      ProviderID         *int   `sql:"provider_id"`
      ProviderName       string `sql:"provider_name"`
      ProviderType       string `sql:"provider_type"`
      ProviderBaseURL    string `sql:"provider_base_url"`
      Scope              string `sql:"scope"`
      TotalRepos         int    `sql:"total_repos"`
      BoundRepos         int    `sql:"bound_repos"`
      UnboundRepos       int    `sql:"unbound_repos"`
      ActiveRepos        int    `sql:"active_repos"`
      WebhookFailedRepos int    `sql:"webhook_failed_repos"`
  }
  ```

  Fold those bounded rows into the existing provider/scopes DTO and preserve `provider_key=scm_provider:<id>` plus `unbound` semantics.

  GREEN evidence: the focused inventory suite passes with one dialect-aware Ent aggregate query for 1,000 repositories, bounded provider/scope rows, deterministic sorting, duplicate provider-name separation by ID, and unchanged totals/unbound behavior.

- [x] **Step 4: Write failing stable-default handler tests**

  Assert an unfiltered first-page request returns `selection` plus only the deterministic default provider/scope rows, explicit `scm_provider_id`, `scope`, or `binding_state` inputs remain unchanged, all-unbound data selects `unbound`, and empty inventory returns an empty page with no selection. Keep page defaults and response fields backward compatible.

- [x] **Step 5: Run stable-default tests and record RED**

  Run: `cd backend && go test ./internal/repo ./internal/handler -run 'Repo(ListPage|ListEndpoint).*Default|RepoListEndpointFilters' -count=1`

  Expected RED: the current unfiltered endpoint returns all repos and has no selected provider/scope contract.

  RED evidence: repository tests fail to compile because `ListPage` is absent, while handler tests return all three unfiltered repositories with no `selection` and therefore fail the deterministic bound/unbound defaults.

- [x] **Step 6: Implement `ListPage` and handler response**

  Resolve a default from the bounded inventory result only when no explicit provider/scope/binding input exists, apply it to the existing paginated query, and return:

  ```json
  {
    "items": [],
    "total": 0,
    "page": 1,
    "page_size": 20,
    "selection": {
      "provider_key": "scm_provider:7",
      "provider_id": 7,
      "provider_name": "GitHub",
      "provider_type": "github",
      "scope": "org",
      "binding_state": "bound"
    }
  }
  ```

  Keep `Service.List` delegating to `ListPage` and returning its legacy tuple for internal callers.

- [x] **Step 7: Run Task 1 verification and commit**

  Run: `cd backend && go test ./internal/repo ./internal/handler -count=1`

  Verification: focused stable-default tests pass; full `internal/repo` and `internal/handler` packages pass. The task commit records the SQL aggregate, additive selection contract, compatibility wrapper, tests, and live plan evidence.

  Commit: `perf(repo): aggregate inventory and select a stable default`

---

### Task 2: Versioned Redis Inventory Read Model And Mutation Invalidation

**Files:**
- Create: `backend/internal/repo/inventory_cache.go`
- Create: `backend/internal/repo/inventory_cache_test.go`
- Create: `backend/internal/repo/inventory_revision.go`
- Create: `backend/internal/repo/inventory_revision_test.go`
- Modify: `backend/internal/repo/service.go`
- Modify: `backend/internal/repo/auto_bind.go`
- Modify: `backend/internal/repo/webhook_repair.go`
- Modify: `backend/internal/repo/repo_test.go`
- Modify: `backend/internal/repo/auto_bind_test.go`
- Modify: `backend/internal/repo/webhook_repair_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/cmd/server/main_test.go`

**Interfaces:**
- Produces: `repo.InventoryRevisionStore.Ensure`, `Current`, and `InvalidateTx` using system-setting key `repo_inventory_revision_v1`.
- Produces: `repo.InventoryCache.GetOrLoad(ctx, loader)` with a schema-versioned JSON envelope and fresh-only semantics.
- Extends: `repo.ServiceOptions{InventoryCache, InventoryRevisionStore}`.
- Consumes: Task 1's `loadInventory` as the only authoritative cache loader.

- [x] **Step 1: Write failing cache and revision tests**

  Cover fresh hit, miss, 48/54-second jitter endpoints, namespace/revision isolation, malformed/schema-mismatched JSON, Redis GET/SET/lease/release failure, 50 same-process callers, two cache instances sharing a distributed lease, lease expiry recovery, one cancelled waiter while another succeeds, final-waiter cancellation, and token-checked release. Revision tests cover concurrent `Ensure`, malformed/missing values, transaction rollback, and one UUID change per successful invalidation.

- [x] **Step 2: Run cache/revision tests and record RED**

  Run: `cd backend && go test ./internal/repo -run 'Inventory(Cache|Revision)' -count=1`

  Expected RED: cache, lease, revision, and Redis adapter types do not exist.

  RED evidence: the focused package build fails on the intentionally absent `InventoryCache`, store/reader/options contracts, `ErrInventoryCacheMiss`, Redis adapter, and revision store APIs.

- [x] **Step 3: Implement cache and revision store**

  Mirror the proven work-items freshness mechanics without importing its business DTOs: strict JSON decode, UUID revision validation before read and before write, process-local waiter-counted flight, Redis `SET NX PX` lease, bounded polling, independent token-checked release, 100 ms command/release budgets, 15-second refresh budget, and no stale fallback.

  GREEN evidence: focused cache/revision tests pass for strict envelopes, 48-54 second fresh TTL, namespace/revision isolation, Redis failure fallback, 50 same-process callers, distributed lease contention/expiry, revision changes, token-checked release, and waiter cancellation.

- [x] **Step 4: Write failing mutation invalidation tests**

  Inject a real revision store and assert create/direct-create, remote create/metadata refresh, update, delete, auto-bind provider assignment/post-bind status, and webhook repair status/metadata writes make the old cache key unreachable immediately. Inject revision-update failure and assert the paired local repo mutation rolls back and does not report success. Assert Redis outage never blocks those mutations.

- [x] **Step 5: Run mutation tests and record RED**

  Run: `cd backend && go test ./internal/repo -run 'Inventory.*(Create|Update|Delete|Bind|Webhook|Mutation|Rollback)' -count=1`

  Expected RED: current repository writes do not own an inventory revision and are not transactionally coupled to invalidation.

  RED evidence: create/metadata/update/delete, auto-bind, webhook success/failure, and Redis-outage mutation tests all observe an unchanged PostgreSQL revision; an injected revision failure still lets `CreateDirect` succeed instead of rolling the row back.

- [x] **Step 6: Transactionally version every repository mutation**

  Add one service helper that begins an Ent transaction, executes a repository-row mutation through the transaction, calls `InventoryRevisionStore.InvalidateTx`, then commits. Apply it to every local row write listed in Global Constraints; keep SCM verification/webhook network calls outside the local transaction. Existing no-op paths return without bumping the revision.

  GREEN evidence: focused mutation tests and the full `internal/repo` suite pass. Create/direct/remote metadata, update, delete, auto-bind assignment/post-bind, and webhook repair writes advance the UUID; injected revision failures roll local writes back, while Redis outage does not enter or block mutation transactions.

- [x] **Step 7: Wire the existing Redis client in server startup**

  After migrations, `Ensure` the repository inventory revision. Construct `InventoryCache` from the existing `*redis.Client` and `redis.namespace`, inject both cache and revision store into the existing `repo.NewService`, and keep readiness semantics unchanged because authoritative SQL remains the fallback.

- [x] **Step 8: Run Task 2 verification and commit**

  Run: `cd backend && go test ./internal/repo ./internal/handler ./cmd/server -count=1`

  Run: `cd backend && go test -race ./internal/repo -count=1`

  Verification: repo/handler/server packages pass, the server wiring test initializes one canonical revision and writes the namespaced Redis value, and `go test -race ./internal/repo -count=1` passes.

  Commit: `perf(repo): cache versioned inventory across replicas`

---

### Task 3: Browser Waterfall Removal And Single Responsive Rows

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/repo.ts`
- Modify: `frontend/src/stores/repo.ts`
- Modify: `frontend/src/views/repos/RepoListView.vue`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-store.test.ts`
- Modify: `frontend/src/__tests__/repo-list-view.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

**Interfaces:**
- Produces: `RepoListSelection` and `RepoListResponse extends PagedResponse<RepoConfig>`.
- Extends: repo store with `selection`, `loaded`, and inventory-specific error state.
- Consumes: Task 1's additive list `selection` response.

- [x] **Step 1: Write failing list-route waterfall tests**

  Delay inventory while resolving the list and assert default rows render before inventory, both requests start on mount, route query provider/scope is sent immediately without inventory, inventory failure leaves list rows available, and provider options are not requested until the add dialog opens. Assert initial inventory completion does not trigger a duplicate list request.

- [x] **Step 2: Write failing detail-route/provider tests**

  Delay and reject `listProviders` for an admin while resolving repo/PR/latest-job calls. Assert repository health and PR rows render immediately, no redirect occurs, and binding controls become available only when provider options resolve.

- [x] **Step 3: Write failing one-subtree render tests**

  For N repository items assert exactly N `[data-testid="repo-row"]` nodes and no separate mobile/desktop `v-for` copies. For N PR items assert exactly N `[data-testid="repo-pr-row"]` nodes, one details command per PR, and expansion renders one detail subtree.

- [x] **Step 4: Run frontend focused tests and record RED**

  Run: `cd frontend && npm test -- repo-store.test.ts repo-list-view.test.ts repo-detail-view.test.ts api-modules.test.ts`

  Expected RED: list waits for inventory, detail global loading waits for providers, response selection types are absent, and both views mount duplicate responsive row trees.

  RED evidence: 10 focused tests fail. The list request has not started while inventory is pending, rows disappear on inventory failure, detail remains on the global loading state while provider options are pending, store state is missing, and repo/PR row test IDs find no single owned subtree.

- [x] **Step 5: Implement list/store concurrency and stable selection hydration**

  Start inventory and list requests together. Build immediate list params from URL query (`scm_provider:<id>`, `unbound`, scope, binding, page), apply the server selection when no explicit selection exists, render list state independently from inventory state, and use later inventory only to populate health/platform/scope controls. Keep mutation workbench refresh authoritative and prevent inventory errors from overwriting list errors or rows.

- [x] **Step 6: Isolate detail provider-option loading**

  Start provider options concurrently for admins but remove it from the awaited core `Promise.all`. Track provider-option loading/error separately; `loading` ends when repo/PR/latest-job core calls finish.

- [x] **Step 7: Replace duplicate responsive row trees**

  Replace each mobile-card plus desktop-table pair with one responsive grid/article per item. Use CSS grid tracks and breakpoint-specific labels/visibility inside the same row; preserve navigation, delete, PR status/usage, details expansion, pagination, and keyboard behavior.

- [x] **Step 8: Run Task 3 verification and commit**

  Run: `cd frontend && npm test -- repo-store.test.ts repo-list-view.test.ts repo-detail-view.test.ts api-modules.test.ts`

  Run: `cd frontend && npm test`

  Run: `cd frontend && npm run build`

  Verification: the four focused files pass 103 tests; the full frontend suite passes 39 files / 459 tests; `vue-tsc -b` and the Vite production build pass. List/inventory and core/provider requests are independent, and repo/PR items now own one responsive subtree each.

  Commit: `perf(frontend): render repository core data without waterfalls`

---

### Task 4: Current Architecture, Review, And Delivery

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [ ] **Step 1: Document only landed current behavior**

  Record SQL aggregate inventory, versioned deployment-isolated Redis freshness, transactional repo mutation revisioning, stable default list selection, independent inventory/provider loading, and single responsive row ownership. Keep the 2026-07-14 performance spec as the approved target/history contract.

- [ ] **Step 2: Run repository verification**

  Run: `cd backend && go test ./...`

  Run: `cd backend && go vet ./...`

  Run: `cd ae-cli && go test ./...`

  Run: `cd frontend && npm test`

  Run: `cd frontend && npm run build`

  Run: `cd frontend && npm run test:e2e:role` with the Vite prerequisite running.

  Run: `git diff --check && git status --short`

- [ ] **Step 3: Complete independent code review**

  Review from base `7f2999a` against issue #132, this plan, current architecture, and the 2026-07-14 performance contract. Fix every critical/important finding with focused RED/GREEN evidence and rerun affected full suites.

- [ ] **Step 4: Publish the draft PR**

  Push `perf/repos-132`, open a draft PR targeting `docs/performance-contracts-116`, include `Closes #132`, verification, dependency, Redis fallback, and no-release notes, wait for CI, and keep this worktree alive for review.
