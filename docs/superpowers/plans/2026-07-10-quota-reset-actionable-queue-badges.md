# Quota Reset Actionable Queue Badges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make quota-reset approval queue badges show actionable work rather than all historical requests, while keeping the requester badge as the historical request total.

**Architecture:** Keep the existing unfiltered queue lists so users can browse history and use the status filters. Source the two approval badge values from the shared `workItems` Pinia store, whose backend contract already counts `pending` and `approved_reset_failed` requests consistently with the sidebar and Work Items page. Refresh counts independently from queue history, hide unavailable approval badges, and generation-scope the store so late responses cannot cross authenticated sessions.

**Tech Stack:** Vue 3, Pinia, TypeScript, Vitest, Vue Test Utils

---

**Status:** Complete. Implementation, code review, full frontend verification, production build, browser acceptance, and the final branch commit are complete.

### Code Review Follow-up

- [x] Evaluate review findings against the current loading and store contracts.
- [x] Add regressions for a delayed count response, a rejected count response, the exact successful-history acceptance case, and stale responses across logout/login.
- [x] Decouple supplemental badge loading from core queue history loading.
- [x] Reset and generation-scope shared counts across authenticated sessions.
- [x] Queue one post-action count fetch after any in-flight pre-action request.
- [x] Re-run focused, full, build, browser, and diff verification.

### Task 1: Lock the badge semantics with a regression test

**Files:**
- Modify: `frontend/src/__tests__/quota-reset-view.test.ts`

- [x] **Step 1: Mock the shared work-item counts API**

Add a `@/api/workItems` mock and return actionable counts separately from quota-reset list totals:

```ts
vi.mock('@/api/workItems', () => ({
  getWorkItemCounts: vi.fn(),
}))

api.getWorkItemCounts.mockResolvedValue({
  data: {
    data: {
      quota_reset_approval_count: 2,
      quota_reset_admin_count: 3,
      ai_access_setup_count: 0,
      offboarding_count: 0,
      total_count: 3,
    },
  },
})
```

- [x] **Step 2: Replace the historical-total badge assertion**

Keep the requester list total at `4`, set the approval and admin list totals to historical values `7` and `12`, and assert that the approval badges render actionable values `2` and `3` instead:

```ts
expect(wrapper.get('[data-testid="quota-reset-tab-mine-count"]').text()).toBe('4')
expect(wrapper.get('[data-testid="quota-reset-tab-approvals-count"]').text()).toBe('2')
expect(wrapper.get('[data-testid="quota-reset-tab-admin-count"]').text()).toBe('3')
```

- [x] **Step 3: Run the focused test and verify RED**

Run:

```bash
cd frontend && npm test -- src/__tests__/quota-reset-view.test.ts
```

Expected: FAIL because the view still renders approval and admin list totals `7` and `12`.

### Task 2: Use shared actionable counts in the quota-reset workbench

**Files:**
- Modify: `frontend/src/views/QuotaResetView.vue`
- Modify: `frontend/src/stores/workItems.ts`
- Modify: `frontend/src/stores/auth.ts`
- Test: `frontend/src/__tests__/quota-reset-view.test.ts`
- Test: `frontend/src/__tests__/work-items-store.test.ts`
- Test: `frontend/src/__tests__/auth-store.test.ts`

- [x] **Step 1: Bind approval badges to the work-items store**

Import and instantiate `useWorkItemsStore`, replace the approval/admin total refs with computed values, and keep `myTotal` backed by the requester list response:

```ts
import { useWorkItemsStore } from '@/stores/workItems'

const workItems = useWorkItemsStore()
const approvalTotal = computed(() => workItems.loading || workItems.error ? 0 : workItems.counts.quota_reset_approval_count)
const adminTotal = computed(() => workItems.loading || workItems.error || !auth.isAdmin ? 0 : workItems.counts.quota_reset_admin_count)
```

- [x] **Step 2: Refresh shared counts with queue data**

Call `workItems.loadCounts({ force: forceCounts })` inside `loadQueues()` without adding it to the list `Promise.all`. Initial navigation and every post-action reload still update the workbench and sidebar, while slow or failed supplemental counts cannot block queue history. Post-action calls set `forceCounts` so one fresh request runs after an already in-flight pre-action request.

- [x] **Step 3: Run the focused test and verify GREEN**

Run:

```bash
cd frontend && npm test -- src/__tests__/quota-reset-view.test.ts
```

Expected: PASS with the requester badge showing history and approval badges showing actionable work.

### Task 3: Synchronize the active contract and verify the frontend

**Files:**
- Modify: `docs/superpowers/specs/2026-07-07-quota-reset-approval-design.md`
- Modify: `docs/superpowers/plans/2026-07-07-quota-reset-approval.md`
- Modify: `docs/superpowers/plans/2026-07-10-quota-reset-actionable-queue-badges.md`

- [x] **Step 1: Update the quota-reset workbench contract**

Document that `My Requests` shows its historical list total while `Approvals` and `All Requests` use the actionable counts from `/api/v1/work-items/counts` (`pending` plus `approved_reset_failed`). Replace the obsolete plan statement that all three badges use list totals.

- [x] **Step 2: Run focused and full frontend verification**

Run:

```bash
cd frontend && npm test -- src/__tests__/quota-reset-view.test.ts src/__tests__/work-items-store.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/work-items-view.test.ts
npm test
npm run build
git diff --check
```

Expected: all tests and the production build pass; `git diff --check` reports no errors.

- [x] **Step 3: Verify the rendered page in a browser**

Use an admin fixture with one successful historical request and zero actionable admin requests. Confirm `My Requests` shows `1`, `Admin queue` has no badge, and the successful row remains available under the `All` filter.

- [x] **Step 4: Commit the fix**

```bash
git add frontend/src/views/QuotaResetView.vue frontend/src/stores/workItems.ts frontend/src/stores/auth.ts frontend/src/__tests__/quota-reset-view.test.ts frontend/src/__tests__/work-items-store.test.ts frontend/src/__tests__/auth-store.test.ts docs/superpowers/specs/2026-07-07-quota-reset-approval-design.md docs/superpowers/plans/2026-07-07-quota-reset-approval.md docs/superpowers/plans/2026-07-10-quota-reset-actionable-queue-badges.md
git commit -m "fix(frontend): show actionable quota reset queue badges"
```
