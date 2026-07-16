# Department-Derived Quota Reset Approval Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for live tracking.

**Status:** Tasks 1-3 and Task 4 delivery are complete except browser
verification, which remains blocked and unchecked because the required browser
runtime reports no available browser.

**Review And Re-review Remediation Status (2026-07-16):** All Task 4 code,
architecture, formatting, searchable approver-picker, and generic-webhook
contract findings are fixed and fully reverified. The frontend workflow
approver type now also matches the source-free backend response. Browser
verification remains the only unchecked item because both the controller and
worker observed no browser runtime.

**Final Whole-Branch Review Remediation Status (2026-07-16):** The reset-start
follow-up, full backend verification, and final re-review are complete. Push and
PR checks remain; browser verification is blocked and unchecked.

**Goal:** Snapshot sequential quota reset approvals from the requester's exact
departments and configured ancestors; the selected subscription group only
identifies the quota to reset.

**Architecture:** Reuse the existing approver config, request/event rows, JSON
workflow state machine, CAS transaction service, notifications, Work Items, and
approval workbench. Move request-time hierarchy resolution into `resolver.go`
and remove the branch-only chain table, API, types, tests, and settings UI.

**Tech Stack:** Go, Gin, Ent, PostgreSQL JSONB, Vue 3, TypeScript, Vitest.

## Constraints

- Contract:
  `docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md`.
- Add `0` business tables, `0` approval-chain routes, and `0` chain settings
  components/stores.
- Retain only two new backend production files: `workflow.go` and
  `workflow_service.go`.
- Retain only two new frontend production components:
  `QuotaResetDecisionDialog.vue` and `QuotaResetWorkflowTimeline.vue`.
- Keep hand-written production additions at or below 1,500 lines versus fixed
  base `70eb6ebe32298c333d4bebf144edd1b474a039dc`, including
  `backend/ent/schema` and excluding tests, frontend tests, and generated Ent
  outside that schema directory.
- Preserve V1 requests, internal `workflow_pending`, required comments, event
  audit, CAS, bounded detached reset, current-step counts, prior-actor reuse,
  and generic/WeCom notification channels.
- Use only synthetic identities, groups, URLs, and secrets in tests/docs.
- Update this plan after each completed step. Never check environment or browser
  verification without actually running it.

## Final Deletions

- `backend/ent/schema/quota_reset_approval_chain.go`
- `backend/ent/quotaresetapprovalchain.go`
- `backend/ent/quotaresetapprovalchain/quotaresetapprovalchain.go`
- `backend/ent/quotaresetapprovalchain/where.go`
- `backend/ent/quotaresetapprovalchain_create.go`
- `backend/ent/quotaresetapprovalchain_delete.go`
- `backend/ent/quotaresetapprovalchain_query.go`
- `backend/ent/quotaresetapprovalchain_update.go`
- `backend/internal/quotareset/workflow_config.go`
- `backend/internal/quotareset/workflow_config_test.go`
- `frontend/src/components/settings/QuotaResetApprovalChainSettings.vue`

---

### Task 1: Replace Chain Routing With Department Resolution

**Files:**

- Modify: `backend/internal/quotareset/resolver.go`
- Modify: `backend/internal/quotareset/resolver_test.go`
- Modify: `backend/internal/quotareset/service.go`
- Modify: `backend/internal/quotareset/service_test.go`
- Modify: `backend/internal/quotareset/types.go`
- Modify: `backend/internal/quotareset/notification_test.go`
- Modify: `backend/internal/workitems/service_test.go`
- Move resolution code out of:
  `backend/internal/quotareset/workflow_config.go`
- Move resolution tests out of:
  `backend/internal/quotareset/workflow_config_test.go`

**Interfaces:**

```go
func (r *ApproverResolver) ResolveWorkflow(context.Context, *ent.User) (*Workflow, []DepartmentPathEvidence, error)
func (s *Service) resolveWorkflowSnapshot(context.Context, *ent.User) (*Workflow, []DepartmentPathEvidence, error)
```

Neither interface accepts `providerID` or `groupID`.

