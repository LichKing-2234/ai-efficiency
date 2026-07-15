# Quota Reset Sequential Approval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` or `superpowers:executing-plans` to
> implement this plan task-by-task. Steps use checkbox syntax for live tracking.

**Status:** Complete. The lean implementation, post-review hardening, full
verification, documentation, and PR update are recorded below.

**Goal:** Add an exact-department first approval, an ordered subscription-group
department chain, prior-approver reuse, actionable notifications, and one final
quota reset without building a generic workflow system.

**Architecture:** Keep the existing quota reset request, event, approver config,
Work Items, and relay reset paths. Add one JSON-backed chain configuration table
and store each request's bounded versioned workflow on the existing request row.
Use `workflow_revision` for compare-and-swap decisions and
`resolved_approver_user_ids` as the current-step query index.

**Tech Stack:** Go, Gin, Ent, PostgreSQL JSONB, Vue 3, TypeScript, Vitest,
TailwindCSS.

## Global Constraints

- Source contract:
  `docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md`.
- Add exactly one business table: `quota_reset_approval_chains`.
- Add no request-node, node-approver, decision, outbox, or template tables.
- Add no generic workflow framework, template DSL, or quota reset Pinia store.
- Resolve only exact departments; never walk to a parent department.
- Preserve version 1 request behavior and public `pending` status.
- Use synthetic identities and groups in tests and docs.
- Generated Ent files are expected and are not counted as hand-written module
  surface.

## Scope And File Budget

The branch diff contains 30 files under `backend/ent`: four hand-written schema
files and 26 generated files. Ent emits entity, mutation, query, predicate, hook,
and migration code for one new business schema plus three extended schemas.
Review scope should not treat every generated file as a separate design module.

**New hand-written production files:**

- `backend/ent/schema/quota_reset_approval_chain.go`
- `backend/internal/quotareset/workflow.go`
- `backend/internal/quotareset/workflow_config.go`
- `backend/internal/quotareset/workflow_service.go`
- `frontend/src/components/quota-reset/QuotaResetDecisionDialog.vue`
- `frontend/src/components/quota-reset/QuotaResetWorkflowTimeline.vue`
- `frontend/src/components/settings/QuotaResetApprovalChainSettings.vue`

**Existing production files extended:**

- `backend/ent/schema/quota_reset_request.go`
- `backend/ent/schema/quota_reset_request_event.go`
- `backend/ent/schema/quota_reset_notification_setting.go`
- generated files under `backend/ent/`
- `backend/internal/quotareset/{errors,types,service,notification}.go`
- `backend/internal/handler/{quota_reset,router}.go`
- `backend/internal/workitems/service.go`
- `frontend/src/api/quotaReset.ts`
- `frontend/src/types/index.ts`
- `frontend/src/i18n.ts`
- `frontend/src/views/QuotaResetView.vue`
- `frontend/src/components/quota-reset/QuotaResetRequestList.vue`
- `frontend/src/components/settings/QuotaResetApprovalSettings.vue`
- `docs/architecture.md`

Tests stay beside these modules. No new backend package, frontend route, or
frontend store is required.

---

### Task 1: Persist A Compact Versioned Workflow

**Produces:**

```go
type Workflow struct {
    Version     int
    CurrentStep int
    Requester   WorkflowPerson
    Steps       []WorkflowStep
}

func EncodeWorkflow(*Workflow) (map[string]any, error)
func DecodeWorkflow(map[string]any) (*Workflow, error)
func (*Workflow) Decide(WorkflowDecisionInput) (WorkflowTransition, error)
func (*Workflow) ActiveApproverUserIDs() []int
```

- [x] **Step 1: Add failing pure transition tests**

  Cover active-step approval, rejection, self-approval, non-candidates, admin
  fallback, prior-actor reuse, terminal state, malformed JSON, 21-step limit,
  and 100-approver limit.

  ```bash
  cd backend
  go test ./internal/quotareset -run '^TestWorkflow' -count=1
  ```

- [x] **Step 2: Implement the pure workflow state machine**

  Keep it independent of Ent, Gin, and notification code. A transition returns
  activated/satisfied step indexes and terminal approval/rejection facts.

- [x] **Step 3: Add request fields, event values, explicit channel, and chain schema**

  Add `workflow_version`, nullable `workflow`, `workflow_revision`, internal
  `workflow_pending`, workflow event enum values, notification `channel`, and
  the one `QuotaResetApprovalChain` schema.

  ```bash
  cd backend
  go generate ./ent
  go test ./internal/quotareset -run 'TestWorkflow|TestSchema' -count=1
  ```

- [x] **Step 4: Commit the compact model**

  Commit recorded on the branch as `feat(quotareset): add compact approval
  workflow state`.

---

### Task 2: Resolve Exact Departments And Ordered Chains

