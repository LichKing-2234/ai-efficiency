# Loading Performance Contract Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Issue:** [#173](https://github.com/LichKing-2234/ai-efficiency/issues/173)

**Status:** In review. Repository/GitHub reconciliation, the full exact-head verification matrix, and final Standards/Spec review are complete against implementation baseline `feat/platform-loading-performance@d2bc26941d6c966c8117a038b770450f7b201ed4`. PR #187 targets that integration branch; final ledger-head CI, merge, and issue closeout remain pending.

**Goal:** Reconcile the active loading-performance contract, every affected execution ledger, issues #117-#135, and PR #160 with the exact integrated implementation, then revalidate and deliver only to `feat/platform-loading-performance`.

**Architecture:** Treat `feat/platform-loading-performance` as the implementation integration boundary and keep production state separate. Historical implementation plans retain their task evidence but gain a current integration-status header; the active performance spec gains one explicit status boundary for implemented behavior, the temporary compatibility surface, and external production work.

**Tech Stack:** Markdown contracts and ledgers, GitHub issues and pull requests, Go 1.24, Vue 3/Vitest/Vite, ae-cli Go tests, release frontend embed harness.

## Global Constraints

- Work only in `/Users/admin/ai-efficiency/.worktrees/performance-reconciliation-173` on `chore/performance-reconciliation-173`.
- Base every reconciliation statement on the exact `feat/platform-loading-performance` integration history and live GitHub state.
- Preserve historical plan task evidence; change only current status boundaries and checkboxes actually completed in this ticket.
- Do not mark production sampling or the compatibility sunset complete. Those remain #136 and #137.
- Do not merge PR #160 to `main`, release, tag, deploy, sample production, or run Helm.
- Use an owned strict-port Vite listener for role E2E and stop it after verification.

## Integration Ledger

| Issue | Source PR | `feat/platform-loading-performance` integration commit |
| --- | --- | --- |
| #117 | #140 | `1c810bbc9b527b7cd3770767a24bdfa7bc37d0ff` |
| #118 | #143 | `5d5eae063020bc9dbe284f64534263287ec6f264` |
| #119 | #139 | `ff61d8d4230ba70184a33e5c829e226dac51bf63` via #138 |
| #120 | #141 | `108419e11549378c358c5317ed8b82e51d0e64f7` |
| #121 | #145 | `028fd34b539f6420340e44d11234362423a91140` |
| #122 | #144 | `f4b2802d40d92fafc8db8a2825c708aac039b9e8` |
| #123 | #148 | `f234d7d284a7428cc35a6a2d17af4a04ab01090f` |
| #124 | #149 | `9225ed879123e04f3f2c4ed3567cf3ecc8f58948` |
| #125 | #153 | `25cf7d6f23a08cfaf3d432fbe727b8943bf7c945` |
| #126 | #154 | `93b7bcf92ff14ba19da10185b90ff0bf800a0335` |
| #127 | #155 | `1098c709d8b0572ce33515ca773be6aead1baad1` |
| #128 | #156 | `dc1ffd6075991f8e221aaac2890d2f3a38ec036d` |
| #129 | #152 | `8e11aadbbebeea9c9fdba3ebde9fa6d5905065ef` |
| #130 | #157 | `37e43258d6c4473715b76662bfb5c8b24d7a6016` |
| #131 | #150 | `531a01dd4ef9b737c5aa2bb8a4d70e2bd5366746` |
| #132 | #151 | `9101a51a274905c528d9b4355f382ea8a1969d2b` |
| #133 | #142 | `0ee4e0141683b8edcc2b2867543af70c0b694ced` |
| #134 | #147 | `ed446febb43f0fbd7b9a5bbb2da6d1925b9a5c5f` |
| #135 | #158 | `94fda0f76d90d4ab1d0c3afc2614e2131b7fd060` |

The later review remediations #161-#172 are also closed and integrated through PRs #174-#186. The audit baseline for this ticket is the #172 squash merge `d2bc26941d6c966c8117a038b770450f7b201ed4`.

---

### Task 1: Audit The Exact Integration State

**Files:**
- Create: this plan

- [x] **Step 1: Verify the isolated worktree and exact baseline**

  Confirm the worktree is clean on `chore/performance-reconciliation-173` at `d2bc26941d6c966c8117a038b770450f7b201ed4`, while the root checkout's unrelated changes remain untouched.

- [x] **Step 2: Build the source PR and integration mapping**

  Audit issues #117-#135, source PRs #139-#158 excluding unrelated #146, first-parent integration commits, remediation issues #161-#172, and PRs #174-#186.