- [x] **Step 1: Write failing resolver tests**

  Add these exact cases to `resolver_test.go`:

  - `TestResolveWorkflowMergesExactDepartments`: an active configured
    non-representative wins; only an exact department with no config falls back
    to its representatives.
  - `TestResolveWorkflowWalksMergedAncestorRounds`: immediate parents form one
    step and a converged root appears once in the next step.
  - `TestResolveWorkflowSkipsUnconfiguredAncestors`: parent representatives do
    not count after step one.
  - `TestResolveWorkflowUsesAdminFallbacks`: unusable exact/ancestor config
    produces fallback; no config and no representative skips the first step;
    no resolved steps produces one final admin fallback.
  - `TestResolveWorkflowRejectsStaleConfiguredMembership`: a configured user
    who is no longer an active member of that configured department is unusable.

  Define the test helper in the same file:

  ```go
  func workflowApproverIDs(step WorkflowStep) []int {
      ids := make([]int, 0, len(step.Approvers))
      for _, approver := range step.Approvers {
          ids = append(ids, approver.UserID)
      }
      sort.Ints(ids)
      return ids
  }
  ```

- [x] **Step 2: Run the tests and confirm RED**

  ```bash
  cd backend
  go test ./internal/quotareset -run '^TestResolveWorkflow' -count=1
  ```

  Expected: FAIL because `ResolveWorkflow` is not implemented and current
  routing still reads the selected group's chain.

- [x] **Step 3: Implement request-time resolution in `resolver.go`**

  Move `workflowDirectoryFacts` and its identity/candidate helpers from
  `workflow_config.go`. Build the workflow with this control flow:

  ```go
  exactIDs := facts.memberDepartmentIDs(requesterMember)
  exactStep, exactHadConfig := facts.resolveExactStep(exactIDs, requester.ID)
  if len(exactStep.Approvers) > 0 || exactHadConfig {
      exactStep.AdminFallback = len(exactStep.Approvers) == 0
      workflow.Steps = append(workflow.Steps, exactStep)
  }

  visited := stringSet(exactIDs)
  for round := facts.parentRound(exactIDs, visited); len(round) > 0; round = facts.parentRound(round, visited) {
      step, roundHadConfig := facts.resolveConfiguredRound(round, requester.ID)
      if roundHadConfig {
          step.AdminFallback = len(step.Approvers) == 0
          workflow.Steps = append(workflow.Steps, step)
      }
      if len(workflow.Steps) > maxWorkflowSteps {
          return nil, nil, ErrInvalidWorkflow
      }
  }
  if len(workflow.Steps) == 0 {
      workflow.Steps = append(workflow.Steps, adminFallbackWorkflowStep())
  }
  ```

  Sort/deduplicate departments and users. First-step config presence must be
  distinct from usable-candidate count. Configured users must be active matched
  directory members of that exact configured department; exclude requester,
  `relay_disabled_at != nil`, and `token_valid_after != nil`. Track visited
  departments so malformed cycles terminate. Store all exact paths and only
  configured department ids for retained ancestor rounds.

  Remove the obsolete nearest-match `ApproverResolver.Resolve`,
  `resolveDepartmentPath`, and `ApproverResolution` type after moving their
  coverage to `ResolveWorkflow`; no production caller may retain the old
  single-stage routing contract. Finalize `CurrentStep = 0`, mark the first step
  active, and call `EncodeWorkflow` before returning so size/version validation
  remains fail-closed.

  Keep the repeatable-read boundary in `resolveWorkflowSnapshot`, but call:

  ```go
  workflow, paths, err := NewApproverResolver(tx.Client()).ResolveWorkflow(ctx, requester)
  ```

- [x] **Step 4: Remove group inputs from request creation and add lifecycle tests**

  In `CreateRequest` call:

  ```go
  workflow, paths, err := s.resolveWorkflowSnapshot(ctx, requester)
  ```

  Add `TestCreateRequestV2IgnoresGroupForRouting` using two synthetic active
  groups, and `TestApproveV2SatisfiesDerivedLaterStep` where one actor appears
  in exact and ancestor steps. The latter must assert `step_satisfied`, source
  step `0`, no duplicate activation notification, and one final relay reset.
  Keep existing V1, rejection, comment, CAS, retry, Work Items, requester/team
  notification, and WeCom mention tests. Notification assertions must cover
  requester name/email, exact department paths, group, reason, step progress,
  previous comment, action URL, active approvers, and mentions sourced only from
  snapshotted `metadata.wecom_userid`.

