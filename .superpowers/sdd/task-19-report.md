# Task 19 Report: Keep Approval Queues and Counts Actionable by Default

## Status

Implemented and committed after local verification. Plan Steps 1-3 are checked.
Step 4 remains unchecked for controller-managed reviews, final browser/Compose
verification, and whole-branch reviews.

## RED Evidence

1. Command:
   `cd backend && go test ./internal/quotareset -run 'Test(ListApprovalsKeepsV2DecisionHistoryBehindExplicitScope|ListApprovalsReturnsRejectedV2DecisionHistoryOnlyWithExplicitScope|ListApprovalsPreservesLegacyV1SemanticsWithHistoryScope|CountWorkItemsAdminUsesAllPendingWithoutDoubleCounting|RetryResetWorkflowWrapsLookupErrorsWithoutLosingNotFoundClassification)$' -count=1`

   Output: failed to compile because `ListParams` did not have `Scope`
   (three `unknown field Scope in struct literal` errors).

2. Command:
   `cd backend && go test ./internal/handler -run '^TestQuotaResetApprovalListPassesExplicitHistoryScope$' -count=1`

   Output: failed to compile because `ListParams.Scope` was undefined in the
   handler regression.

3. Command:
   `cd frontend && npm test -- src/__tests__/quota-reset-api.test.ts src/__tests__/quota-reset-view.test.ts`

   Output: API tests passed; the new view regression failed because the last
   `listQuotaResetApprovals` call received `[]`, not
   `[{ scope: 'history' }]`.

## GREEN Evidence

- `cd backend && go test ./internal/quotareset -run 'Test(ListApprovalsReturnsOnlyActiveV2Assignments|ListApprovalsKeepsV2DecisionHistoryBehindExplicitScope|ListApprovalsReturnsRejectedV2DecisionHistoryOnlyWithExplicitScope|ListApprovalsPreservesLegacyV1SemanticsWithHistoryScope)$' -count=1`: passed.
- `cd backend && go test ./internal/handler -run '^TestQuotaResetApprovalListPassesExplicitHistoryScope$' -count=1`: passed.
- `cd backend && go test ./internal/quotareset -run 'Test(ListApprovalsKeepsV2DecisionHistoryBehindExplicitScope|ListApprovalsReturnsRejectedV2DecisionHistoryOnlyWithExplicitScope|ListApprovalsPreservesLegacyV1SemanticsWithHistoryScope|CountWorkItemsAdminUsesAllPendingWithoutDoubleCounting|RetryResetWorkflowWrapsLookupErrorsWithoutLosingNotFoundClassification)$' -count=1`: passed.
- `cd frontend && npm test -- src/__tests__/quota-reset-api.test.ts src/__tests__/quota-reset-view.test.ts`: 2 files, 30 tests passed.

## Full Verification

- `cd backend && go test ./internal/quotareset ./internal/handler -count=1`: passed.
- `cd backend && go test ./...`: exit 0.
- `cd backend && go vet ./...`: exit 0.
- `cd frontend && npm test`: 40 files, 527 tests passed.
- `cd frontend && npm run build`: exit 0; `vue-tsc -b && vite build` completed.
- `gofmt -d` over all changed Go files: no output.
- `git diff --check`: exit 0.

## Changed Files

- Backend: quota-reset list scope, handler query parsing, admin count predicate,
  retry lookup error context, and regressions.
- Frontend: typed history scope API, isolated processed-history state, and API/view regressions.
- Docs: current quota-reset spec and live execution plan.

## Self-Review and Concerns

- Confirmed default v2 approvals only include active-node assignments and retry
  actor assignments; explicit history is decision-actor-only and does not affect
  badges or default action controls.
- Confirmed v2 processed history retains overall-pending requests, while
  legacy/v1 and mine/admin processing stays status-based.
- Confirmed v2 admin pending excludes self-request while failed-reset retries,
  including self-owned failures, remain countable; v1 count behavior is
  unchanged.
- No Compose, Helm, release, tag, merge, push, browser, or controller-managed
  review was run. Existing Task 18 residuals remain unchanged.
