# Lean Multi-Stage Quota Reset Approval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` or `superpowers:executing-plans` to
> implement this plan task-by-task. Steps use checkbox syntax for live tracking.

**Status:** In progress. The replacement spec is complete; implementation has
not started.

**Goal:** Extend the existing quota reset feature with one exact-department
approval step, an ordered subscription-group department chain, prior-approver
reuse, durable comments, and actionable notifications.

**Architecture:** Keep the existing request, event, approver config, Work Items,
and relay reset paths. Add one JSON-backed chain config table and store each
request's bounded workflow in the existing request row with a compare-and-swap
revision. Continue using `resolved_approver_user_ids` as the active-step index.

**Tech Stack:** Go, Gin, Ent, PostgreSQL JSONB, Vue 3, TypeScript, Vitest,
TailwindCSS.

## Global Constraints

- The source contract is
  `docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md`.
- Add exactly one business table: `quota_reset_approval_chains`.
- Do not add request-node, node-approver, or decision tables.
- Do not add a generic workflow framework, notification outbox, or template DSL.
- Do not add a quota reset Pinia store; extend the current view/API flow.
- Keep version 1 request behavior operational.
- Use synthetic identities such as `alice@example.com` in tests and docs.
- If hand-written production additions exceed 4,000 lines, stop and reduce the
  design before continuing.

## File Map

**Create:**

- `backend/ent/schema/quota_reset_approval_chain.go` - one row per provider/group,
  with an ordered JSON department list.
- `backend/internal/quotareset/workflow.go` - bounded version 2 workflow types,
  validation, authorization, and pure transitions.
- `backend/internal/quotareset/workflow_test.go` - pure workflow transition tests.
- `backend/internal/quotareset/workflow_service.go` - version 2 request creation
  and compare-and-swap decision transactions.
- `backend/internal/quotareset/workflow_config.go` - chain discovery, validation,
  replacement, and request-time resolution.
- `backend/internal/quotareset/workflow_config_test.go` - chain/resolver tests.
- `frontend/src/components/quota-reset/QuotaResetDecisionDialog.vue` - required
  decision comment dialog.
- `frontend/src/components/quota-reset/QuotaResetWorkflowTimeline.vue` - compact
  workflow progress and comment history.

**Modify:**

- `backend/ent/schema/quota_reset_request.go`
- `backend/ent/schema/quota_reset_request_event.go`
- `backend/ent/schema/quota_reset_notification_setting.go`
- generated files under `backend/ent/`
- `backend/internal/quotareset/types.go`
- `backend/internal/quotareset/resolver.go`
- `backend/internal/quotareset/service.go`
- `backend/internal/quotareset/notification.go`
- focused tests beside those files
- `backend/internal/handler/quota_reset.go`
- `backend/internal/handler/router.go`
- `frontend/src/api/quotaReset.ts`
- `frontend/src/types/index.ts`
- `frontend/src/components/settings/QuotaResetApprovalSettings.vue`
- `frontend/src/components/quota-reset/QuotaResetRequestList.vue`
- `frontend/src/views/QuotaResetView.vue`
- `frontend/src/i18n.ts`
- existing focused frontend tests
- `docs/architecture.md`

No Work Items backend change is planned: it already queries
`resolved_approver_user_ids`, which version 2 keeps synchronized to the active
step.

---

### Task 1: Add The Compact Workflow Model

**Interfaces:**

```go
type Workflow struct {
    Version     int              `json:"version"`
    CurrentStep int              `json:"current_step"`
    Requester   WorkflowPerson   `json:"requester"`
    Steps       []WorkflowStep   `json:"steps"`
}

type WorkflowStep struct {
    Kind                  string             `json:"kind"`
    Label                 string             `json:"label"`
    DepartmentExternalIDs []string           `json:"department_external_ids"`
    Approvers             []WorkflowApprover `json:"approvers"`
    AdminFallback         bool               `json:"admin_fallback"`
    Status                string             `json:"status"`
    Decision              *WorkflowDecision  `json:"decision,omitempty"`
    SatisfiedByStep       *int               `json:"satisfied_by_step,omitempty"`
}

func DecodeWorkflow(raw map[string]any) (*Workflow, error)
func (w *Workflow) ActiveApproverUserIDs() []int
func (w *Workflow) Decide(input WorkflowDecisionInput) (WorkflowTransition, error)
```