**Produces:**

```go
func (s *Service) ListApprovalChains(context.Context) (*ApprovalChainListResponse, error)
func (s *Service) SaveApprovalChains(context.Context, int, []ApprovalChainInput) (*ApprovalChainListResponse, error)
func (s *Service) resolveWorkflowSnapshot(context.Context, *ent.User, int, string) (*Workflow, []DepartmentPathEvidence, error)
```

- [x] **Step 1: Add failing resolution and validation tests**

  Cover exact-department config, per-department representative fallback,
  multi-department merge, no parent walk, configured non-representative members,
  requester exclusion, ordered chain steps, admin fallback, stale sources,
  duplicate groups/departments, non-subscription groups, and the 20-department
  limit.

  ```bash
  cd backend
  go test ./internal/quotareset -run 'TestResolveWorkflow|TestApprovalChain|TestApproverCandidate' -count=1
  ```

- [x] **Step 2: Implement request-time resolution in one repeatable-read snapshot**

  Reuse the current Directory Sync facts, existing approver config, and relay
  group listing. Snapshot requester and approver display/WeCom identity into the
  workflow.

- [x] **Step 3: Add admin chain routes and directory-backed candidates**

  Add `GET/PUT /api/v1/admin/quota-reset/approval-chains`; return
  `current_directory_source_id`; allow active matched department members in the
  approver dropdown while retaining representative flags.

  ```bash
  cd backend
  go test ./internal/handler -run 'TestQuotaReset.*ApprovalChain|TestQuotaReset.*ApproverCandidate' -count=1
  ```

- [x] **Step 4: Commit resolver and config work**

  Commit recorded as `feat(quotareset): resolve department approval chains`.

---

### Task 3: Execute Decisions, Notifications, And Reset

**Consumes:** `Workflow`, request-time snapshot, existing relay resetter, existing
request event table.

- [x] **Step 1: Add failing service transaction tests**

  Cover V2 creation, active-candidate replacement, required comments, one
  decision per step, prior-actor reuse, rejection, admin active-step fallback,
  compare-and-swap conflict, one final reset, retry ownership, and V1 behavior.

  ```bash
  cd backend
  go test ./internal/quotareset -run 'TestCreateRequestV2|TestApproveV2|TestRejectV2|TestConcurrent|TestRetryReset|TestApproveV1' -count=1
  ```

- [x] **Step 2: Implement transactional V2 creation and decisions**

  Write request plus initial events atomically. Update decisions only where
  `status = workflow_pending` and `workflow_revision` matches; commit before
  relay or webhook calls.

- [x] **Step 3: Add explicit generic and WeCom notification rendering**

  Reuse the existing notifier. Include requester/team/group/reason/progress,
  mention only snapshotted `wecom_userid` values, skip auto-satisfied steps, and
  keep test messages synthetic.

- [x] **Step 4: Keep current-step lists and Work Items actionable**

  Publicly map `workflow_pending` to `pending`; include both stored pending
  statuses in user/admin counts and current approval lists.

  ```bash
  cd backend
  go test ./internal/quotareset ./internal/workitems ./internal/handler -count=1
  ```

- [x] **Step 5: Commit service and notification work**

  Commits recorded as `feat(quotareset): execute sequential approval decisions`
  and `feat(quotareset): notify active approval steps`.

---

### Task 4: Extend Existing Frontend Surfaces

**Consumes:** Existing quota reset API/view state plus chain/workflow response
types. `QuotaResetView` remains the network-state owner.

- [x] **Step 1: Add failing settings tests**

  Cover group selection, department and member filtering inside opened
  dropdowns, ordered add/remove/move, replacement payloads, disabled-chain
  preservation, explicit channel save, and API failures.

- [x] **Step 2: Extend settings without a new store or nested card layout**

  Keep department approvers, group chains, and notification channel as three
  subsections of `QuotaResetApprovalSettings.vue`. Use backend display labels and
  ids only as submitted values.

- [x] **Step 3: Add failing approval workbench tests**

  Cover step progress, comments, prior-approval satisfaction, required decision
  dialog, processed history, and action visibility only for current candidates.

- [x] **Step 4: Add the small decision dialog and timeline components**

  Keep fetching and refresh behavior in `QuotaResetView`; child components emit
  decisions only.

- [x] **Step 5: Run focused frontend verification and commit**

  ```bash
  cd frontend
  npm test -- src/__tests__/quota-reset-api.test.ts \
    src/__tests__/quota-reset-approval-settings.test.ts \
    src/__tests__/quota-reset-view.test.ts
  npm run build
  ```

  Commit recorded as `feat(frontend): show sequential quota reset approvals`.

---

### Task 5: Harden Failure And Rollout Boundaries