- [x] **Step 5: Run focused regression and commit**

  ```bash
  cd backend
  go test ./internal/quotareset ./internal/workitems -count=1
  cd ..
  git add backend/internal/quotareset backend/internal/workitems \
    docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
  git commit -m "refactor(quotareset): derive approval workflow from departments"
  ```

  Expected: both packages PASS.

---

### Task 2: Remove Backend Chain Persistence And API

**Files:** Delete all backend entries under **Final Deletions**; regenerate
`backend/ent`; modify `backend/internal/handler/{quota_reset.go,quota_reset_test.go,router.go}`,
`backend/internal/quotareset/{types.go,schema_test.go}`.

**Remove:** `ApprovalChain*` types, `ListApprovalChains`, `SaveApprovalChains`,
and `GET/PUT /api/v1/admin/quota-reset/approval-chains`. Preserve all existing
request, decision, approver-config, candidate, and notification routes.

- [x] **Step 1: Add and run a failing no-table guard**

  Import `github.com/ai-efficiency/backend/ent/migrate` in `schema_test.go` and
  add:

  ```go
  func TestSchemaDoesNotRegisterQuotaResetApprovalChainTable(t *testing.T) {
      for _, table := range migrate.Tables {
          if table.Name == "quota_reset_approval_chains" {
              t.Fatal("branch-only approval-chain table is still registered")
          }
      }
  }
  ```

  ```bash
  cd backend
  go test ./internal/quotareset -run '^TestSchemaDoesNotRegisterQuotaResetApprovalChainTable$' -count=1
  ```

  Expected: FAIL while the branch-only table exists.

- [x] **Step 2: Delete the chain surface and regenerate Ent**

  Delete every backend chain file listed in **Final Deletions**, remove handler
  interface/fake methods and both routes, remove chain types, then run:

  ```bash
  cd backend
  go generate ./ent
  gofmt -w internal/handler/quota_reset.go internal/handler/quota_reset_test.go \
    internal/quotareset/types.go internal/quotareset/schema_test.go
  ```

- [x] **Step 3: Verify absence and commit**

  ```bash
  cd backend
  go test ./internal/quotareset ./internal/handler -run 'TestSchema|TestQuotaReset' -count=1
  ! rg -n 'QuotaResetApprovalChain|approval-chains|quota_reset_approval_chains' ent internal
  cd ..
  git add backend docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
  git commit -m "refactor(quotareset): remove approval chain configuration"
  ```

  Expected: tests PASS and the scan finds no chain contract.

---

### Task 3: Remove Frontend Chain Settings

**Files:** Delete the frontend entry under **Final Deletions**; modify
`frontend/src/components/settings/QuotaResetApprovalSettings.vue`,
`frontend/src/api/quotaReset.ts`, `frontend/src/types/index.ts`,
`frontend/src/i18n.ts`, `frontend/src/__tests__/quota-reset-api.test.ts`,
`frontend/src/__tests__/quota-reset-approval-settings.test.ts`, and
`frontend/src/__tests__/quota-reset-view.test.ts`.

**Remove:** `get/saveQuotaResetApprovalChains` and every
`QuotaResetApprovalChain*` type. Preserve department/member dropdowns,
notification channel controls, decision dialog, timeline, current-step actions,
and complete processed-history pagination.

- [x] **Step 1: Add and run a failing absence test**

  Replace the ordered-chain settings test with:

  ```ts
  expect(wrapper.find('[data-testid="quota-reset-chain-group"]').exists()).toBe(false)
  expect(wrapper.find('[data-testid="quota-reset-chain-save"]').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('Subscription group approval chains')
  ```

  ```bash
  cd frontend
  npm test -- src/__tests__/quota-reset-approval-settings.test.ts
  ```

  Expected: FAIL while the chain subsection remains mounted.

