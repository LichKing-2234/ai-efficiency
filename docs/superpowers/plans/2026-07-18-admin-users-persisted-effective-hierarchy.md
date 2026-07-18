# Admin Users Persisted Effective Hierarchy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Implementation, non-environment-sensitive verification, and Standards/Spec review are complete on `feat/admin-users-effective-hierarchy-169`. Exact-head CI and merge for #169 remain outstanding; the environment-sensitive role E2E gap is recorded below.

**Known Remaining Gap:** `npm run test:e2e:role` was attempted but no application was listening on its required `http://localhost:5173`, so all three cases ended at navigation with `ERR_CONNECTION_REFUSED`. This environment-sensitive item remains unchecked; backend/full frontend unit/build, ae-cli, embedded-static, and diff-hygiene verification passed.

**Goal:** Complete issue #169 by making every administrator department read and current-filter mutation consume the persisted effective hierarchy without reconstructing source-wide cycles per request.

**Architecture:** Keep `navigation_departments` as the shared source-scoped SQL relation so list, targets, options, children, summaries, and page enrichment retain one predicate boundary. Project `directory_departments.effective_parent_external_id` directly into that relation, while preserving only the bounded subtree, candidate-ancestor, and summary-descendant recursion required by the requested result. The deployment sequence must apply a current Directory Sync snapshot after #165 storage is present and before activating #169 readers because legacy rows do not carry a hierarchy-materialization version and `NULL` is also the valid persisted root value.

**Tech Stack:** Go 1.24, Ent, PostgreSQL recursive CTEs, Gin, Vue 3, TypeScript, Vitest.

## Global Constraints

- Keep list, target, option, child, summary, enrichment, persisted-job, and compatibility current-filter response/selection contracts unchanged.
- Treat current membership rows as authoritative and use the legacy primary department only when a member has no current membership rows.
- Preserve matched-user-id and normalized-email union mapping, unmatched default-list users, multi-membership deduplication, representative counts, one-based pagination, and stable user-id ordering.
- Read `effective_parent_external_id` exactly as persisted; do not infer missing parents or cycle anchors in administrator request SQL.
- Keep subtree, page-candidate ancestor, and requested-summary descendant recursion bounded to their result roles and prove each recursive union runs once.
- Use only synthetic identities and domains in tests and examples.
- Update this plan checkbox immediately after each completed step.
- Do not release, tag, deploy, or run Helm from this issue.

---

### Task 1: Specify The Persisted Read Contract With RED Tests

**Files:**
- Modify: `backend/internal/adminusers/query_plan_test.go`
- Modify: `backend/internal/adminusers/department_test.go`
- Modify: `backend/internal/adminusers/service_test.go`

**Interfaces:**
- Requires every captured administrator hierarchy query to project `directory_departments.effective_parent_external_id`.
- Forbids `source_cardinality`, `cycle_walk`, `closed_cycle_paths`, `cycle_members`, and `cycle_anchors` in active read SQL.

- [x] **Step 1: Add persisted-parent authority and fixture coverage**

  Populate effective parents explicitly in department fixtures. Add a hierarchy whose raw parents conflict with the persisted relation and require options, navigation, list/targets, and summaries to follow only the persisted relation.

- [x] **Step 2: Replace source-wide cycle-plan assertions**

  Require small and large captured queries to omit every source-wide reconstruction CTE, retain the shared source-scoped `navigation_departments` projection, and keep subtree/ancestor/descendant recursive unions at one actual loop.

- [x] **Step 3: Run focused RED**

  ```bash
  cd backend
  go test ./internal/adminusers -run 'PersistedEffective|DepartmentReadPlan|TargetPlanRecursive|CountPlanAndPagePlan|DepartmentChildren|ListTargets' -count=1
  ```

  Expected: failures show that current SQL still references runtime cycle reconstruction or ignores explicit persisted-parent fixtures.

  RED evidence (2026-07-18): the focused suite failed because the conflicting raw-parent fixture exposed `dept-persisted-child` and `dept-persisted-leaf` as roots instead of following the stored root/child/leaf relation, and captured small/large SQL still contained `source_cardinality` and `cycle_walk` rather than projecting the persisted effective parent.

