# Team Member Metadata Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep selected-member usage detail responsive across many active subscriptions by replacing serial rate-multiplier metadata reads with one batch-shaped provider call that has bounded fan-out, deadlines, and explicit per-group degradation.

**Architecture:** `teamusage.Service` continues to authorize the selected subject and load current subscriptions before reading multiplier metadata. It calls a new optional Relay batch reader once with the unique active group IDs. The Sub2API adapter implements that batch shape over the real per-group endpoint with at most four workers, a two-second per-group deadline, and a five-second overall deadline. Each group returns its own entries or error so one failed branch cannot hide successful rows. Existing single-group read/replace calls, provider-group locking, readback verification, and audit writes remain the only multiplier mutation path.

**Tech Stack:** Go, Gin/Ent service layer, Sub2API HTTP adapter, Vue 3, TypeScript, Vitest.

## Global Constraints

- Keep Relay/Sub2API access behind `backend/internal/relay.Provider` optional capabilities; do not modify Sub2API or introduce direct database coupling.
- The upstream multiplier API remains per-group. The adapter may fan out, but must not claim or simulate an upstream batch endpoint.
- Batch reads use at most four concurrent requests, a two-second deadline per group, and a five-second overall deadline; a shorter caller deadline always wins.
- Batch results are per-group. One group failure leaves other active subscription rows available and marks only the failed row's multiplier metadata unavailable.
- An unavailable multiplier row must not display an inherited value as the authoritative effective multiplier and must be non-editable.
- Authorization, current subscriptions, single-group mutation reads, whole-group replacement, readback verification, provider-group locking, and local audit behavior remain authoritative and unchanged.
- Do not cache multiplier authorization or mutation state. Redis is not required for this bounded fan-out change.
- Tests and examples use only synthetic users, domains, credentials, and group names.
- Do not merge, release, tag, deploy, or run Helm as part of this issue.
- Every completed step updates this file in the same execution turn.

**Status:** In progress for issue #129. Stacked on `perf/team-scope-124` / Draft PR #149.

---

### Task 1: Add a batch-shaped Relay multiplier reader

**Files:**
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api.go`
- Modify: `backend/internal/relay/sub2api_test.go`

**Interfaces:**
- Produce `relay.GroupRateMultiplierBatchReader` with one call accepting unique group IDs and returning a `relay.GroupRateMultiplierReadResult` for each group.
- Preserve `relay.GroupRateMultiplierManager.ListGroupRateMultipliers` and `ReplaceGroupRateMultipliers` unchanged for authoritative writes.
- The Sub2API adapter deduplicates group IDs, runs at most four requests concurrently, gives each request a two-second deadline under a five-second batch budget, and records errors per group.

- [x] **Step 1: Add failing adapter tests for batch shape, deduplication, concurrency bound, near-slowest-branch duration, request count, per-group partial failure, and deadline cancellation**
- [x] **Step 2: Run focused Relay tests and record RED**
- [x] **Step 3: Add the provider result/capability types and implement the bounded Sub2API fan-out**
- [x] **Step 4: Re-run focused Relay tests and confirm GREEN**
- [x] **Step 5: Run `go test -race ./internal/relay -run 'GroupRateMultipliersForGroups' -count=1` and confirm GREEN**

### Task 2: Consume one batch read in selected-member detail

**Files:**
- Modify: `backend/internal/teamusage/types.go`
- Modify: `backend/internal/teamusage/quota.go`
- Modify: `backend/internal/teamusage/service.go`
- Modify: `backend/internal/teamusage/service_test.go`

**Interfaces:**
- Add explicit `multiplier_metadata_status` (`ok` or `unavailable`) and optional `multiplier_metadata_message` to each subscription row.
- Successful groups preserve existing multiplier values and edit policy.
- Failed/missing group results set the multiplier source to `unknown`, do not expose an inherited effective value as authoritative, and disable edits with `multiplier_metadata_unavailable`.
- Providers without the batch capability return active subscription rows with unavailable multiplier metadata rather than reintroducing serial calls.

- [x] **Step 1: Add failing service tests for one unique batch call, successful multi-group rows, isolated partial failure, unsupported capability, and no serial dashboard reads**
- [x] **Step 2: Add a failing regression test proving a subsequent multiplier edit still uses single-group reads, whole-group replace/readback, and a successful audit**
- [x] **Step 3: Run focused Team Usage tests and record RED**
- [x] **Step 4: Implement the row metadata contract and batch-only selected-member read path**
- [x] **Step 5: Re-run focused Team Usage tests and confirm GREEN**

### Task 3: Render explicit multiplier metadata degradation

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/user/usage/SelectedSubjectSubscriptionRows.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/selected-subject-subscription-rows.test.ts`

**Interfaces:**
- Treat missing status from an older backend as `ok` for rolling compatibility.
- For `unavailable`, render localized warning text in the multiplier cell, hide the numeric effective multiplier, and keep the edit command disabled.
- Preserve usage/quota display because those fields come from the current subscription response, not the failed multiplier metadata branch.

- [ ] **Step 1: Add failing component tests for warning copy, hidden inherited multiplier, disabled edit, preserved usage/quota, and Chinese localization**
- [ ] **Step 2: Run the focused Vitest file and record RED**
- [ ] **Step 3: Implement the TypeScript contract, localized warning state, and non-editable rendering**
- [ ] **Step 4: Re-run the focused Vitest file and confirm GREEN**

### Task 4: Document, verify, review, and publish

**Files:**
- Modify: `docs/architecture.md`
- Modify: this plan

- [ ] **Step 1: Update current architecture with the batch-shaped selected-member metadata read, bounded Sub2API fan-out, and per-group unavailable state**
- [ ] **Step 2: Run `gofmt` and `git diff --check`**
- [ ] **Step 3: Run `cd backend && go test ./...`**
- [ ] **Step 4: Run changed-package race tests and `go vet`**
- [ ] **Step 5: Run `cd frontend && npm test`, `npm run build`, and the role E2E suite**
- [ ] **Step 6: Run `cd ae-cli && go test ./...`**
- [ ] **Step 7: Review the exact branch diff against issue #129 and the active performance spec; fix all critical/important findings**
- [ ] **Step 8: Commit with Conventional Commits, push, and open a Draft PR against `perf/team-scope-124` with `Closes #129` and dependency/no-release notes**
- [ ] **Step 9: Wait for all required PR checks and record their final state**

## Verification Notes

- Baseline: `cd backend && go test ./internal/teamusage ./internal/relay` passed before implementation.
