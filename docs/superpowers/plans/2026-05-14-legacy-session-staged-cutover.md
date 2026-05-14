# Legacy Session / Local Proxy Staged Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make sessionless attribution the only formal user-facing workflow while keeping legacy session/local-proxy code in minimal compatibility mode.

**Architecture:** Phase 1 does not delete the legacy backend/session schema. Instead it changes the user entrypoints: new CLI commands (`init/sync/doctor`), hidden legacy commands that fail with migration guidance, `Attribution` replacing `Sessions` in primary navigation, and docs updated so local proxy is no longer described as the formal runtime data plane.

**Tech Stack:** Go, Cobra, Gin, Vue 3, Pinia, Vitest, Markdown docs

---

## File Map

- Create: `docs/superpowers/specs/2026-05-14-legacy-session-staged-cutover-design.md`
  - Phase 1 cutover contract.
- Modify: `README.md`
  - Replace session/local-proxy user entrypoint language.
- Modify: `docs/architecture.md`
  - Mark sessionless as the formal path and legacy session/proxy as compatibility/debug.
- Modify: `docs/ae-cli/session-pr-attribution.md`
  - Downgrade to explicit legacy/debug runbook.
- Modify: `ae-cli/cmd/root.go`
  - Register new top-level `init`, `sync`, `doctor` commands.
- Create: `ae-cli/cmd/init.go`
  - Sessionless initialization command.
- Create: `ae-cli/cmd/sync.go`
  - Public sessionless sync command.
- Create: `ae-cli/cmd/doctor.go`
  - Sessionless diagnostics command.
- Modify: `ae-cli/cmd/start.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/stop.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/run.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/attach.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/ps.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/shell.go`
  - Convert to explicit migration error path.
- Modify: `ae-cli/cmd/flush.go`
  - Convert hidden legacy flush into migration guidance or fold into `sync`.
- Modify: `ae-cli/cmd/*_test.go` and/or `ae-cli/cmd/version_test.go`
  - Add CLI migration-behavior tests.
- Modify: `frontend/src/router/index.ts`
  - Replace visible `Sessions` route positioning with `Attribution` route and keep legacy session page hidden/debug.
- Modify: `frontend/src/components/AppSidebar.vue`
  - Replace `Sessions` nav with `Attribution`.
- Create or modify: `frontend/src/views/attribution/AttributionLandingView.vue`
  - Phase 1 landing page that routes users toward repo/PR attribution and explains sessionless model.
- Modify: `frontend/src/views/sessions/SessionListView.vue`
  - Mark page as legacy/debug.
- Modify: `frontend/src/views/sessions/SessionDetailView.vue`
  - Mark page as legacy/debug.
- Modify: `frontend/src/views/DashboardView.vue`
  - Remove or rename `Active Sessions` as a top-level primary metric.
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`
  - Assert `Sessions` is removed from primary nav and `Attribution` is present.
- Modify: `frontend/src/__tests__/router.test.ts`
  - Assert `Attribution` route exists and legacy session route remains but is not primary.
- Modify: `frontend/src/__tests__/session-list-view.test.ts`
  - Assert legacy/debug presentation.
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`
  - Update metrics expectations after session metric removal/rename.

## Task 1: Write The New Cutover Spec

**Files:**
- Create: `docs/superpowers/specs/2026-05-14-legacy-session-staged-cutover-design.md`

- [x] **Step 1: Save the approved phase-1 cutover design**
- [x] **Step 2: Self-check the spec for contradictions with current architecture docs**

## Task 2: Add Public Sessionless CLI Entry Points