- [x] **Step 2: Remove chain component/API/types/copy and verify**

  Settings must contain only department approval representatives followed by
  notification controls. Search stays inside opened dropdowns.

  ```bash
  cd frontend
  npm test -- src/__tests__/quota-reset-api.test.ts \
    src/__tests__/quota-reset-approval-settings.test.ts \
    src/__tests__/quota-reset-view.test.ts
  npm run build
  ! rg -n 'QuotaResetApprovalChain|ApprovalChainSettings|approval-chains|quotaResetSettings\.chain' src
  ```

  Expected: tests/build PASS and the scan finds no frontend chain contract.

- [x] **Step 3: Commit frontend cleanup**

  ```bash
  cd ..
  git add frontend docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
  git commit -m "refactor(frontend): remove quota reset chain settings"
  ```

---

### Task 4: Documentation, Full Verification, And PR

**Files:** Modify `docs/architecture.md` and keep this plan's status/checklist
current. Do not rewrite the historical 2026-07-07 spec.

- [x] **Step 1: Update current architecture**

  Document exact-department config/representative fallback, configured ancestor
  rounds, same-round merge, admin fallback, request-time snapshot, prior-actor
  reuse, CAS/event audit, and explicit generic/WeCom channels. State that the
  subscription group affects only the reset target.

- [x] **Step 2: Run full automated verification**

  ```bash
  cd backend && go test ./... -count=1 && go vet ./...
  cd ../ae-cli && go test ./... -count=1
  cd ../frontend && npm test && npm run build && npm run test:e2e:role
  ```

  Expected: every command exits 0.

  Verification evidence (2026-07-16):

  - Review-fix rerun: backend tests/vet and ae-cli tests exited 0; frontend
    reported 39 files / 435 tests, a successful 192-module production build,
    and role E2E 16/16. Output:
    `.superpowers/sdd/task-4-logs/review-fixes-full-verification.log`.
  - Review-fix focused checks passed quota-reset/work-items tests and vet, 3
    frontend quota-reset files / 22 tests, and the production build. Output:
    `.superpowers/sdd/task-4-logs/review-fixes-focused.log`.

  - The final combined rerun used the three commands above against the final
    production diff. Backend tests/vet and ae-cli tests exited 0; frontend
    reported 39 test files / 435 tests, a successful production build, and role
    E2E 16/16. Output:
    `.superpowers/sdd/task-4-logs/full-verification-commit-gate.log`.
  - Focused regression after the line-budget simplification passed
    `go test ./internal/quotareset ./internal/workitems -count=1`,
    `go vet ./internal/quotareset`, 3 frontend quota-reset files / 22 tests,
    and the frontend production build. Output:
    `.superpowers/sdd/task-4-logs/focused-regression.log`.
  - The first frontend attempt reached successful unit tests and build but role
    E2E could not connect to its required Vite URL at `localhost:5173`. The
    successful fresh rerun used `npm run dev -- --host 127.0.0.1`; the failed
    environment attempt remains in
    `.superpowers/sdd/task-4-logs/frontend-test-build-role-e2e.log`.
  - Re-review rerun: focused quota-reset/work-items tests and vet passed; focused
    frontend quota-reset API/settings/view tests reported 3 files / 21 tests and
    the build passed. Because backend production code was structurally reduced,
    full backend tests/vet were rerun and passed. Full frontend verification
    reported 39 files / 435 tests, a successful 192-module build, and role E2E
    16/16. The untouched CLI was not rerun for this frontend finding.

