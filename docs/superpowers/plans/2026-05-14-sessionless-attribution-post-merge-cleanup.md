# Sessionless Attribution Post-Merge Cleanup Implementation Plan

> **Status:** Completed. This cleanup plan covered the highest-signal follow-up work after PR #23 merged: local attribution sync now fails open instead of crashing, the dead manual bind path has been removed, the legacy session create path now persists the authenticated owner, sessionless write paths now enforce the authenticated owner on scoped writes, sessionless Codex scanning no longer misattributes global SQLite transport logs to the active workspace, and repo docs now describe the actual dual-track runtime state. Remaining larger architecture choices such as fully demoting the session/local-proxy path are still out of scope for this plan.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stabilize the merged sessionless attribution path and remove stale or misleading follow-up pieces without changing the broader legacy session/local-proxy runtime contract.

**Architecture:** Keep the existing dual-track runtime intact for now. Tighten the new sessionless path so it fails open correctly, remove the unused manual bind surface because commit checkpoint recording already performs binding server-side, repair the legacy session create handler so ownership is still correct on the compatibility path, and update docs so they stop overstating what is already current.

**Tech Stack:** Go (`cobra`, `gin`, `ent`), Vue 3 docs/UI references, Markdown docs, existing ae-cli/backend test suites.

---

## File Structure

### Modify

- `ae-cli/internal/attributionlocal/sync.go`
  Guard nil backend client usage and spool newly scanned events on upload failure.
- `ae-cli/internal/attributionlocal/sync_test.go`
  Cover fail-open sync behavior for newly scanned events.
- `ae-cli/internal/attributionlocal/scanner.go`
  Stop treating workspace-local Codex home as a required sessionless attribution input.
- `ae-cli/internal/attributionlocal/scanner_test.go`
  Keep scanner behavior explicit after source selection changes.
- `ae-cli/internal/client/client.go`
  Remove dead `BindToolUsageEvents` client call if the API surface is deleted.
- `ae-cli/internal/client/client_test.go`
  Drop bind-route client coverage if the method is removed.
- `backend/internal/handler/router.go`
  Remove the dead `/tool-usage-events/bind` route if no production caller remains.
- `backend/internal/handler/tool_usage.go`
  Remove bind handler wiring if the route is deleted.
- `backend/internal/handler/tool_usage_test.go`
  Drop the dead bind-endpoint test and keep create coverage.
- `backend/internal/handler/session.go`
  Fix legacy `POST /sessions` to use authenticated user context correctly.
- `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`
  Mark the spec status and implementation note accurately after the merged partial rollout.
- `docs/superpowers/plans/2026-05-13-sessionless-local-tool-attribution.md`
  Correct stale top-level status text so it no longer claims “in progress” after merge.
- `docs/architecture.md`
  Soften “current sessionless path” wording to reflect dual-track runtime reality.
- `README.md`
  Keep the repo entrypoint accurate without implying sessionless attribution has already replaced the session workflow.
- `docs/ae-cli/session-pr-attribution.md`
  Mark this as a legacy session/local-proxy runbook and remove the claim that sessionless attribution depends on workspace-local Codex home.

## Task 1: Make Local Attribution Sync Fail Open

**Files:**
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`

- [x] **Step 1: Add a failing test for nil backend client handling during hook-triggered sync**
- [x] **Step 2: Run the targeted ae-cli sync test to confirm failure**
- [x] **Step 3: Implement nil-client guard plus spool-on-upload-failure for newly scanned events**
- [x] **Step 4: Re-run the targeted ae-cli sync tests to confirm pass**

## Task 2: Remove The Dead Manual Tool-Usage Bind Path

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/tool_usage.go`
- Modify: `backend/internal/handler/tool_usage_test.go`

- [x] **Step 1: Add or adjust tests so the bind endpoint/client path is proven unused by production flow**
- [x] **Step 2: Run the targeted ae-cli/backend tests to confirm the dead path is still referenced only by tests**
- [x] **Step 3: Remove the bind client/interface/route/handler/test coverage**
- [x] **Step 4: Re-run the targeted ae-cli/backend tests to confirm pass**

## Task 3: Fix Legacy Session Create Owner Assignment

**Files:**
- Modify: `backend/internal/handler/session.go`
- Modify: `backend/internal/handler/handler_test.go` or the closest existing session HTTP test file

- [x] **Step 1: Add a failing HTTP test proving `POST /api/v1/sessions` does not currently persist the authenticated owner**
- [x] **Step 2: Run the targeted backend handler test to confirm failure**
- [x] **Step 3: Implement the minimal handler fix using `auth.GetUserContext(c)`**
- [x] **Step 4: Re-run the targeted backend handler test to confirm pass**

## Task 4: Align Docs With Actual Runtime State

**Files:**
- Modify: `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`
- Modify: `docs/superpowers/plans/2026-05-13-sessionless-local-tool-attribution.md`
- Modify: `docs/architecture.md`
- Modify: `README.md`
- Modify: `docs/ae-cli/session-pr-attribution.md`

- [x] **Step 1: Update doc status lines and runtime wording so they describe the current dual-track implementation truthfully**
- [x] **Step 2: Remove the specific claim that sessionless attribution depends on workspace-local Codex home**
- [x] **Step 3: Run lightweight doc verification plus any touched targeted tests**

## Task 5: Tighten Owner Checks On New Sessionless Write Paths

**Files:**
- Modify: `backend/internal/toolusage/service.go`
- Modify: `backend/internal/toolusage/service_test.go`
- Modify: `backend/internal/handler/tool_usage.go`
- Modify: `backend/internal/handler/tool_usage_test.go`
- Modify: `backend/internal/checkpoint/service.go`
- Modify: `backend/internal/handler/checkpoint.go`
- Modify: `backend/internal/handler/checkpoint_test.go`
- Modify: `backend/internal/handler/handler_coverage_test.go`

- [x] **Step 1: Add coverage for cross-user `tool-usage-events` writes and session-bound checkpoint writes**
- [x] **Step 2: Implement authenticated owner checks for `tool_usage_events` scope resolution**
- [x] **Step 3: Implement authenticated owner checks for checkpoint/rewrite requests when `session_id` is present**
- [x] **Step 4: Re-run backend `toolusage` / `checkpoint` / `handler` tests to confirm pass**

## Task 6: Remove Unsafe Sessionless Codex SQLite Workspace Attribution

**Files:**
- Modify: `ae-cli/internal/attributionlocal/scanner.go`
- Modify: `ae-cli/internal/attributionlocal/scanner_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `ae-cli/internal/attributionlocal/test_helpers_test.go`
- Modify: `docs/architecture.md`

- [x] **Step 1: Add coverage proving sessionless scanner ignores global Codex SQLite transport logs**
- [x] **Step 2: Switch sessionless scanner to workspace-matching Codex JSONL input only**
- [x] **Step 3: Re-run attributionlocal tests to confirm pass**

## Verification

- [x] `cd /Users/admin/ai-efficiency/ae-cli && go test ./internal/attributionlocal ./internal/client ./internal/hooks ./cmd`
- [x] `cd /Users/admin/ai-efficiency/backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/toolusage ./internal/checkpoint ./internal/handler`

## Known Remaining Gaps

- This plan does not remove the legacy session/local-proxy runtime or the `Sessions` frontend surface.
- This plan does not redesign PR/repo UI to surface credit/request-count attribution outputs.
