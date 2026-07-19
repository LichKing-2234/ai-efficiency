# Admin Users Hierarchy Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Complete. PR #185 was squash-merged into `feat/platform-loading-performance` at `a717e37df2a039d71b7aefabc80596a44ff648b9`; issue #171 is closed, exact PR head `8650d2034f9b1b1a69d9b8be00ad75e98c392dbe` passed the required CI, and the exact integration head `d2bc2694` includes #172. No `main` merge, release, or production verification is claimed.

**Goal:** Remove administrator request-time hierarchy compatibility reconstruction and positional-placeholder coupling while preserving all persisted effective-hierarchy behavior.

**Architecture:** Administrator hierarchy queries will read `directory_departments.effective_parent_external_id` directly and compose SQL with Ent's builder so every runtime value receives an explicit bound argument. The legacy complete `/admin/users/departments` response will move behind one `adminusers.Service` method with a constant query-role shape, persisted-parent presentation, and the same shared summary aggregation as bounded navigation; handler-owned `directorytree` and member aggregation will be deleted.

**Tech Stack:** Go 1.24, Ent, PostgreSQL recursive CTEs, Gin, Vue 3, TypeScript, Vitest.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/admin-users-hierarchy-cleanup-171` on `refactor/admin-users-hierarchy-cleanup-171`.
- Preserve effective-cycle, orphan, multi-membership, representative, list, mutation-scope, complete-route, and query-plan behavior.
- No administrator hierarchy request may reconstruct cycle repairs from raw parents or depend on rewriting/inference of an earlier `$n` placeholder.
- The complete compatibility route must use a constant query-role count as department cardinality grows; do not introduce per-parent queries.
- Use only synthetic identities and domains in tests and examples.
- Update each checkbox immediately after its action actually completes.
- Do not push, open a PR, merge, tag, release, deploy, or run Helm.

---

### Task 1: Lock The Contract With RED Tests

**Files:**
- Modify: `backend/internal/adminusers/query_plan_test.go`
- Modify: `backend/internal/adminusers/department_test.go`
- Modify: `backend/internal/handler/admin_users_test.go`

**Interfaces:**
- `(*adminusers.Service).Departments(context.Context) ([]DepartmentSummary, error)` returns the complete current-source response in persisted-parent preorder.
- Captured hierarchy SQL contains no `navigation_departments` compatibility CTE and binds each positional placeholder explicitly rather than reusing or inferring a prior position.
- Complete-route SQL role count remains constant across small and large fixtures.

- [x] **Step 1: Add generated-SQL cleanup and constant-role tests**

  Capture target/list, options, children, presentation, summary, and complete-route queries. Assert no compatibility source projection or source-wide cycle reconstruction remains, each positional placeholder maps to one explicit argument occurrence, recursive subtree/ancestor/descendant roles still loop once, and complete-route query count is scale-independent.

- [x] **Step 2: Add complete-route persisted-parent parity coverage**

  Seed synthetic raw parents that conflict with stored effective parents, then require `/api/v1/admin/users/departments` to return persisted root/child/leaf preorder, display paths, depth, subtree counts, representatives, and no duplicate members.

- [x] **Step 3: Run focused RED**

  ```bash
  cd backend
  go test ./internal/adminusers ./internal/handler -run 'HierarchyCleanup|CompleteDepartments|PersistedEffective' -count=1
  ```

  Expected: fail because `Service.Departments` does not exist, generated queries still contain `navigation_departments` and reused/inferred placeholders, and the compatibility handler still follows raw parents through `directorytree`.

  RED evidence (2026-07-19): `adminusers` failed to compile because `Service.Departments` is absent. The handler regression independently failed because the complete route returned `dept-persisted-child` as a root with `Persisted Child`, proving that it still followed the conflicting raw parent facts instead of the stored root/child/leaf relation.

### Task 2: Remove SQL Compatibility Coupling

**Files:**
- Modify: `backend/internal/adminusers/department.go`
- Modify: `backend/internal/adminusers/query_plan_test.go`

**Interfaces:**
- Query builders write source, department, candidate, and summary CTEs directly with `builder.Arg` for every use.
- Generated SQL reads `directory_departments.effective_parent_external_id` and retains only requested subtree, candidate-ancestor, and summary-descendant recursion.
- Delete `effectiveDepartmentCTEs`, `effectiveDepartmentParameter`, `effectiveSubtreeParameter`, `previousPostgresPlaceholder`, and tests that assert their exact string prefix.

- [x] **Step 1: Replace formatted-parameter SQL injection with explicit builder composition**

  Make target/list/current-filter, page enrichment, options, children, and summaries source-scope the persisted table directly. Bind repeated source values explicitly and keep arrays as one PostgreSQL array argument.

- [x] **Step 2: Run focused GREEN for SQL and behavioral parity**

  ```bash
  cd backend
  gofmt -w internal/adminusers/department.go internal/adminusers/query_plan_test.go
  go test ./internal/adminusers ./internal/adminsubscription ./internal/handler -run 'HierarchyCleanup|Department|List|Targets|CurrentFilter|Subscription' -count=1
  ```

  Confirm cycle/orphan/multi-membership/list/current-filter/query-plan suites pass before moving the complete route.

  GREEN evidence (2026-07-19): `go test ./internal/adminusers ./internal/adminsubscription ./internal/handler -count=1` passed after the migration (`adminusers` 14.874s, `adminsubscription` 8.483s, `handler` 69.684s). Production admin-user code contains no `FormatParam`, `previousPostgresPlaceholder`, or `navigation_departments` reference.

### Task 3: Move The Complete Route Behind Admin Users

**Files:**
- Modify: `backend/internal/adminusers/department.go`
- Modify: `backend/internal/adminusers/department_test.go`
- Modify: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/admin_users_test.go`