- [x] **Step 3: Audit final scope**

  ```bash
  git diff --check
  git diff --numstat 70eb6ebe32298c333d4bebf144edd1b474a039dc -- backend/internal backend/ent/schema frontend/src ':(exclude)**/*_test.go' ':(exclude)frontend/src/__tests__/**' | awk '{a+=$1;d+=$2} END{print a,d}'
  git diff --numstat 70eb6ebe32298c333d4bebf144edd1b474a039dc -- backend/ent ':(exclude)backend/ent/schema/**' \
    | awk '{ a += $1; d += $2 } END { print "generated Ent +" a "/-" d }'
  ! rg -n 'QuotaResetApprovalChain|approval-chains|quota_reset_approval_chains' \
    backend frontend docs/architecture.md \
    --glob '!backend/internal/quotareset/schema_test.go'
  git status --short
  ```

  Expected: no whitespace errors; hand-written additions are at most 1,500;
  chain scan is empty; only intentional branch files are modified. Simplify and
  leave this step unchecked if the limit is exceeded.

  Latest fixed-base evidence (2026-07-16 final review fix wave):

  - `git diff --check` exited 0.
  - The binding hand-written production audit, including
    `backend/ent/schema`, is `+1499/-523` after the re-review follow-up.
  - Generated Ent outside `backend/ent/schema` is `+862/-33` and is reported
    separately from the hand-written total.
  - The production chain scan found no matches when excluding only
    `backend/internal/quotareset/schema_test.go`; that guard still contains the
    intentional `quota_reset_approval_chains` literal.
  - `git status --short` listed only intentional re-review tests, picker code,
    structural simplifications, and plan evidence before commit.

  Webhook-contract re-review evidence (2026-07-16):

  - A test-first recursive generic-payload assertion failed on leaked
    `notification_ids` before the outward DTO mapping was restored, then passed
    after the fix. Synthetic requester, previous-approver, and current-approver
    WeCom IDs are all rejected by value as well as by field name.
  - Generic webhook requester, active approver, workflow-step, and decision
    history are now mapped explicitly; internal workflow DTOs are not serialized
    into the outward payload. WeCom rendering continues to read only snapshotted
    active-approver notification IDs.
  - Focused quota-reset/work-items tests and vet, full backend tests and vet,
    whitespace checks, and scope audits passed. Frontend files are unchanged
    from the searchable-picker commit and were not rerun.
  - The earlier moving-base count is superseded by the binding fixed-base
    `+1499/-523` audit above; the production chain scan is empty when excluding
    only the intentional schema-test guard.

  Frontend workflow-approver contract re-review evidence (2026-07-16):

  - A source-free fixture using `satisfies QuotaResetWorkflowApprover` made
    `npm run build` fail with TS1360 while the stale public type still required
    `source`. Removing that one obsolete type field and both fixture values made
    the same build pass.
  - Focused quota-reset tests passed 22/22; full frontend tests passed 435/435;
    the production build transformed 192 modules; role E2E passed 16/16. The
    temporary Vite server was stopped afterward.
  - Backend and generic webhook files are unchanged from `c569a34`; no backend
    rerun was required. Workflow-approver source and production chain scans are
    empty.
  - The earlier moving-base count is superseded by the binding fixed-base
    `+1499/-523` audit above.

- [ ] **Step 4: Browser-test one complete workflow**

  ```bash
  docker compose -p ai-efficiency-quota-reset-rework \
    --env-file /Users/admin/ai-efficiency/deploy/.env \
    -f docker-compose.dev.yml \
    up -d --build --force-recreate --remove-orphans
  ```

  Use the browser skill at `/usage/quota-reset` with synthetic exact and parent
  approvers. Submit, approve each active step with a comment, and verify queue
  badges, timeline/comments, prior-actor auto-satisfaction without a repeated
  notification, one relay reset, terminal `approved_reset_succeeded`, and no
  desktop/mobile overlap. Record the URL, result, and screenshot paths here
  before checking this step.

  **BLOCKED (2026-07-16):** This checkbox remains unchecked. The requested
  `26.707.71524` browser skill path was absent, so the installed replacement at
  `26.707.91948` was read and followed. Browser runtime selection for
  `http://127.0.0.1:28081/usage/quota-reset` returned `No browser is available`;
  the required bootstrap troubleshooting then returned an empty browser list
  (`[]`). No standalone Playwright or alternate browser was substituted, and
  there are no desktop/mobile screenshot paths.

  The review-fix controller and worker both observed the same missing browser
  runtime, so this evidence was not reclassified and the API diagnostic remains
  explicitly non-browser verification.

  Environment and diagnostic evidence:

  - Compose command: `docker compose -p ai-efficiency-quota-reset-rework
    --env-file /Users/admin/ai-efficiency/deploy/.env -f
    docker-compose.dev.yml up -d --build --force-recreate --remove-orphans`.
    Output: `.superpowers/sdd/task-4-logs/compose-rebuild-final.log`.
  - Isolated URL/ports: `http://127.0.0.1:28081/usage/quota-reset`, backend
    `28081`, PostgreSQL `25432`, Redis `26379`. The pre-existing
    `ai-efficiency` project remained running on `18081/15432/16379`; no stop,
    recreate, remove, or build command targeted it.
  - Synthetic actors: Alice requester (local user 101), Bob exact-department
    approver (102), and Carol parent/root approver (103). Fixture output:
    `.superpowers/sdd/task-4-logs/browser-fixture-seed.log`.
  - API-only diagnostic fallback (not browser verification) rejected Bob's
    empty comment with `decision_reason_required`, showed Bob and Carol each
    with one actionable approval, preserved both comments, auto-satisfied the
    root from Carol's parent approval without a second activation notice,
    issued exactly one relay reset, and ended at
    `approved_reset_succeeded`. Output:
    `.superpowers/sdd/task-4-logs/api-workflow-fallback.log`.

