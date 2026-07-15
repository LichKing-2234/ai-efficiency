# Quota Reset Queue Loading Performance Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans and superpowers:test-driven-development. Update this ledger immediately after each verified step.

**Status:** Complete. The implementation and review follow-ups are locally verified, branch `perf/quota-reset-131` is published, and draft PR #150 targets `docs/performance-contracts-116`. GitHub CI is pending.

**Goal:** Open `/usage/quota-reset` with only the active requester queue on the critical path, while preserving authoritative quota-reset mutations and immediately refreshed Work Items counts.

**Architecture:** Keep queue orchestration in `QuotaResetView.vue` because it is route-local state over the existing `frontend/src/api/quotaReset.ts` boundary. Give `mine`, `approvals`, and `admin` independent idle/loading/loaded/error state, request identity, in-flight deduplication, and action state. Loaded results are reused until explicit refresh or a related mutation invalidates them. Refresh and invalidation clear rows before the authoritative request, and no queue uses stale-if-error.

**Constraints:**

- Mount requests only `mine` and the existing shared Work Items counts.
- Approvals and admin load on first selection; admin is never requested for a non-admin.
- A hidden queue's delay, error, refresh, or mutation action cannot replace or block the active queue.
- Explicit refresh reloads only the active queue and clears its prior rows before the request.
- A successful mutation refreshes its source queue, invalidates only overlapping queues, and force-refreshes Work Items counts.
- A failed mutation leaves queues and counts unchanged. A successful mutation followed by a read failure remains a successful mutation but exposes an empty error state for that queue.
- Tests and docs use synthetic identities only.

---

### Task 1: Route Contract Tests

**Files:**
- Modify: `frontend/src/__tests__/quota-reset-view.test.ts`

- [x] **Step 1: Add mount and role-dependent request tests**

  Assert mount requests only `mine`, approvals loads on first selection, repeated visits reuse it, admin loads only after an admin selects it, and non-admins have neither an admin tab nor an admin request.

- [x] **Step 2: Add independent queue lifecycle tests**

  Assert a delayed hidden queue does not block visible rows, a hidden rejection does not replace visible content, repeated selections deduplicate an in-flight request, and explicit refresh reloads only the active queue.

- [x] **Step 3: Add authoritative refresh and mutation invalidation tests**

  Assert refresh clears stale rows and exposes the active queue error on failure. Cover cancel, approver approve/reject/retry, and admin approve/reject/retry so only the source queue reloads immediately, overlapping queues become idle for their next visit, Work Items counts force-refresh exactly once, and a tab switch during mutation does not block the newly active queue.

- [x] **Step 4: Run the focused route test and record RED**

  Run: `cd frontend && npm test -- quota-reset-view.test.ts`

  Expected RED: current mount eagerly calls approval/admin queues, one global loading/error state couples hidden queues to visible content, no active-only refresh exists, and mutations reload every queue.

  RED evidence: `npm test -- quota-reset-view.test.ts` ran 20 tests with 14 expected failures covering eager hidden requests, coupled loading/error state, missing refresh control, and all-queue mutation reloads.

---

### Task 2: Independent On-Demand Queue State

**Files:**
- Modify: `frontend/src/views/QuotaResetView.vue`
- Modify: `frontend/src/i18n.ts`

- [x] **Step 1: Implement queue-local lifecycle and request identity**

  Add queue-local status, items, total, error, and action state. Deduplicate repeated selections through one active promise per queue and ignore superseded responses with generation/request identity.

- [x] **Step 2: Implement active-only selection and refresh**

  Mount `mine`, load an idle queue when selected, reuse loaded/error state on later visits, and add an explicit refresh command for the active queue. Every forced or invalidated read clears prior rows before requesting.

- [x] **Step 3: Implement mutation-scoped invalidation**

  Capture the source queue when an action starts. On success, force-refresh that queue, invalidate only overlapping queues, and invalidate plus force-refresh shared Work Items counts. Keep action state local to the source queue so a later active queue remains interactive.

- [x] **Step 4: Run the focused route test and record GREEN**

  Run: `cd frontend && npm test -- quota-reset-view.test.ts`

  GREEN evidence: 1 file and 20 route tests passed.

---

### Task 3: Current Contract Documentation

**Files:**
- Modify: `docs/superpowers/specs/2026-07-07-quota-reset-approval-design.md`
- Modify: `docs/architecture.md`
- Modify: this plan

- [x] **Step 1: Document the landed queue contract**

  Record independent on-demand queues, explicit active-only refresh, no stale-if-error, scoped mutation invalidation, and supplemental Work Items behavior in the current quota-reset spec.

- [x] **Step 2: Update project architecture**

  Update the current AI Usage frontend task-zone description with the landed route lifecycle; leave historical specs intact.

- [x] **Step 3: Run documentation/diff checks**

  Run: `git diff --check`

  Evidence: PASS after the current contract and architecture updates.

---

### Task 4: Verification And Delivery

- [x] **Step 1: Run the full frontend unit suite**

  Run: `cd frontend && npm test`

  Evidence: 39 files and 457 tests passed.

- [x] **Step 2: Run the production frontend build**

  Run: `cd frontend && npm run build`

  Evidence: `vue-tsc -b && vite build` passed with 188 modules transformed.

- [x] **Step 3: Run role regression and repository checks**

  Run: `cd frontend && npm run test:e2e:role`

  Run: `git diff --check && git status --short`

  Evidence: the first role run failed only because no process listened on the script's fixed `localhost:5173` prerequisite. After starting this worktree's Vite server, the rerun passed 16/16 role checks. Final repository checks are repeated after review.

- [x] **Step 4: Address code review findings with focused RED/GREEN tests**

  Detach supplemental Work Items refresh completion from source-queue loading, so slow counts do not hide refreshed history. Remove `approvals` from cancellation invalidation because backend approver resolution excludes the requester. Each focused regression failed before its fix and passed afterward.

- [x] **Step 5: Re-run fresh post-review verification**

  Re-run the focused route file, full frontend unit suite, production build, role regression with its Vite prerequisite, and final diff/status checks after all review fixes.

  Evidence: focused route tests passed 20/20; the full suite passed 39 files and 457 tests; `vue-tsc -b && vite build` passed; role regression passed 16/16 with Vite running; final diff/status checks are run immediately before commit.

- [x] **Step 6: Commit, publish, and open a draft PR**

  Commit with Conventional Commits, push `perf/quota-reset-131`, and create a draft PR targeting `docs/performance-contracts-116` with issue, test, dependency, and no-release notes. Keep this worktree alive for review.

  Evidence: implementation commit `683cfbb` is published and draft PR #150 is open with the required base, issue, verification, dependency, and no-release notes.