- [x] **Step 3: Identify stale contracts and ledgers**

  Confirm which plan headers still report draft PR, pending CI, or pending merge state; identify the unchecked #169 full-verification item; and audit the active spec plus GitHub descriptions.

### Task 2: Reconcile Repository Contracts And Plans

**Files:**
- Modify: `docs/superpowers/specs/2026-07-14-end-to-end-page-loading-performance-design.md`
- Modify: affected `docs/superpowers/plans/2026-07-*.md` ledgers
- Modify: this plan

- [x] **Step 1: Add an explicit active-spec status boundary**

  Separate implementation integrated on `feat/platform-loading-performance`, the retained legacy Overview compatibility adapter, prior partial staging evidence, and production work still blocked on a normal release/deployment under #136/#137.

- [x] **Step 2: Reconcile every affected plan header**

  Record the actual source-PR and feat-branch integration state without rewriting historical task evidence. Preserve only real external gaps.

### Task 3: Reconcile GitHub Descriptions

**Files:**
- Update: issues #117-#135 descriptions
- Update: PR #160 description
- Modify: this plan

- [x] **Step 1: Add current integration notes to issues #117-#135**

  Preserve the original acceptance contracts, identify the source PR and integrated state, and explicitly avoid claiming main/release/production completion.

- [x] **Step 2: Update PR #160 to the exact integrated scope**

  Include #161-#172 remediation, current compatibility behavior, and #136/#137 external gates. Remove stale exact-head verification claims tied only to PR #159.

### Task 4: Verify And Review The Exact Reconciliation Head

**Files:**
- Modify: this plan
- Modify: `docs/superpowers/plans/2026-07-18-admin-users-persisted-effective-hierarchy.md`

- [x] **Step 1: Run the full verification matrix**

  ```bash
  git diff --check
  cd backend && go test ./... -count=1 && go vet ./... && go build ./...
  cd ../frontend && npm ci && npm test && npm run build
  # Start an owned strict-port Vite listener, run npm run test:e2e:role, then stop it.
  cd ../ae-cli && go test ./... -count=1
  cd .. && bash deploy/test/release-frontend-embed-test.sh
  ```

  Verification evidence (2026-07-19): `git diff --check`; backend `go test ./... -count=1`, `go vet ./...`, and `go build ./...`; frontend clean `npm ci`, 46 files / 680 tests, production build, and 16/16 role E2E against an owned strict-port `127.0.0.1:5173` Vite listener; ae-cli `go test ./... -count=1`; and `bash deploy/test/release-frontend-embed-test.sh` all passed. The listener was stopped and port 5173 was confirmed free. `npm ci` reported the existing dependency audit/allow-scripts warnings, role E2E emitted expected proxy warnings for unmocked non-asserted APIs because no backend dev server was started, and the embed harness emitted its known synthetic unresolved-hook warning before passing.

- [x] **Step 2: Run final Standards and Spec review**

  Review the exact diff against `AGENTS.md`, issue #173, and the active performance contract. Resolve every Critical or Important finding and rerun affected checks.

  Review evidence (2026-07-19): Standards review found no Critical, Important, or actionable Minor findings. The diff changes only the active status boundary and live execution ledgers, keeps historical implementation evidence intact, uses English consistently with the affected documents, and passes `git diff --check`. Spec review found no Critical, Important, or actionable Minor findings: all 19 recorded integration commits are ancestors of `d2bc2694`; issues #117-#135 remain open/in-review with current source/remediation/delivery boundaries; PR #160 includes #161-#172 and no longer treats PR #159 as the final head; #136/#137 remain open/blocked; compatibility behavior stays implemented; and the exact-head verification matrix is recorded. Only this plan's delivery/closeout steps remain unchecked.

### Task 5: Deliver Only To The Performance Integration Branch

**Files:**
- Modify: this plan

- [x] **Step 1: Commit, push, and open a PR targeting `feat/platform-loading-performance`**

  Use a Conventional Commit. Do not target or merge `main`.

  Delivery evidence (2026-07-19): commit `0aad9000` was pushed on `chore/performance-reconciliation-173`, and non-Draft PR #187 was opened with base `feat/platform-loading-performance`. Its description records the complete verification/review evidence and the explicit no-main/no-release boundary.

- [ ] **Step 2: Wait for exact-head GitHub CI and merge**

  Require backend, frontend, ae-cli, and deploy-static success on the exact PR head before squash-merging to `feat/platform-loading-performance`.

- [ ] **Step 3: Close #173 with exact evidence**

  Record the PR head, CI run, integration commit, verification matrix, review result, and the explicit absence of release/deployment/production actions.