**Interfaces:**
- `Service.Departments` resolves the current source, reads the persisted-parent hierarchy in one SQL role, and obtains all summary aggregates in one SQL role.
- `AdminUsersHandler.ListDepartments` maps service rows only; it no longer imports `directorytree`, loads all members, or owns hierarchy/member/representative algorithms.

- [x] **Step 1: Implement the constant-role complete department reader**

  Use one persisted-parent hierarchy query with deterministic depth-first ordering and one shared summary query. Keep raw `parent_external_id` in the response while deriving order, depth, child relations, and display paths only from `effective_parent_external_id`.

- [x] **Step 2: Delete obsolete handler compatibility algorithms**

  Remove handler-owned membership unions, representative metadata aggregation, `directorytree` traversal, and tests tied only to those private helpers. Preserve HTTP JSON fields and empty-current-source behavior.

- [x] **Step 3: Run complete-route GREEN and query-plan verification**

  ```bash
  cd backend
  gofmt -w internal/adminusers/department.go internal/adminusers/department_test.go internal/handler/admin_users.go internal/handler/admin_users_test.go
  go test ./internal/adminusers ./internal/handler -run 'HierarchyCleanup|CompleteDepartments|DepartmentSummaries|DepartmentChildren|DepartmentOptions' -count=2
  ```

  GREEN evidence (2026-07-19): the exact count-two command passed (`adminusers` 8.867s, `handler` 4.587s), including persisted-parent complete-route parity, ICU/C external-id tie ordering, constant query roles across 12/120 departments, cycle/orphan navigation, representative summaries, and bounded options/children.

### Task 4: Synchronize Current Contracts And Verify

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: this plan

**Interfaces:**
- Current architecture and Directory Sync contract state that apply owns effective hierarchy materialization and every administrator reader consumes it without raw-parent repair.
- The performance contract no longer promises a transitional shared prefix or future #171 cleanup.

- [x] **Step 1: Update architecture and active contracts**

  Describe direct persisted-parent reads, explicit SQL binding, the adminusers-owned complete compatibility route, and the existing post-storage successful-apply deployment requirement.

  Documentation evidence (2026-07-19): current architecture, the Directory Sync contract, and the active performance contract now identify apply as the sole hierarchy-repair owner, describe direct persisted-parent readers with explicit builder arguments, and place the constant-role complete compatibility response and shared summaries in `backend/internal/adminusers` rather than the HTTP handler.

- [x] **Step 2: Run full required verification**

  ```bash
  git diff --check
  cd backend
  go test ./... -count=1
  go vet ./...
  cd ../frontend
  npm test
  npm run build
  npm run test:e2e:role
  cd ../ae-cli
  go test ./... -count=1
  cd ..
  bash deploy/test/release-frontend-embed-test.sh
  ```

  Record environment-sensitive role E2E separately when its required runtime is unavailable; do not mark this step complete unless every non-environment-sensitive command passes.

  Verification evidence (2026-07-19): `git diff --check`; backend `go test ./... -count=1` and `go vet ./...`; frontend `npm test` (46 files / 680 tests) and `npm run build`; ae-cli `go test ./... -count=1`; and `bash deploy/test/release-frontend-embed-test.sh` passed. The first frontend test attempt stopped before tests because this fresh worktree had no installed `vitest`; `npm ci` restored the lockfile dependencies and the fresh rerun passed. The first role E2E attempt confirmed its documented `ERR_CONNECTION_REFUSED` prerequisite; after starting this worktree's strict-port Vite listener on `127.0.0.1:5173`, the suite passed 16/16. The owned listener was stopped and port 5173 was confirmed free. Expected proxy warnings for non-asserted APIs appeared because no backend dev server was intentionally started. The embed script emitted its known synthetic unresolved-hook warning before passing. There are no remaining environment-sensitive verification gaps.

- [x] **Step 3: Review exact diff and acceptance criteria**

  Confirm no Critical or Important self-review findings remain, no production identifier entered fixtures/docs, query-role counts stay constant, no deleted helper remains referenced, and `git diff --check` is clean.

  Review evidence (2026-07-19): the first pass identified one collation-sensitive full-preorder tie key and recursive-summary mismatch when `external_id` uses ICU collation. A synthetic equal-name `Zulu`/`İstanbul` regression failed before the fix; complete preorder and summary descendant identifiers now use explicit `C` collation and the regression passes in both insertion orders. Final source scans find no production `navigation_departments`, `FormatParam`, `previousPostgresPlaceholder`, or handler `directorytree` reference; added identities use only `example.org`; small/large plan coverage proves constant complete-route query roles and one recursive loop per relation; `git diff --check` is clean. A fresh acceptance bundle passed across `adminusers`, `adminsubscription`, and `handler`. No Critical, Important, or actionable Minor findings remain.

- [x] **Step 4: Commit the complete change**

  ```bash
  git add backend/internal/adminusers backend/internal/handler/admin_users.go backend/internal/handler/admin_users_test.go docs/architecture.md docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md docs/superpowers/plans/2026-07-19-admin-users-hierarchy-cleanup.md
  git commit -m "refactor(admin-users): remove runtime hierarchy reconstruction"
  ```

  Commit evidence (2026-07-19): committed the complete issue #171 change on `refactor/admin-users-hierarchy-cleanup-171` with message `refactor(admin-users): remove runtime hierarchy reconstruction`. Nothing was pushed and no PR was created.