- [x] **Step 1: Add failing pure transition tests**

  Add table-driven tests proving one approval advances one step, rejection ends
  the workflow, the requester is rejected, a non-candidate is rejected, a prior
  actor satisfies all matching later steps, and malformed/oversized documents
  fail closed.

  Run:

  ```bash
  cd backend && go test ./internal/quotareset -run 'TestWorkflow' -count=1
  ```

  Expected: compile failure because the workflow API does not exist.

- [x] **Step 2: Implement the smallest pure workflow state machine**

  Keep transition code free of Ent and HTTP dependencies. Limit documents to 21
  steps and 100 unique approvers. Return transition facts needed by the service:
  activated step, automatically satisfied steps, terminal approval/rejection,
  and current approver ids.

- [x] **Step 3: Verify the pure tests pass**

  Run the Task 1 test command. Expected: PASS.

- [x] **Step 4: Add the schema fields and generate Ent code**

  Add `workflow_version`, nullable `workflow`, and `workflow_revision` to
  `QuotaResetRequest`; add the five workflow event enum values; add explicit
  notification `channel`; create `QuotaResetApprovalChain` with JSON
  `department_chain`.

  Run:

  ```bash
  cd backend && go generate ./ent
  go test ./ent/... ./internal/quotareset -run 'TestWorkflow|TestSchema' -count=1
  ```

  Expected: PASS.

- [x] **Step 5: Commit Task 1**

  ```bash
  git add backend/ent backend/internal/quotareset/workflow.go backend/internal/quotareset/workflow_test.go
  git commit -m "feat(quotareset): add compact approval workflow state"
  ```

---

### Task 2: Resolve Exact Departments And Group Chains

**Interfaces:**

```go
type ApprovalChainInput struct {
    ProviderID int                   `json:"provider_id"`
    GroupID    string                `json:"group_id"`
    GroupName  string                `json:"group_name"`
    Enabled    bool                  `json:"enabled"`
    Departments []ChainDepartmentInput `json:"departments"`
}

func (s *Service) ListApprovalChains(ctx context.Context) (*ApprovalChainListResponse, error)
func (s *Service) SaveApprovalChains(ctx context.Context, actorID int, items []ApprovalChainInput) (*ApprovalChainListResponse, error)
func (s *Service) resolveWorkflow(ctx context.Context, requester *ent.User, providerID int, groupID string) (*Workflow, []DepartmentPathEvidence, error)
```

- [x] **Step 1: Add failing resolver/config tests**

  Cover exact-department configured candidates; per-department representative
  fallback; multiple direct departments merged into one step; no parent walk;
  configured non-representative members; requester exclusion; ordered chain
  nodes; missing chain approvers using admin fallback; and no-step synthetic
  admin fallback.

  Also test replacement validation: current directory source only, active
  provider/group only, no duplicate group or department, and at most 20 chain
  departments.

  Run:

  ```bash
  cd backend && go test ./internal/quotareset -run 'TestResolveWorkflow|TestApprovalChains|TestApproverCandidates' -count=1
  ```

  Expected: FAIL because the lean resolver/config implementation is absent.

- [x] **Step 2: Implement chain CRUD and request-time resolution**

  Reuse the current Directory Sync snapshot, `QuotaResetApproverConfig`, relay
  provider group listing, and existing department tree helpers. Approver picker
  candidates become active directory-matched members of the chosen department;
  include representative and `has_wecom_userid` flags in responses.