- [x] **Step 1: Reject terminal or malformed service-layer workflow cursors**

  The new test first reproduced an index-out-of-range panic; the service now
  returns `ErrInvalidStatus` before reading the active step.

- [x] **Step 2: Bound detached relay reset execution**

  A blocking-provider test first failed because caller cancellation was removed
  with no replacement deadline. The service now applies a 30-second relay-call
  timeout and persists timeout failure using the detached base context.

- [x] **Step 3: Keep internal status and third-party messages out of webhooks/audit**

  Generic payload tests require public `pending`. Business-error tests require
  only numeric `errcode`; untrusted third-party `errmsg` is not persisted.

- [x] **Step 4: Preserve rollback-safe active-request uniqueness**

  Keep the original partial unique index predicate unchanged and add the named
  `quotaresetrequest_workflow_active_unique` index spanning both pending states.
  A generated-schema test locks both predicates.

- [x] **Step 5: Run focused hardening regression**

  ```bash
  cd backend
  go test ./internal/quotareset ./internal/workitems -count=1
  ```

  Evidence: both packages passed after the four fixes.

- [x] **Step 6: Re-run handler regression after the final hardening changes**

  ```bash
  cd backend
  go test ./internal/handler -run '^TestQuotaReset' -count=1
  ```

  Evidence: focused quota reset handler tests passed after the final hardening
  changes; the full handler package remains covered by Task 6's backend run.

- [x] **Step 7: Keep processed history readable beyond the first API page**

  A frontend test first proved that the 101st processed approval was hidden.
  `QuotaResetView` now reuses the existing `page` / `page_size` contract in
  100-row chunks for each queue before applying local status filters. No API,
  component, route, or store was added.

  ```bash
  cd frontend
  npm test -- src/__tests__/quota-reset-view.test.ts
  npm test
  npm run build
  npm run test:e2e:role
  ```

  Evidence: the focused view tests passed 11/11, Vitest passed 39 files / 435
  tests, the production build passed, and role E2E passed 16/16 with Vite
  running on `127.0.0.1:5173`.

---

### Task 6: Documentation, Full Verification, And PR Update

- [x] **Step 1: Synchronize current architecture documentation**

  Update `docs/architecture.md` with the one-table/request-JSON model, exact
  department rules, dual pending-state indexes, bounded relay reset, and explicit
  webhook channels. Do not rewrite the historical 2026-07-07 spec.

- [x] **Step 2: Run full backend and CLI verification**

  ```bash
  cd backend
  go test ./... -count=1
  go vet ./...
  cd ../ae-cli
  go test ./... -count=1
  ```

  Expected: all commands exit 0.

  Evidence: all backend packages passed, `go vet ./...` exited 0, and all ae-cli
  packages passed.

- [x] **Step 3: Run full frontend verification**

  ```bash
  cd frontend
  npm test
  npm run build
  npm run test:e2e:role
  ```

  Expected: unit tests, production build, and all role E2E scenarios pass.

  Evidence: Vitest passed 39 files / 435 tests, the production build completed,
  and role E2E passed 16/16.

- [x] **Step 4: Browser-test one complete built workflow**

  Evidence from the isolated Compose run on `127.0.0.1:28081`: Bob approved the
  initial step, Bob's later matching step displayed as automatically satisfied,
  Carol approved the final step, the request reached
  `approved_reset_succeeded`, the fake relay recorded exactly one reset, and the
  mobile page had no horizontal overflow. Screenshots are outside the repository
  under `/tmp/ae-quota-reset-lean-real/`.

- [x] **Step 5: Audit the final diff**

  ```bash
  git diff --check
  git diff --stat origin/main
  git diff --name-only origin/main | sort
  rg -n 'request_node|node_approver|decision_table|Pinia' \
    docs/superpowers/specs/2026-07-10-multi-stage-quota-reset-approval-design.md \
    backend/internal/quotareset frontend/src
  ```

  Confirm one new business schema, expected Ent generation, no generic workflow
  tables/framework, no quota reset store, and no real identity or secret data.

  Evidence: `git diff --check` passed. The branch changes 62 files: four
  hand-written Ent schemas, 26 generated Ent files, 17 backend files, 12
  frontend files, and three documents. Hand-written production code is
  `+2351/-200` across 23 files. The scan found no generic workflow tables/store,
  real identity data, or committed secret.

- [x] **Step 6: Commit review fixes and documentation**

  ```bash
  git add backend frontend docs
  git commit -m "fix(quotareset): harden sequential approval workflow"
  ```

- [x] **Step 7: Push the feature branch and update PR 146**

  Push only after all verification steps pass. Do not merge, release, or run a
  Helm rollout without a separate explicit request.

## Deferred Work

- General audit browsing UI.
- Durable notification delivery retries/outbox.
- Provider-level exactly-once reset after a process crash.
- Additional code-owned notification channel presets.