**Files:**
- Modify: `ae-cli/cmd/root.go`
- Create: `ae-cli/cmd/init.go`
- Create: `ae-cli/cmd/sync.go`
- Create: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/cmd/*_test.go` and/or `ae-cli/cmd/version_test.go`

- [x] **Step 1: Write failing CLI tests for `init`, `sync`, and `doctor` command registration**
- [x] **Step 2: Run the targeted CLI tests to verify failure**
- [x] **Step 3: Implement minimal public commands using existing hook/sync/runtime helpers**
- [x] **Step 4: Re-run targeted CLI tests to verify pass**

## Task 3: Retire Legacy CLI Commands From The User Workflow

**Files:**
- Modify: `ae-cli/cmd/start.go`
- Modify: `ae-cli/cmd/stop.go`
- Modify: `ae-cli/cmd/run.go`
- Modify: `ae-cli/cmd/attach.go`
- Modify: `ae-cli/cmd/ps.go`
- Modify: `ae-cli/cmd/shell.go`
- Modify: `ae-cli/cmd/flush.go`
- Modify: `ae-cli/cmd/*_test.go` and/or `ae-cli/cmd/version_test.go`

- [x] **Step 1: Write failing tests that old commands now return explicit migration guidance**
- [x] **Step 2: Run the targeted CLI tests to verify failure**
- [x] **Step 3: Replace legacy command bodies with consistent “legacy workflow retired” errors that point to `init/sync/doctor`**
- [x] **Step 4: Re-run targeted CLI tests to verify pass**

## Task 4: Switch Frontend Primary Navigation To Attribution

**Files:**
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/AppSidebar.vue`
- Create or modify: `frontend/src/views/attribution/AttributionLandingView.vue`
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`

- [x] **Step 1: Write failing frontend tests for `Attribution` nav visibility and `Sessions` nav removal**
- [x] **Step 2: Run the targeted frontend tests to verify failure**
- [x] **Step 3: Add the `Attribution` route and sidebar entry, keeping legacy session routes reachable but not primary**
- [x] **Step 4: Re-run targeted frontend tests to verify pass**

## Task 5: Downgrade Session Pages To Legacy Debug

**Files:**
- Modify: `frontend/src/views/sessions/SessionListView.vue`
- Modify: `frontend/src/views/sessions/SessionDetailView.vue`
- Modify: `frontend/src/__tests__/session-list-view.test.ts`
- Modify: `frontend/src/__tests__/session-detail-view.test.ts`

- [x] **Step 1: Write failing view tests for legacy/debug wording**
- [x] **Step 2: Run the targeted frontend tests to verify failure**
- [x] **Step 3: Update page titles and helper text so `Sessions` is clearly marked as legacy/debug**
- [x] **Step 4: Re-run targeted frontend tests to verify pass**

## Task 6: Remove Session As A Primary Dashboard Metric

**Files:**
- Modify: `frontend/src/views/DashboardView.vue`
- Modify: `frontend/src/__tests__/dashboard-view.test.ts`

- [x] **Step 1: Write failing dashboard tests for the new top-level metric wording**
- [x] **Step 2: Run the targeted frontend tests to verify failure**
- [x] **Step 3: Replace `Active Sessions` with a neutral attribution/usage-facing metric presentation**
- [x] **Step 4: Re-run targeted frontend tests to verify pass**

## Task 7: Align Docs And User-Facing Copy

**Files:**
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/ae-cli/session-pr-attribution.md`

- [x] **Step 1: Update user-facing docs so they no longer recommend `ae-cli start/stop/flush`**
- [x] **Step 2: Mark local proxy and session pages as compatibility/debug paths only**
- [x] **Step 3: Re-read the changed docs for consistency with the new spec**

## Task 8: Remove Legacy Session Write Data Plane

**Files:**
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/session_usage_test.go`
- Modify: `ae-cli/cmd/start.go`

- [x] **Step 1: Remove `/session-usage-events` and `/session-events` from the backend router**
- [x] **Step 2: Update handler tests so those routes now assert `404`**
- [x] **Step 3: Remove `proxy-serve-internal` from the CLI command tree**
- [x] **Step 4: Re-run backend handler, CLI command, and frontend tests to confirm pass**

## Task 9: Remove Legacy Session Read Surface

**Files:**
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/handler_extended_test.go`
- Modify: `backend/internal/handler/handler_90_test.go`
- Modify: `backend/internal/handler/handler_final_coverage_test.go`
- Modify: `backend/internal/handler/session_bootstrap_http_test.go`
- Delete: `backend/internal/handler/session_detail_http_test.go`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/views/attribution/AttributionLandingView.vue`
- Delete: `frontend/src/views/sessions/SessionListView.vue`
- Delete: `frontend/src/views/sessions/SessionDetailView.vue`
- Delete: `frontend/src/api/session.ts`
- Delete: `frontend/src/__tests__/session-list-view.test.ts`
- Delete: `frontend/src/__tests__/session-detail-view.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/types/index.ts`

- [x] **Step 1: Remove frontend `/sessions` routes, pages, and API wrapper**
- [x] **Step 2: Remove backend `/sessions` read routes and delete read-surface-specific tests**
- [x] **Step 3: Re-run frontend and backend handler verification to confirm pass**

## Task 10: Remove Proxy As A Hook Data Plane

**Files:**
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`
- Modify: `ae-cli/cmd/hook.go`
- Modify: `ae-cli/cmd/hook_test.go`

- [x] **Step 1: Remove local-proxy posting from post-commit and post-rewrite hook handling**
- [x] **Step 2: Remove the hidden `ae-cli hook session-event` path**
- [x] **Step 3: Re-run CLI/hooks verification to confirm pass**

## Verification

- [x] `cd /Users/admin/ai-efficiency/ae-cli && go test ./cmd ./internal/hooks ./internal/attributionlocal ./internal/client`
- [x] `cd /Users/admin/ai-efficiency/frontend && pnpm test`
- [x] `cd /Users/admin/ai-efficiency/backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/toolusage ./internal/checkpoint ./internal/handler`

## Known Remaining Gaps

- Phase 1 does not delete legacy backend tables or read APIs.
- Phase 1 does not remove proxy implementation files; it only removes them from the formal user workflow.
- Phase 1 does not yet build a full workspace/commit attribution UI beyond the new `Attribution` entrypoint and messaging.