- [x] **Step 3: Add and test admin HTTP routes**

  Extend the handler interface and router with `GET/PUT approval-chains`. Keep
  replacement payload semantics and return readable group/department snapshots.

  Run:

  ```bash
  cd backend && go test ./internal/handler -run 'TestQuotaReset.*ApprovalChain|TestQuotaReset.*ApproverCandidate' -count=1
  ```

  Expected: PASS after first observing the new handler tests fail.

- [ ] **Step 4: Commit Task 2**

  ```bash
  git add backend/internal/quotareset backend/internal/handler backend/internal/handler/router.go
  git commit -m "feat(quotareset): resolve department approval chains"
  ```

---

### Task 3: Integrate Version 2 Requests And Decisions

**Interfaces:** Existing `CreateRequest`, `Approve`, `Reject`, `Cancel`, list,
retry, and reset methods remain the public service boundary.

- [x] **Step 1: Add failing service tests**

  Prove creation snapshots a version 2 workflow; active approver ids follow the
  current step; intermediate approval does not reset; final approval starts one
  reset; comments are mandatory; later matching nodes auto-satisfy; admin acts
  only on the active node; two decisions at one revision yield one winner;
  rejection is terminal; and version 1 requests still use the old path.

  Run:

  ```bash
  cd backend && go test ./internal/quotareset -run 'TestCreateRequestV2|TestApproveV2|TestRejectV2|TestConcurrentDecision|TestVersion1' -count=1
  ```

  Expected: FAIL on missing version 2 behavior.

- [x] **Step 2: Implement transactional creation and decisions**

  Add small transaction helpers local to the quota reset module. Update version
  2 rows with `WHERE status = pending AND workflow_revision = ?`, increment the
  revision, update `resolved_approver_user_ids`, and append transition events in
  the same transaction. Commit before notifications and relay calls.

- [x] **Step 3: Return workflow summaries and current actions**

  Extend request summaries with workflow version/current step/steps. Approval
  lists continue using JSON containment on current approver ids. Processed rows
  remain listable but Work Items counts remain actionable only.

- [x] **Step 4: Run quota reset and Work Items regression tests**

  ```bash
  cd backend && go test ./internal/quotareset ./internal/workitems ./internal/handler -count=1
  ```

  Expected: PASS.

- [x] **Step 5: Commit Task 3**

  ```bash
  git add backend/internal/quotareset backend/internal/handler backend/internal/workitems
  git commit -m "feat(quotareset): execute sequential approval decisions"
  ```

---

### Task 4: Make Notifications Actionable Without A Framework

**Interfaces:** Keep `WebhookNotifier.NotifyRequestEvent`; internally render one
of two explicit channels: `generic_webhook` or `wecom_group_robot`.

- [x] **Step 1: Add failing notification tests**

  Test explicit channel selection, generic JSON fields, WeCom markdown requester
  name/email/team/group/reason/progress, `<@userid>` mentions, missing-id labels,
  no notification for auto-satisfied steps, synthetic test content, business
  error decoding, and redacted errors.

  Run:

  ```bash
  cd backend && go test ./internal/quotareset -run 'TestWebhook|TestWeCom|TestNotificationSettings' -count=1
  ```

  Expected: FAIL on the new channel/context assertions.

- [x] **Step 2: Extend the existing notifier directly**

  Add two focused render functions in `notification.go`; do not add adapter,
  registry, outbox, or template packages. Read workflow snapshots for requester,
  active approvers, progress, and prior decision context. Use only
  `metadata.wecom_userid` for mentions.

- [x] **Step 3: Add explicit channel settings and legacy URL backfill**

  Validate channel/URL/auth combinations. Existing WeCom robot URLs migrate to
  `wecom_group_robot`; other rows remain `generic_webhook`. API responses never
  expose credentials or full secret-bearing error URLs.

- [x] **Step 4: Run focused and full quota reset tests**

  ```bash
  cd backend && go test ./internal/quotareset -count=1
  ```

  Expected: PASS.

- [ ] **Step 5: Commit Task 4**

  ```bash
  git add backend/internal/quotareset backend/internal/handler backend/ent
  git commit -m "feat(quotareset): notify active approval steps"
  ```

---

