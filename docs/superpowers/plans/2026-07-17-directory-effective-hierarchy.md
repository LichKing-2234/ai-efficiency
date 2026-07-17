# Directory Effective Hierarchy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver issue #165 by persisting one deterministic, applied-run-versioned effective parent relation for every current Directory Sync department without migrating existing readers in this ticket.

**Architecture:** Add one nullable `effective_parent_external_id` field to the existing current `directory_departments` snapshot; `source_id` and `last_seen_run_id` remain its source and applied-run version. A private pure hierarchy module resolves missing parents and breaks one deterministic edge per closed cycle before facts are mutated, and `replaceFactsTx` persists the result in the existing atomic apply transaction. Administrator, representative-scope, offboarding, and directory readers continue using their current response-compatible paths until issues #169 and #171 migrate them.

**Tech Stack:** Go 1.23/1.24, Ent 0.14, PostgreSQL 16, existing `directorysync` apply transaction, Go testing, `internal/testdb`.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/feat-directory-effective-hierarchy-165` on `feat/directory-effective-hierarchy-165` from `feat/platform-loading-performance@e95277d0`.
- Current code, issue #165, `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md`, and `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md` are the contract.
- Persist the relation on `directory_departments`; do not add a hierarchy history table, Redis cache, new process, direct Sub2API coupling, or reader migration.
- Keep `parent_external_id` as the upstream fact. Null, blank, or missing same-source parents have no effective parent; do not rewrite the raw field.
- For each closed cycle, remove only the edge owned by the row ordered first by `LOWER(BTRIM(name)), external_id`. Preserve every other valid effective edge.
- Use `source_id` and `last_seen_run_id` as the applied hierarchy scope/version. Every department inserted by a successful apply must carry that run id.
- Resolve hierarchy in bounded, deterministic, non-recursive application work. Do not use recursion whose stack depth grows with the number of departments.
- Resolve the complete effective-parent map before deleting current facts so hierarchy validation errors leave the previous snapshot authoritative.
- Persist departments, effective parents, members, memberships, run completion, source pointers, and the work-item revision in the existing transaction.
- Existing administrator, representative-scope, offboarding, directory API, and frontend response contracts must remain unchanged in this ticket.
- Tests and examples use only synthetic departments, users, emails, credentials, and URLs.
- Maintain this file as a live ledger and check a step only after its command or edit actually completes.

**Status:** In progress. Task 1 is committed as `12ce1fa0`; Task 2 implementation and focused verification are complete, with review/commit pending. Tasks 3-4 have not started.

---

### Task 1: Add Applied Hierarchy Storage

**Files:**
- Modify: `backend/ent/schema/directory_department.go`
- Regenerate: `backend/ent/`
- Modify: `backend/go.sum`
- Modify: `backend/internal/directorysync/schema_test.go`

**Interfaces:**
- Produces nullable `DirectoryDepartment.EffectiveParentExternalID *string`.
- Produces Ent builders/predicates/selectors for `effective_parent_external_id`.
- Adds index `(source_id, effective_parent_external_id)` for the dependent reader migrations.

- [x] **Step 1: Write the RED schema regression**

  Extend `TestDirectorySyncSchemaPersistsFactsAndRevocationFloor` so the synthetic
  department calls `SetEffectiveParentExternalID("dept-root")` and asserts both
  `ParentExternalID` and `EffectiveParentExternalID` remain `dept-root` after reload.

- [x] **Step 2: Run the schema test and record RED**

  ```bash
  cd backend
  go test ./internal/directorysync -run '^TestDirectorySyncSchemaPersistsFactsAndRevocationFloor$' -count=1
  ```

  Expected: compile failure because the generated Ent builder and entity do not yet expose `EffectiveParentExternalID`.

  RED evidence (2026-07-17):

  ```text
  internal/directorysync/schema_test.go:73:3: ...
  SetEffectiveParentExternalID undefined
  FAIL github.com/ai-efficiency/backend/internal/directorysync [build failed]
  ```

- [x] **Step 3: Add the Ent field and index**

  Add to `DirectoryDepartment.Fields()`:

  ```go
  field.String("effective_parent_external_id").Optional().Nillable(),
  ```

  Add to `DirectoryDepartment.Indexes()`:

  ```go
  index.Fields("source_id", "effective_parent_external_id"),
  ```

- [x] **Step 4: Regenerate Ent twice and verify no drift**

  ```bash
  cd backend
  go generate ./ent
  git diff --check
  go generate ./ent
  git diff --check
  ```

  Expected: the second generation produces no additional tracked diff.

  Evidence (2026-07-17): the default `proxy.golang.org` route timed out before
  generation, while `https://goproxy.cn` returned 200. Running both generations
  with a command-local `GOPROXY=https://goproxy.cn,direct` succeeded without
  changing global Go configuration. The diff SHA-256 before and after the second
  generation was identical:

  ```text
  87702453cf10fcf21717cc9ac2040fdfb197c756796456b587824b5af2005af7
  ```