### Task 2: Switch Every Administrator Read To Persisted Parents

**Files:**
- Modify: `backend/internal/adminusers/department.go`
- Verify: `backend/internal/adminusers/service.go`
- Verify: `backend/internal/adminsubscription/service.go`
- Verify: `backend/internal/handler/admin_users.go`

**Interfaces:**
- `effectiveDepartmentCTEs(sourcePlaceholder string)` continues producing the shared `navigation_departments` relation during the staged migration.
- All existing consumers continue composing `effectiveSubtreeCTE`, `eligibleUserCTEs`, candidate ancestors, department options/children, summaries, and current-filter target readers without new service branches.

- [x] **Step 1: Implement the minimal persisted projection**

  Replace source-wide cardinality/cycle CTEs with one materialized source-scoped projection of external id, raw parent, persisted effective parent, name, path, and metadata.

- [x] **Step 2: Run focused GREEN and parity suites**

  ```bash
  cd backend
  gofmt -w internal/adminusers/department.go internal/adminusers/department_test.go internal/adminusers/service_test.go internal/adminusers/query_plan_test.go
  go test ./internal/adminusers ./internal/adminsubscription ./internal/handler -count=2
  git diff --check
  ```

  Confirm list/count, targets, both current-filter mutation paths, paging, representative totals, cycle/orphan navigation, and query plans remain green.

  GREEN evidence (2026-07-18): the persisted-parent RED suite passed; `go test ./internal/adminusers ./internal/adminsubscription ./internal/handler -count=2` passed after all cross-layer directory fixtures were updated to the post-apply storage shape; and `git diff --check` was clean. Production `backend/internal/adminusers` contains no source-cardinality or cycle-reconstruction CTE.

### Task 3: Document, Verify, Review, And Deliver

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: this plan
- Verify: `frontend/src/__tests__/admin-users-view.test.ts`

- [x] **Step 1: Update current architecture and the active performance contract**

  Record that administrator reads now consume apply-time effective parents, distinguish bounded result recursion from removed source-wide reconstruction, and state the expand-contract snapshot re-apply requirement. Keep the historical Directory Sync design intact except where its current staged-migration statement already owns this contract.

  Documentation evidence (2026-07-18): current architecture and the active loading-performance contract now describe the persisted `navigation_departments` projection, the three bounded recursive result roles, #171 cleanup ownership, and the required successful apply between storage expansion and reader activation for legacy snapshots.

- [ ] **Step 2: Run full verification**

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

  Report environment-sensitive role E2E separately if its required runtime is unavailable.

  Verification evidence (2026-07-18): `git diff --check`; backend `go test ./... -count=1` and `go vet ./...`; frontend 46 files / 680 tests and production build; ae-cli `go test ./... -count=1`; and `bash deploy/test/release-frontend-embed-test.sh` passed. The embed script emitted its known synthetic unresolved-hook warning before passing. Role E2E remains unchecked for the connection-refused environment gap recorded above.

- [x] **Step 3: Review the exact branch against issue #169 and active specs**

  Confirm every administrator read/mutation path uses the shared persisted relation, no source-wide read-time inference remains, query recursion stays bounded, deployment compatibility is explicit, and no Critical or Important findings remain. Fix findings and rerun affected verification.

  Review evidence (2026-07-18): the first two-axis review found one Important plan-status contradiction and one Minor active-spec overview drift. Commit `ccf8679d` corrected both. Standards and Spec re-reviews of `303291c4...ccf8679d` report no remaining Critical, Important, or actionable Minor findings; the documented post-schema apply requirement and unavailable role E2E remain explicit residual operational gaps.

- [ ] **Step 4: Deliver to the performance feature branch**

  Push `feat/admin-users-effective-hierarchy-169`, open a PR targeting `feat/platform-loading-performance`, wait for exact-head backend/frontend/ae-cli/deploy-static CI, squash-merge, close #169, update #173, and record the final SHAs and CI run here. Do not release or deploy.