- [x] **Step 5: Commit docs, push, and watch PR 146**

  ```bash
  git add docs/architecture.md \
    docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
  git commit -m "docs(architecture): document department-derived quota reset approvals"
  git push origin codex/multi-stage-quota-reset-approval
  gh pr checks 146 --watch
  ```

  Skip an empty docs commit. Do not merge, tag, release, or run Helm without a
  separate request. PR 146 must eventually be squash merged so abandoned chain
  commits do not enter `main` history.

  Delivery evidence (2026-07-16):

  - Review-fix implementation commit: `de3c327` (`fix(quotareset): address
    task 4 review findings`). GitHub smart-HTTP receive-pack timed out twice;
    the exact matching tree was uploaded through the Git Data API, the branch
    ref was advanced without force, and read-side fetch synchronized local and
    remote to the API commit.
  - `gh pr checks 146 --watch --interval 10` exited 0 for run `29490734626`:
    `deploy-static` passed in 10s, `ae-cli` in 29s, `frontend` in 1m2s, and
    `backend` in 3m8s.
  - The review-fix evidence-only plan commit is the final branch update; its
    final-head CI result is recorded in the Task 4 report.

  - Production simplification commit: `76a767f` (`refactor(quotareset):
    simplify approval workflow`).
  - Architecture/verification commit: `d929b00` (`docs(architecture): document
    department-derived quota reset approvals`).
  - `git push origin codex/multi-stage-quota-reset-approval` updated PR 146.
  - `gh pr checks 146 --watch` exited 0 for run `29486757404`: `deploy-static`
    passed in 17s, `ae-cli` in 31s, `frontend` in 51s, and `backend` in 3m2s.
  - No merge, tag, release, Helm command, or deployment was run.

---

### Task 5: Final Whole-Branch Review Remediation

- [x] **Step 1: Preserve V2 failed-reset Work Items for the final approver**

  Add a real V2 create/approve/reset-failure regression, then count retry work
  through `approved_by_user_id` without changing active-step, V1, admin, or
  total deduplication behavior.

  RED returned zero after a real V2 final approval cleared active approvers and
  the relay reset failed. GREEN returned one through `approved_by_user_id`;
  the existing Work Items suite also passed.

- [x] **Step 2: Project authenticated workflow summaries explicitly**

  Add recursive backend/API leakage regressions for requester and future
  approver WeCom IDs. Return only the public timeline/action fields and align
  frontend response types and fixtures.

  RED leaked `notification_ids` and all synthetic values from mine,
  approvals, and admin responses; `current_step: 0` was also omitted. GREEN
  reuses the explicit outward workflow mapper, preserves the UI/history fields,
  and passes the backend service/API regressions plus the frontend exact-key
  build guard.

- [x] **Step 3: Complete transactional V2 creation and cancellation audit**

  Record initial step activation/fallback events in the create transaction and
  make the V2 cancellation CAS update plus cancellation event one transaction.
  Preserve V1 cancellation and post-commit notification behavior.

  RED showed both missing activation events, a committed request when the
  unattempted activation audit was injected to fail, and a committed cancelled
  status after cancellation audit failure. GREEN passes all four regressions
  with transactional event writes and V2 status/revision CAS.

- [x] **Step 4: Close documentation and bounded-size review findings**

  Mark the approved spec implemented while retaining its replacement history.
  Add a focused bounded-size guard only if it fits the production-line budget;
  otherwise record the concrete residual risk.

  The spec is marked implemented. No additional string-byte guard was added:
  the workflow already enforces 21 steps and 100 unique approvers, while
  decision-comment and snapshotted display strings remain a documented
  residual storage risk. The binding fixed-base audit after the newest review
  wave is `+1499/-523`, including `backend/ent/schema`, so adding a new
  production guard would exceed the complexity limit.

