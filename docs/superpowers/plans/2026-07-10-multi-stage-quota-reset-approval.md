# Department-Derived Quota Reset Approval Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for live tracking.

**Status:** Ready for implementation. This replaces the rejected
subscription-group approval-chain plan; all checkboxes are intentionally reset.

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
- Keep hand-written production additions at or below 1,500 lines versus
  `origin/main`, excluding tests and generated Ent code.
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

- [ ] **Step 1: Update current architecture**

  Document exact-department config/representative fallback, configured ancestor
  rounds, same-round merge, admin fallback, request-time snapshot, prior-actor
  reuse, CAS/event audit, and explicit generic/WeCom channels. State that the
  subscription group affects only the reset target.

- [ ] **Step 2: Run full automated verification**

  ```bash
  cd backend && go test ./... -count=1 && go vet ./...
  cd ../ae-cli && go test ./... -count=1
  cd ../frontend && npm test && npm run build && npm run test:e2e:role
  ```

  Expected: every command exits 0.

- [ ] **Step 3: Audit final scope**

  ```bash
  git diff --check origin/main
  git diff --numstat origin/main -- backend/internal backend/ent/schema frontend/src \
    ':(exclude)**/*_test.go' ':(exclude)frontend/src/__tests__/**' \
    | awk '{ a += $1; d += $2 } END { print "handwritten production +" a "/-" d }'
  git diff --numstat origin/main -- backend/ent ':(exclude)backend/ent/schema/**' \
    | awk '{ a += $1; d += $2 } END { print "generated Ent +" a "/-" d }'
  ! rg -n 'QuotaResetApprovalChain|approval-chains|quota_reset_approval_chains' \
    backend frontend docs/architecture.md
  git status --short
  ```

  Expected: no whitespace errors; hand-written additions are at most 1,500;
  chain scan is empty; only intentional branch files are modified. Simplify and
  leave this step unchecked if the limit is exceeded.

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

- [ ] **Step 5: Commit docs, push, and watch PR 146**

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

## Deferred

- General audit browsing UI.
- Durable notification retries/outbox.
- Provider-level reset recovery after a process crash.
- Additional code-owned notification presets.