- [x] **Step 5: Run GREEN schema verification and commit**

  ```bash
  cd backend
  go test ./internal/directorysync -run '^TestDirectorySyncSchemaPersistsFactsAndRevocationFloor$' -count=1
  go test ./ent/... -count=1
  cd ..
  git diff --check
  git add backend/ent backend/internal/directorysync/schema_test.go
  git commit -m "feat(directorysync): add effective hierarchy storage"
  ```

  GREEN evidence (2026-07-17):

  ```text
  ok  github.com/ai-efficiency/backend/internal/directorysync  0.979s
  go test ./ent/... -count=1: PASS
  git diff --check: exit 0
  ```

---

### Task 2: Resolve Effective Parents Deterministically

**Files:**
- Create: `backend/internal/directorysync/effective_hierarchy.go`
- Create: `backend/internal/directorysync/effective_hierarchy_test.go`

**Interfaces:**
- Produces `resolveEffectiveParents([]DepartmentRecord) (map[string]string, error)`.
- A missing map value or empty returned parent means effective root.
- Returns an error for blank or duplicate applied department external IDs.

- [x] **Step 1: Write RED semantic tests**

  Add table tests for:

  ```go
  tests := []struct {
      name        string
      departments []DepartmentRecord
      want        map[string]string
  }{
      {
          name: "acyclic and orphan",
          departments: []DepartmentRecord{
              {ExternalID: "dept-root", Name: "Root"},
              {ExternalID: "dept-child", ParentExternalID: "dept-root", Name: "Child"},
              {ExternalID: "dept-orphan", ParentExternalID: "dept-missing", Name: "Orphan"},
          },
          want: map[string]string{"dept-root": "", "dept-child": "dept-root", "dept-orphan": ""},
      },
      {
          name: "name first cycle anchor",
          departments: []DepartmentRecord{
              {ExternalID: "dept-a", ParentExternalID: "dept-b", Name: "Zulu"},
              {ExternalID: "dept-b", ParentExternalID: "dept-c", Name: "Alpha"},
              {ExternalID: "dept-c", ParentExternalID: "dept-a", Name: "Alpha"},
          },
          want: map[string]string{"dept-a": "dept-b", "dept-b": "", "dept-c": "dept-a"},
      },
      {
          name: "self cycle",
          departments: []DepartmentRecord{{ExternalID: "dept-self", ParentExternalID: "dept-self", Name: "Self"}},
          want: map[string]string{"dept-self": ""},
      },
  }
  ```

  Also add:

  - an input-order test that compares all permutations of a small cycle/forest;
  - blank/duplicate ID error tests;
  - a 10,000-department chain test that proves completion without recursive stack growth.

- [x] **Step 2: Run the hierarchy tests and record RED**

  ```bash
  cd backend
  go test ./internal/directorysync -run '^TestResolveEffectiveParents' -count=1
  ```

  Expected: compile failure because `resolveEffectiveParents` does not exist.

  RED evidence (2026-07-17):

  ```text
  internal/directorysync/effective_hierarchy_test.go:42:16: undefined: resolveEffectiveParents
  internal/directorysync/effective_hierarchy_test.go:65:15: undefined: resolveEffectiveParents
  internal/directorysync/effective_hierarchy_test.go:88:17: undefined: resolveEffectiveParents
  internal/directorysync/effective_hierarchy_test.go:109:14: undefined: resolveEffectiveParents
  FAIL github.com/ai-efficiency/backend/internal/directorysync [build failed]
  ```