### Task 5: Extend The Existing Frontend Surfaces

**Interfaces:** API types mirror the backend chain and workflow summaries. The
existing `QuotaResetView` remains data owner; child components only emit actions.

- [ ] **Step 1: Add failing API/settings tests**

  Add focused tests for group dropdowns, department filtering inside an opened
  dropdown, member candidate filtering, ordered add/remove/move chain rows,
  replacement payloads, explicit channel save, and backend error display.

  Run:

  ```bash
  cd frontend && npm test -- src/__tests__/quota-reset-api.test.ts src/__tests__/quota-reset-approval-settings.test.ts
  ```

  Expected: FAIL on missing chain/channel controls.

- [ ] **Step 2: Extend API/types and the existing settings component**

  Keep the three settings subsections in
  `QuotaResetApprovalSettings.vue`. Reuse existing Directory Sync department
  search and approver candidate APIs. Do not create nested settings cards or a
  new state store.

- [ ] **Step 3: Add failing approval workbench tests**

  Test active step/progress, timeline comments, mandatory decision comment,
  cancel/action visibility, processed history, and refreshed pending counts.

  Run:

  ```bash
  cd frontend && npm test -- src/__tests__/quota-reset-view.test.ts
  ```

  Expected: FAIL on missing version 2 presentation.

- [ ] **Step 4: Implement the compact timeline and dialog**

  Add the two small child components listed in the file map. Keep network state
  in `QuotaResetView`; after a decision refresh the active list, mine/admin list
  as applicable, and Work Items counts without clearing successful UI state on
  a secondary refresh failure.

- [ ] **Step 5: Run focused frontend tests and build**

  ```bash
  cd frontend
  npm test -- src/__tests__/quota-reset-api.test.ts src/__tests__/quota-reset-approval-settings.test.ts src/__tests__/quota-reset-view.test.ts src/__tests__/work-items-store.test.ts
  npm run build
  ```

  Expected: PASS and build exit 0.

- [ ] **Step 6: Commit Task 5**

  ```bash
  git add frontend
  git commit -m "feat(frontend): show sequential quota reset approvals"
  ```

---

### Task 6: Documentation, Browser Verification, And Diff Audit

- [ ] **Step 1: Update current architecture documentation**

  Update `docs/architecture.md` to name the one chain config table, request JSON
  workflow, current approver ids, event audit, and explicit webhook channels.
  Do not rewrite the historical 2026-07-07 spec.

- [ ] **Step 2: Run full repository verification**

  ```bash
  cd backend && go test ./... -count=1 && go vet ./...
  cd ../frontend && npm test && npm run build && npm run test:e2e:role
  cd .. && git diff --check origin/main...HEAD
  ```

  Expected: all commands exit 0.

- [ ] **Step 3: Browser-test one complete workflow**

  Against local Compose, verify admin chain/channel save, user request creation,
  initial approver comment, configured-chain comment, prior-actor skip display,
  terminal reset state, processed history, and pending badge changes. Capture
  screenshots outside the repository.

- [ ] **Step 4: Audit final scope before push**

  Run:

  ```bash
  git diff --shortstat origin/main...HEAD
  git diff --numstat origin/main...HEAD | awk '
    $3 !~ /^backend\/ent\// &&
    $3 !~ /^docs\/superpowers\/(plans|specs)\// { add += $1; del += $2 }
    END { print "manual-nondoc", add, del }'
  ```

  Confirm one new business schema, no runtime node/decision tables, no quota
  reset store, and no real user/company data. If hand-written production
  additions exceed the global constraint, reduce before push.

- [ ] **Step 5: Commit docs and plan evidence**

  ```bash
  git add docs/architecture.md docs/superpowers/plans/2026-07-10-multi-stage-quota-reset-approval.md
  git commit -m "docs(architecture): document lean quota reset approvals"
  ```

## Known Deferred Work

- Durable notification retries/outbox.
- Provider-level reset idempotency after a process crash.
- General audit browsing UI.
- Arbitrary admin-authored notification templates.