- [ ] **Step 5: Verify, report, commit, push, and wait for PR checks**

  Run the requested focused/full suites and exact audits, keep browser
  verification unchecked, write the final-review report, push PR 146, and wait
  for every final-head check without merging or releasing.

  Local verification completed on 2026-07-16:

  - focused backend quota-reset, Work Items, and handler packages passed;
  - backend `go test ./... -count=1` and `go vet ./...` passed;
  - `ae-cli` `go test ./... -count=1` passed;
  - frontend passed 39 files / 435 tests and the production build;
  - role E2E passed 16/16 after starting its required Vite server;
  - the earlier moving-base audit is superseded by the binding fixed-base
    `+1499/-523` result, including `backend/ent/schema`;
  - the in-app browser still reports `No browser is available`, so the browser
    workflow checkbox remains intentionally open.

---

### Task 6: Reset Audit Atomicity And Candidate Consistency Final Fix

- [x] **Step 1: Add and run focused failing regressions**

  Event-insert hooks proved that reset-start, reset-success, and reset-failure
  state could commit without their audit events. A normalized-email candidate
  passed list/save but resolved to an admin fallback.

- [x] **Step 2: Make reset transitions atomic and align candidate eligibility**

  Start CAS plus retry/start events now share one transaction; success/failure
  CAS plus their terminal event each share one transaction. The relay call stays
  between committed transactions, notifications stay after terminal commit,
  and normalized-email directory matches use the same active exact-membership
  checks across list/save/resolve.

- [x] **Step 3: Pass focused tests and the binding production audit**

  `go test ./internal/quotareset ./internal/workitems ./internal/handler -count=1`
  passed. `git diff --check` passed. The latest fixed-base audit after Step 6
  reports `+1499/-523`, including `backend/ent/schema`.

- [x] **Step 4: Complete full backend verification and report**

  Run `go test ./... -count=1`, `go vet ./...`, append the dated final report,
  and repeat formatting/diff/audit checks. Both full backend commands passed;
  the report records the RED/GREEN evidence and residual concerns. Browser
  verification remains unchecked.

- [x] **Step 5: Review and commit locally**

  Review the staged diff and commit all tracked changes with a Conventional
  Commit message. The fix wave was committed locally as
  `fix(quotareset): close reset audit consistency gaps`. It was not pushed.

- [x] **Step 6: Close reset-start re-review and restore readable formatting**

  A failed initial `reset_started` audit must transition the already-claimed
  request to `approved_reset_failed` with a `reset_failed` event before any
  relay call. Retry-start audit failure remains in its existing failed state.
  Simplify the synthetic webhook test request, restore conventional multiline
  formatting for privacy-sensitive outward payloads and initial workflow event
  metadata, then rerun focused/full backend verification and the binding audit.

  Terminal persistence after a successful external relay call cannot be made
  atomic with PostgreSQL without relay idempotency or durable provider-level
  reconciliation. That pre-existing distributed-outcome recovery remains in
  Deferred below; the implementation must not automatically call relay twice.

  RED returned the injected `reset_started` error and left the initial request
  in `approved_resetting`. GREEN stores `approved_reset_failed` plus one
  `reset_failed` event and makes zero relay calls; retry-start failure still
  rolls back to its existing failed state. Focused quota-reset/work-items/
  handler tests, full backend tests, and `go vet ./...` passed. The final audit
  is `+1499/-523`; generated Ent is `+862/-33`; whitespace and production chain
  scans passed. Frontend and CLI remained unchanged from their prior green
  local/CI runs.

  Final re-review reported no Critical, Important, or Minor findings and marked
  the branch ready to merge. It classified terminal database failure after a
  successful external relay call as the explicitly deferred provider outcome
  recovery risk, not an in-scope blocker; automatic relay replay remains
  prohibited because it can reset newly accumulated usage.

## Deferred

- General audit browsing UI.
- Durable notification retries/outbox.
- Provider-level reset recovery after a process crash.
- Additional code-owned notification presets.