- [x] **Step 3: Implement the private hierarchy module**

  Implement `resolveEffectiveParents` with these steps:

  1. Build `departmentByID` and reject blank/duplicate external IDs.
  2. Initialize each parent to the trimmed upstream parent only when that exact ID exists in the same input; otherwise use `""`.
  3. Walk the functional parent graph iteratively. Track the index of each ID in the current path and a global completed set so every department is completed once.
  4. When a path closes a cycle, choose its anchor by normalized name then external ID and set only that anchor's effective parent to `""`.
  5. Return one map entry for every input department.

  Keep anchor comparison in one helper:

  ```go
  func effectiveHierarchyLess(left, right DepartmentRecord) bool {
      leftName := strings.ToLower(strings.TrimSpace(left.Name))
      rightName := strings.ToLower(strings.TrimSpace(right.Name))
      if leftName != rightName {
          return leftName < rightName
      }
      return left.ExternalID < right.ExternalID
  }
  ```

- [x] **Step 4: Run GREEN, race, and repeatability verification**

  ```bash
  cd backend
  gofmt -w internal/directorysync/effective_hierarchy*.go
  go test ./internal/directorysync -run '^TestResolveEffectiveParents' -count=20
  go test -race ./internal/directorysync -run '^TestResolveEffectiveParents' -count=1
  cd ..
  git diff --check
  ```

  GREEN evidence (2026-07-17):

  ```text
  go test ./internal/directorysync -run '^TestResolveEffectiveParents' -count=20
  ok  github.com/ai-efficiency/backend/internal/directorysync  0.739s
  go test -race ./internal/directorysync -run '^TestResolveEffectiveParents' -count=1
  ok  github.com/ai-efficiency/backend/internal/directorysync  1.768s
  git diff --check: exit 0
  ```

- [x] **Step 5: Review and commit**

  Review the Task 2 range against issue #165 and the active specs. Resolve every Critical/Important finding, rerun Step 4, then:

  ```bash
  git add backend/internal/directorysync/effective_hierarchy.go backend/internal/directorysync/effective_hierarchy_test.go
  git commit -m "feat(directorysync): resolve effective department hierarchy"
  ```

  Review evidence (2026-07-17): initial Spec review found only the stale plan
  status. Initial Standards review found two Important test gaps: the large chain
  was root-first, and no tail entered a closed cycle. The status was corrected,
  the 10,000-row fixture became leaf-first, and a name-sorted tail-to-cycle case
  now proves `cycleStart > 0` excludes the tail from anchor selection. Focused
  repeat and race tests passed again. Spec re-review: PASS. Standards re-review:
  PASS, with no Critical/Important findings and no over-design finding.

---

### Task 3: Persist Hierarchy In The Atomic Apply Transaction

**Files:**
- Modify: `backend/internal/directorysync/service.go`
- Modify: `backend/internal/directorysync/service_test.go`
- Modify: `backend/internal/directorysync/run_query_plan_test.go`

**Interfaces:**
- `replaceFactsTx` consumes the complete map from `resolveEffectiveParents` before deleting current facts.
- Every new department row persists the raw parent, effective parent, source ID, and `last_seen_run_id`.
- No handler, frontend, `adminusers`, `representativescope`, or offboarding interface changes.

- [ ] **Step 1: Write RED apply persistence tests**

  Add `TestCompleteApplyPersistsVersionedEffectiveHierarchy` using a synthetic
  `ExecutionResult` containing an acyclic child, an orphan, and the three-row cycle
  from Task 2. Call `completeApplyRun` and assert:

  - every stored department has `LastSeenRunID == run.ID`;
  - raw `ParentExternalID` remains unchanged;
  - effective parents match Task 2, including `dept-b` as the cycle anchor;
  - source `last_run_id` and `last_successful_run_id` both equal the run;
  - applying the same departments in reverse order under a second run produces the same effective map and replaces every hierarchy row's version with the second run ID.

- [ ] **Step 2: Extend rollback and compatibility regressions**

  Seed `TestServiceApplyRollsBackFactsWhenSourcePointerUpdateFails` with a completed
  baseline run and department whose effective parent is persisted. After the
  injected source-pointer failure, assert the baseline raw parent, effective parent,
  `last_seen_run_id`, and source pointers remain authoritative.

  Extend the large-history state capture to include raw parent, effective parent,
  and run ID so preview/failed apply preservation and successful replacement cover
  hierarchy state without changing offboarding facts.

- [ ] **Step 3: Run RED apply tests**

  ```bash
  cd backend
  go test ./internal/directorysync -run 'EffectiveHierarchy|ApplyRollsBackFactsWhenSourcePointerUpdateFails|LargeHistory' -count=1
  ```

  Expected: runtime assertions fail because `replaceFactsTx` does not persist effective parents.

- [ ] **Step 4: Wire the builder before destructive writes**

  At the start of `replaceFactsTx`, before any `DELETE`, resolve the map:

  ```go
  effectiveParents, err := resolveEffectiveParents(result.Departments)
  if err != nil {
      return fmt.Errorf("resolve effective department hierarchy: %w", err)
  }
  ```

  When creating each department, preserve the raw field and set the derived field only when non-empty:

  ```go
  if effectiveParentID := effectiveParents[department.ExternalID]; effectiveParentID != "" {
      create.SetEffectiveParentExternalID(effectiveParentID)
  }
  ```

- [ ] **Step 5: Run GREEN apply and compatibility verification**

  ```bash
  cd backend
  gofmt -w internal/directorysync/service*.go internal/directorysync/run_query_plan_test.go
  go test ./internal/directorysync -run 'EffectiveHierarchy|Apply|LargeHistory|Department' -count=1
  go test ./internal/adminusers ./internal/representativescope ./internal/handler -count=1
  go test -race ./internal/directorysync -run 'EffectiveHierarchy|ApplyRollsBackFactsWhenSourcePointerUpdateFails' -count=1
  cd ..
  git diff --check
  ```

- [ ] **Step 6: Review and commit**

  Review Task 3 against the issue acceptance criteria, atomic apply contract, and
  response compatibility. Resolve every Critical/Important finding, rerun Step 5,
  then:

  ```bash
  git add backend/internal/directorysync/service.go backend/internal/directorysync/service_test.go backend/internal/directorysync/run_query_plan_test.go
  git commit -m "feat(directorysync): persist effective hierarchy on apply"
  ```

---

### Task 4: Reconcile Architecture, Verify, And Publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-07-17-directory-effective-hierarchy.md`

**Interfaces:**
- Architecture identifies Directory Sync apply as the owner of persisted effective hierarchy.
- Existing readers remain explicitly transitional and unchanged until #169/#171.

- [ ] **Step 1: Update current architecture**

  Document the nullable persisted effective-parent relation, source/run versioning,
  deterministic orphan/cycle handling, atomic apply ownership, and staged reader
  migration. Do not claim #169 or #171 behavior has landed.

- [ ] **Step 2: Run complete local verification**

  ```bash
  cd backend
  go generate ./ent
  go test ./... -count=1
  go vet ./...
  cd ../frontend
  npm test
  npm run build
  cd ../ae-cli
  go test ./... -count=1
  cd ..
  bash deploy/test/release-frontend-embed-test.sh
  git diff --check
  ```

  Report environment-sensitive role E2E separately and leave it unclaimed unless run.

- [ ] **Step 3: Run final Standards and Spec reviews**

  Compare the complete branch with `e95277d0`. Require both reviews to confirm no
  Critical/Important findings, no reader migration, raw-parent preservation,
  deterministic parity with the existing read-side effective relation, atomic
  rollback, and bounded non-recursive scale behavior.

- [ ] **Step 4: Commit documentation and publish**

  ```bash
  git add docs/architecture.md docs/superpowers/plans/2026-07-17-directory-effective-hierarchy.md
  git commit -m "docs(architecture): record applied effective hierarchy"
  git push -u origin feat/directory-effective-hierarchy-165
  ```

  Create a non-Draft PR targeting `feat/platform-loading-performance` with
  `Closes #165`, mark #165 `in-review`, and wait for backend/frontend/ae-cli/deploy-static CI on the exact PR head. Do not merge to main, release, tag, deploy, or run Helm.
