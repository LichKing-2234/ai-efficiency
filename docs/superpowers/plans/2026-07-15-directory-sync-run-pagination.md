# Directory Sync Run Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** Implementation and the pending-pagination remediation are complete and integrated into `feat/platform-loading-performance` through PR #145 at `028fd34b`; the exact integration head `d2bc2694` contains the later review remediations through #172. Earlier push/CI language and the non-blocking Minor note below are historical delivery evidence, not current gates. This work is not merged to `main` or production-verified; #136 remains open for that external evidence.

**Goal:** Let administrators browse long Directory Sync history through stable, lightweight pages while loading complete diagnostics only for the selected run and polling only the latest active preview/apply run.

**Architecture:** Replace the unbounded Ent-entity list with a projected `RunPage` contract using `limit + offset`, stable `started_at DESC, id DESC` ordering, and an independently bounded `latest_active_run` summary. Keep `GET /runs/:id` as the complete diagnostic contract. Migrate the only verified consumer, `DirectorySyncSettings.vue`, in the same release to page history, fetch detail on selection, and recover/poll only the latest queued/running preview or apply run.

**Tech Stack:** Go 1.23/1.24, Gin, Ent 0.14, PostgreSQL, Vue 3 `<script setup lang="ts">`, Vite, TailwindCSS, Vitest, Vue Test Utils.

## Global Constraints

- Work from `docs/performance-contracts-116@5f6c58e6821dfcd95eefff14ea3426d454ae86cd`; do not stack on sibling performance branches.
- Preserve `POST /api/v1/admin/directory/sources/:id/preview`, `POST /api/v1/admin/directory/sources/:id/runs`, `GET /api/v1/admin/directory/sources/:id/runs`, and `GET /api/v1/admin/directory/runs/:id` paths and the existing preview/apply state machine.
- List pagination is `limit + offset`: default 20, maximum 100, nonpositive limit defaults to 20, negative or invalid offset becomes 0, and response metadata is zero-based `page = floor(normalized_offset / normalized_limit)`, `page_size`, `total`, and `items`. A positive unaligned offset is not rounded.
- Run summaries order by `started_at DESC NULLS FIRST, id DESC`. Queued rows have null `started_at`, so they sort before started rows and tie by descending run ID. Do not substitute `created_at` or `updated_at`.
- Summary rows contain only bounded display/progress fields. They omit `warnings`, `summary`, `preview_diff`, `error_message`, and every other complete diagnostic or source/result blob.
- `GET /api/v1/admin/directory/runs/:id` remains the complete selected-run contract, including warnings, summary, preview diff, and error message.
- `latest_active_run` is the newest `preview` or `apply` row whose status is `queued` or `running`, using the same started-time/ID order. It is independently selected and does not depend on the requested history page.
- The frontend polls only `latest_active_run` (or a just-created preview/apply run that becomes it). Selecting a terminal or older history row fetches detail once and never starts a polling loop for that selection.
- Preview never changes current directory facts. Failed apply leaves current facts/offboarding state unchanged. Successful apply remains transactional and source pointers keep their current semantics.
- Repository and GitHub-organization code search found only the current Vue consumer. Migrate it in the same platform release and do not retain an old unpaginated/full-entity compatibility path. If a consumer appears during delivery verification, stop and document a bounded temporary compatibility contract before changing behavior.
- Keep query composition in `backend/internal/directorysync`, handlers thin, API calls in `frontend/src/api`, and view state in the current component boundary. Do not introduce Redis, CDN work, a new service, direct `sub2api` coupling, or background work outside the existing run executor.
- Ent schema changes require `cd backend && go generate ./ent`; commit every generated change and verify generation drift is clean.
- The primary history query and the page-independent latest-active query each require matching ordered indexes and separate PostgreSQL plan evidence. A top-level `Limit` alone is not proof of bounded filtering/sorting.
- Use only synthetic identities, URLs, source names, warnings, and payload markers in tests and docs.
- PostgreSQL query-plan/large-history tests and browser role E2E are environment-sensitive and must be reported separately from ordinary unit tests.
- Update `docs/architecture.md` and the current `2026-06-22-configurable-directory-sync-design.md` only after behavior lands. Do not rewrite older historical specs; the 2026-07-14 performance design already records the governing bounds.
- Maintain this plan as a live ledger: check each step only after it runs and keep the top status consistent with remaining checkboxes.

## API Compatibility Matrix

| Surface | Request | Response and behavior |
| --- | --- | --- |
| Run start | Existing preview/apply POST bodies | Existing complete queued run row; execution and conflict semantics unchanged |
| Run history | `GET .../sources/:id/runs?limit=20&offset=0` | `items`, `total`, zero-based `page`, `page_size`, nullable `latest_active_run`; items are lightweight summaries |
| Run detail | `GET .../directory/runs/:id` | Existing complete `DirectorySyncRun`, including all selected diagnostics |
| Frontend recovery | First history page | Uses page-independent `latest_active_run`; otherwise displays the newest preview/apply summary without polling |

## File Map

- Modify `backend/internal/directorysync/service.go`: page constants, normalization, summary/page DTOs, projected list and latest-active queries.
- Modify `backend/internal/directorysync/service_test.go`: bounds, stable order, projection, latest active, and apply/preview parity.
- Modify `backend/internal/handler/directory.go` and `directory_test.go`: paginated HTTP contract and complete detail compatibility.
- Modify `backend/ent/schema/directory_sync_run.go`; regenerate `backend/ent/`: stable source/started-time/ID index.
- Create `backend/internal/directorysync/run_query_plan_test.go`: 2,400-row recording-driver fixture, bytes, stable pages, projection, and PostgreSQL plan proof.
- Modify `frontend/src/api/directory.ts`, `frontend/src/types/index.ts`, and `frontend/src/__tests__/api-modules.test.ts`: typed paginated history API.
- Modify `frontend/src/components/settings/DirectorySyncSettings.vue`, `frontend/src/__tests__/directory-sync-settings.test.ts`, and `frontend/src/i18n.ts`: history navigation, selected detail, latest-active recovery, and polling isolation.
- Modify `docs/architecture.md` and `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md`: current runtime/API truth.
- Maintain this plan with test, review, PR, and CI evidence.

---

### Task 1: Bounded Run Summary And Detail HTTP Contracts

**Files:**
- Modify: `backend/internal/directorysync/service.go`
- Modify: `backend/internal/directorysync/service_test.go`
- Modify: `backend/internal/handler/directory.go`
- Modify: `backend/internal/handler/directory_test.go`
- Modify: `backend/ent/schema/directory_sync_run.go`
- Regenerate: `backend/ent/`
- Maintain: `docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md`

**Interfaces:**
- Produces `DefaultRunPageSize = 20`, `MaxRunPageSize = 100`, and `NormalizeRunPage(limit, offset) (int, int)`.
- Produces `RunListRequest{SourceID, Limit, Offset int}`, `RunSummary`, and `RunPage{Items, Total, Page, PageSize, LatestActiveRun}`.
- Changes internal `DirectoryAdminService.ListRuns` to `ListRuns(context.Context, directorysync.RunListRequest) (directorysync.RunPage, error)`; public HTTP paths remain unchanged.

- [x] **Step 1: Add failing service tests for bounds, ordering, projection, and active recovery**

  Add tests:

  ```text
  TestListRunsDefaultsToTwenty
  TestListRunsClampsLimitToOneHundred
  TestListRunsNormalizesNegativeOffset
  TestListRunsOrdersStartedAtThenIDDescending
  TestListRunsOrdersQueuedNullStartedAtFirst
  TestListRunsPagesTiesWithoutDuplicates
  TestListRunsSummaryOmitsDiagnosticBlobs
  TestListRunsReturnsLatestActiveOutsideRequestedPage
  TestGetRunKeepsCompleteDiagnostics
  ```

  Seed 125 runs for one synthetic source, including equal `started_at` values, queued null-start rows, completed preview/apply rows, validate rows, and two active rows whose IDs make the expected latest row explicit. Put unique markers in `warnings`, `summary`, `preview_diff`, and `error_message`.

  Assert default/maximum/negative bounds, `limit=-1`, `limit=20&offset=21` yielding page 1 without rounding the offset, full `total`, exact ordered IDs across page 0/page 1, no overlap under ties, and latest active independence from offset. Marshal each `RunSummary` and require every diagnostic key/marker to be absent; marshal selected detail and require every marker to be present.

- [x] **Step 2: Add failing handler compatibility tests and record RED**

  Cover absent/zero/invalid/negative limit, `limit=101`, `limit=1000`, negative/invalid offset, `limit=20&offset=21`, and `limit=20&offset=40`. Assert page size 20/100, floor-derived page 0/1/2 without offset rounding, total, items, and nullable latest active. Assert list items omit all diagnostic keys, while `GET /runs/:id` returns complete diagnostics.

  Run:

  ```bash
  (cd backend && go test ./internal/directorysync ./internal/handler -run 'Test(ListRuns|DirectoryHandler.*Run)' -count=1)
  ```

  Expected: FAIL because the service returns an unbounded full-entity slice ordered by `created_at`, the handler accepts no pagination, and the response has no metadata/latest-active contract.

- [x] **Step 3: Implement one normalized projected page contract**

  Add exact DTOs:

  ```go
  type RunListRequest struct { SourceID, Limit, Offset int }

  type RunSummary struct {
      ID                 int                      `json:"id"`
      SourceID           int                      `json:"source_id"`
      Mode               directorysyncrun.Mode    `json:"mode"`
      Trigger            directorysyncrun.Trigger `json:"trigger"`
      Status             directorysyncrun.Status  `json:"status"`
      Phase              directorysyncrun.Phase   `json:"phase"`
      StartedAt          *time.Time               `json:"started_at"`
      CompletedAt        *time.Time               `json:"completed_at"`
      HTTPRequestCount   int                      `json:"http_request_count"`
      DepartmentCount    int                      `json:"department_count"`
      MemberCount        int                      `json:"member_count"`
      InvalidMemberCount int                      `json:"invalid_member_count"`
      WarningCount       int                      `json:"warning_count"`
  }

  type RunPage struct {
      Items []RunSummary `json:"items"`
      Total int `json:"total"`
      Page int `json:"page"`
      PageSize int `json:"page_size"`
      LatestActiveRun *RunSummary `json:"latest_active_run"`
  }
  ```

  `ListRuns` validates positive source ID, normalizes twice defensively through the shared helper, counts the complete source result, then selects only summary fields before ordering/offset/limit. `Page` is integer floor division of the unchanged normalized offset by normalized limit. Use a second projected `Limit(1)` query for preview/apply statuses queued/running, independent of history offset. Return `Items: []`, not null.

  Keep `GetRun` unchanged. In the handler, parse query integers, call `NormalizeRunPage`, pass a `RunListRequest`, and return the page directly through `pkg.Success`. Do not expose an old query switch that restores full rows.

- [x] **Step 4: Add the stable descending index and regenerate Ent**

  Add both indexes:

  ```go
  index.Fields("source_id", "started_at", "id").
      Annotations(entsql.DescColumns("started_at", "id")),
  index.Fields("source_id", "started_at", "id").
      StorageKey("directory_sync_runs_active_started_id").
      Annotations(
          entsql.DescColumns("started_at", "id"),
          entsql.IndexWhere("mode IN ('preview', 'apply') AND status IN ('queued', 'running')"),
      )
  ```

  Keep existing indexes. Run:

  ```bash
  (cd backend && go generate ./ent)
  (cd backend && gofmt -w internal/directorysync/service.go internal/directorysync/service_test.go internal/handler/directory.go internal/handler/directory_test.go ent/schema/directory_sync_run.go)
  git diff --check
  ```

- [x] **Step 5: Verify focused and broad backend GREEN**

  Run separately:

  ```bash
  (cd backend && go test ./internal/directorysync ./internal/handler -run 'Test(ListRuns|GetRun|DirectoryHandler.*Run)' -count=1)
  (cd backend && go test ./internal/directorysync ./internal/handler ./cmd/server -count=1)
  git diff --check
  ```

  Expected: PASS; start/execute/apply/preview tests remain unchanged, list is bounded/projected, detail stays complete, and source/active order is exact.

- [x] **Step 6: Commit Task 1 and record the checkpoint**

  Commit implementation plus checked Steps 1-5:

  ```bash
  git add backend/internal/directorysync backend/internal/handler/directory.go backend/internal/handler/directory_test.go backend/ent docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md
  git commit -m "perf(directory): page lightweight sync run summaries"
  ```

  After the commit, check Step 6 and commit `docs(plan): record directory run API task 1`.

---

### Task 2: Large-History Bytes And PostgreSQL Plan Proof

**Files:**
- Create: `backend/internal/directorysync/run_query_plan_test.go`
- Modify: `backend/internal/handler/directory_test.go`
- Modify only if evidence requires it: `backend/ent/schema/directory_sync_run.go` and generated `backend/ent/`
- Maintain: `docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md`

**Interfaces:**
- Consumes Task 1 `RunListRequest`, `RunPage`, summary projection, ordering, and index.
- Produces repeatable 2,400-row response-byte, stable-page, projection, and PostgreSQL structural-plan evidence.

- [x] **Step 1: Add the recording driver and exact scale fixture**

  Wrap `dialect.Driver.Query` under a mutex, copying SQL and arguments before delegation. Open a second Ent client from `testdb.OpenWithDSN` through `entsql.OpenDB`.

  Insert exactly 2,400 runs in batches of 200 for one synthetic source:

  ```text
  preview/apply/validate modes with deterministic distribution
  queued/running/completed/completed_with_warnings/failed statuses;
  all apply rows are terminal and active rows are preview-only so a real apply can still start
  64 repeated non-null started_at values plus bounded queued nulls
  4 KiB generated markers in each of warnings, summary, and preview_diff
  unique error_message markers
  latest active ID known in advance
  ```

  Run `ANALYZE directory_sync_runs` after seeding. The active-present plan uses this fixture unchanged. For the no-active case in the same query-plan test, transition the bounded active preview rows to terminal status, rerun `ANALYZE`, and execute the exact active query again while all 2,400 historical rows remain; do not substitute an empty source.

- [x] **Step 2: Write scale behavior, bytes, and exact SQL-shape tests**

  Add:

  ```text
  TestLargeRunHistoryBoundsBytesAndProjection
  TestLargeRunHistoryStablePages
  TestLargeRunHistoryQueryPlans
  TestLargeRunHistoryDetailRemainsComplete
  TestLargeRunHistoryPreservesPreviewAndApplyStateSemantics
  TestDirectoryHandlerLargeRunHistoryWireBounds
  ```

  Assert default page 20, maximum page 100, total 2,400, stable `(started_at DESC NULLS FIRST, id DESC)` pages at limits 20/50/100, no duplicate/omitted IDs for the traversed result, and page-independent latest active.

  Marshal a 100-row `RunPage` and require less than 128 KiB plus absence of all large markers/diagnostic keys. Keep SQL capture, scale data, and EXPLAIN tests in package `directorysync`. Put `TestDirectoryHandlerLargeRunHistoryWireBounds` in `backend/internal/handler/directory_test.go`, where the existing fake service returns the same deterministic 100 summaries with `total=2400`; serve the real list handler and require the complete `pkg.Success` wire body to remain below 128 KiB while retaining `items`, `total`, `page`, `page_size`, and `latest_active_run`. Through the same real handler boundary, fetch one selected populated detail and require its full 4 KiB markers/error message, then fetch a queued zero-count detail and require the response/type fixture to accept omitted Ent `omitempty` count and timestamp fields. This keeps handler serialization evidence out of the `directorysync` package and avoids an import cycle.

  Classify captured SQL by shape rather than call order: source count, primary page, and latest-active page. Require the count query to aggregate without entity projection; require the primary page to have `LIMIT`, exact two-field order, and no diagnostic fields; require the active query to have the exact preview/apply and queued/running predicates, the same order, `LIMIT 1`, and the same lightweight projection.

  Replay all three exact SQL/argument sets under `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`. Assert the count has an Aggregate node and the primary page has a Limit with at most 100 actual rows and uses the general source/started-time/ID index. The active-present case must use `directory_sync_runs_active_started_id`, contain no Sort, and materialize at most one row. After terminalizing the active rows with all 2,400 historical rows still present and rerunning `ANALYZE`, the no-active case must contain no Sort, materialize no result row, scan/filter no terminal history rows, and use a predicate-compatible index path; do not require one exact index name when PostgreSQL can prove absence through an equally bounded source/status path. Do not assert costs, elapsed time, buffers, or a complete node tree.

  For `TestLargeRunHistoryPreservesPreviewAndApplyStateSemantics`, keep the 2,400 historical rows present, create known current departments/members/memberships, source run pointers, offboarding actions/candidate state, and a deterministic executor sequence. Prove a preview leaves facts, memberships, offboarding state, and `last_successful_run_id` unchanged while advancing `last_run_id` to the completed preview ID; an injected failed apply leaves facts, offboarding state, `last_run_id`, and `last_successful_run_id` unchanged; and a successful apply transactionally replaces facts and updates both pointers to the successful apply ID exactly as the existing ordinary contract specifies.

- [x] **Step 3: Run post-implementation characterization without mutating production**

  Run:

  ```bash
  (cd backend && go test ./internal/directorysync -run 'TestLargeRunHistory' -count=1 -v)
  (cd backend && go test ./internal/handler -run '^TestDirectoryHandlerLargeRunHistoryWireBounds$' -count=1 -v)
  ```

  Expected: GREEN because Task 2 is explicit post-implementation scale/plan characterization of Task 1. Do not edit correct production code to manufacture RED. Instead, table-test the SQL-role/parser assertion helpers with synthetic bad SQL strings containing an extra sort expression, a selected diagnostic column, missing active predicates, and missing limit; require deterministic validation errors before using those helpers against recorded production SQL.

- [x] **Step 4: Verify repeated scale and package GREEN**

  Run:

  ```bash
  (cd backend && go test ./internal/directorysync -run 'TestLargeRunHistory' -count=2 -v)
  (cd backend && go test ./internal/handler -run '^TestDirectoryHandlerLargeRunHistoryWireBounds$' -count=2 -v)
  (cd backend && go test ./internal/directorysync ./internal/handler -count=1)
  git diff --check
  ```

  Record exact fixture/blob/DTO/wire byte counts, the general and partial-active selected indexes, active-present/no-active plans, state-semantic outcomes, and structural nodes; elapsed time is diagnostic only, not a budget.

- [x] **Step 5: Commit Task 2 and record the checkpoint**

  Commit `test(directory): prove bounded sync run history`, then check Step 5 and commit `docs(plan): record directory run scale task 2`.

---

### Task 3: Paginated History, Detail-On-Demand, And Active-Only Polling

**Files:**
- Modify: `frontend/src/api/directory.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/components/settings/DirectorySyncSettings.vue`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/directory-sync-settings.test.ts`
- Maintain: `docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md`

**Interfaces:**
- Consumes Task 1 `RunPage` JSON and unchanged complete `DirectorySyncRun` detail.
- Produces exact lightweight `DirectoryRunSummary`, expanded full-detail `DirectorySyncRun`, `DirectoryRunPage`, `listDirectoryRuns(id, {limit, offset})`, paginated history state, and selected detail state.

- [x] **Step 1: Add failing API/type and component tests**

  Change API tests to require:

  ```ts
  listDirectoryRuns(7, { limit: 20, offset: 40 })
  // GET /admin/directory/sources/7/runs with params
  ```

  Add component tests for:

  ```text
  first page requests limit=20 offset=0 and renders only returned summaries
  next/previous pages use offset and preserve total/page metadata
  selecting a terminal summary fetches getDirectoryRun once and renders complete warning/summary/diff/error detail
  selecting terminal/history rows never starts polling
  latest_active_run outside items is recovered and is the only ID polled
  a newer terminal summary never displaces an active run for recovery
  conflict recovery reloads the page and uses latest_active_run
  source switching prevents stale page/detail responses from overwriting current state
  slow same-source page 0 cannot overwrite a later page 1 response
  slow detail A cannot overwrite later selected detail B
  active A remains the only polled ID while terminal B is fetched once and displayed
  source switch, unmount, and recovery of a newer active ID invalidate old poll responses/timers
  just-created preview/apply remains visible and polling completion refreshes only current source/page
  ```

  Update default mocks to return `{items: [], total: 0, page: 0, page_size: 20, latest_active_run: null}`. Keep fake timers bounded and restore them in `finally`.

- [x] **Step 2: Run frontend tests and record RED**

  Run:

  ```bash
  (cd frontend && npm test -- src/__tests__/api-modules.test.ts src/__tests__/directory-sync-settings.test.ts)
  ```

  Expected: FAIL because the API accepts no params, the component scans an unbounded entity list, has no history/detail state, and derives polling from whichever row it finds.

- [x] **Step 3: Implement typed page/detail state without duplicate requests**

  Define the exact lightweight and full types:

  ```ts
  export interface DirectoryRunSummary {
    id: number
    source_id: number
    mode: 'validate' | 'preview' | 'apply'
    trigger: 'manual' | 'schedule'
    status: 'queued' | 'running' | 'completed' | 'completed_with_warnings' | 'failed'
    phase: 'validating' | 'executing' | 'normalizing' | 'applying' | 'completed' | 'failed'
    started_at: string | null
    completed_at: string | null
    http_request_count: number
    department_count: number
    member_count: number
    invalid_member_count: number
    warning_count: number
  }

  export interface DirectorySyncRun
    extends Omit<DirectoryRunSummary,
      | 'started_at'
      | 'completed_at'
      | 'http_request_count'
      | 'department_count'
      | 'member_count'
      | 'invalid_member_count'
      | 'warning_count'> {
    started_at?: string | null
    completed_at?: string | null
    http_request_count?: number
    department_count?: number
    member_count?: number
    invalid_member_count?: number
    warning_count?: number
    warnings?: DirectorySyncWarning[]
    summary?: Record<string, unknown>
    preview_diff?: Record<string, unknown>
    error_message?: string | null
    created_at?: string
    updated_at?: string
  }

  export interface DirectoryRunPage {
    items: DirectoryRunSummary[]
    total: number
    page: number
    page_size: number
    latest_active_run: DirectoryRunSummary | null
  }
  ```

  API tests must prove list typing/fixtures contain no diagnostic fields. Selected-detail component tests must render distinct markers from `warnings`, `summary`, `preview_diff`, and `error_message`, so the expanded type cannot silently omit the complete contract. Add a queued/zero-count detail fixture that omits timestamps and all five Ent `omitempty` count fields, proving the exact detail type remains compatible with the unchanged backend entity response.

  Change `listDirectoryRuns` to require/accept typed limit/offset params. Increment the page request generation for every page load, including same-source navigation, and increment the detail request generation for every selection; a response applies only when its captured generation, source, offset, and selected ID still match. Source switch invalidates both generations. Track summaries, total/page/page size, latest active ID, selected summary ID, and selected full detail separately.

  Render an unframed run-history section consistent with the current settings task-zone styling: stable summary rows, page controls, one selected detail region, loading/empty/error states, and existing localized mode/status/progress values. Do not render diagnostic JSON from summary data.

  Selecting any row calls `getDirectoryRun(id)` once. A terminal/older selection never schedules or cancels the independent latest-active timer. Recovery and conflict handling use only `latest_active_run`; when absent, the newest preview/apply summary may update the existing status message but must not be polled. A just-created nonterminal run becomes the current latest active locally and uses the existing polling loop.

  Maintain a separate poll generation. Increment it and clear the timer on source switch, unmount, and whenever recovery selects a different latest-active ID. Every poll captures generation/source/run ID and may apply/reschedule only when all three still match. Selecting terminal B changes only detail generation, so active A remains the sole polled ID. On A's terminal completion, stop polling and refresh the current page; update selected detail only when the selected ID is A and its detail generation is still current.

- [x] **Step 4: Verify focused, full frontend, and build GREEN**

  Run separately:

  ```bash
  (cd frontend && npm test -- src/__tests__/api-modules.test.ts src/__tests__/directory-sync-settings.test.ts src/__tests__/settings-view.test.ts)
  (cd frontend && npm test)
  (cd frontend && npm run build)
  git diff --check
  ```

  Expected: PASS; all existing validate/preview/apply/conflict/source-race behavior remains green, history is bounded, and selected detail/polling lifecycles are isolated.

- [x] **Step 5: Commit Task 3 and record the checkpoint**

  Commit `perf(frontend): page directory run history`, then check Step 5 and commit `docs(plan): record directory run frontend task 3`.

#### Task 3 Independent Review Remediation

- [x] **Record the first review and remediate I1/I2 page lifecycle findings**

  The independent review of `139cadb..0b92b8d` failed with **0 Critical / 4 Important / 1 Minor**. I1 found that terminal fallback and poll completion could replace a non-first page; I2 found that a failed pending page request could advance navigation from an uncommitted offset.

  I1/I2 RED used `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts`: 1 file, 2 failed / 28 passed. The I1 assertion expected the completion refresh at offset `20` but received `0`; the I2 assertion expected pending Next to be disabled but it remained enabled. Commit `32506c7` (`fix(frontend): keep directory run pages coherent`) made the displayed offset atomic, isolated first-page terminal fallback, and refreshed the committed page. GREEN evidence: component file 30/30; focused three files 81/81; full frontend 39 files / 443 tests; production build and `git diff --check` passed.

- [x] **Add RED coverage for I3/I4/M1**

  I3 RED: `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "ignores stale source action"` failed all 6 new preview/apply x success/conflict/error cases. The failures proved that stale success could cancel source B polling, stale 409 could issue a source A page load and invalidate B's pending page, and stale errors rendered under B.

  I4 RED: `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "keeps selected active run terminal"` failed 2/2 response-order cases. The selected header remained Running after a terminal poll, and a selection started during an in-flight poll did not adopt the terminal result.

  M1 RED: `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "formats run timestamps"` failed 1/1 because switching from `en-US` to `zh-CN` retained the English timestamp.

- [x] **Implement and verify I3/I4/M1 lifecycle isolation**

  Commit `7e54dff` (`fix(frontend): isolate directory run lifecycles`) added a source/action generation invalidated by new actions, source switches, and unmount; guarded every preview/apply success, error, and conflict-recovery boundary; made a terminal poll invalidate same-ID detail work and synchronize the selected summary/detail; and formatted timestamps with `locale.value`.

  Targeted GREEN: I3 6/6, I4 2/2, and M1 1/1. The complete component file passed 39/39. Fresh required verification passed: focused three files 90/90, full frontend 39 files / 452 tests, `npm run build`, and `git diff --check`.

- [x] **Record the independent re-review and add RED coverage for stale same-source recovery**

  The independent re-review of `139cadb..912f6ee` failed with **0 Critical / 1 Important / 0 Minor**. The remaining finding showed that action A could enter a 409 recovery page load, action B could then start on the same source and establish the only active poll, and A's older no-active page response could commit history state and cancel B's timer before A reached its post-await action-generation guard.

  The temporary reviewer probe was promoted to a parameterized preview/apply component test. RED command `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "keeps a newer same-source"` failed both cases as expected: each reported zero timers after the stale recovery resolved instead of the newer action's one timer.

- [x] **Reject stale action-scoped recovery pages and verify Task 3 again**

  Commit `54b61d1` (`fix(frontend): reject stale run recovery pages`) bound conflict page recovery to the initiating action generation, required the action/page/source/offset context to match before page commits, recovery application, error state, or finally cleanup, and made a newer action invalidate only an older action-scoped recovery request. Ordinary pagination remains independent, and source-switch invalidation continues through the existing page and action generations.

  Targeted GREEN passed 2/2 and the complete component file passed 41/41. Fresh required verification passed: focused three files 92/92, full frontend 39 files / 454 tests, `npm run build`, and `git diff --check`.

- [x] **Record the third review and add RED coverage for stale ordinary pages**

  The independent third review of `139cadb..ef50fcb` failed with **0 Critical / 1 Important / 0 Minor**. The remaining finding showed that an ordinary same-source page request started before action B was not action-generation-bound: after B established its preview/apply poll, the older page could return `latest_active_run: null`, commit stale history, and cancel B's only timer.

  The temporary reviewer probe was promoted to a permanent parameterized preview/apply component test. RED command `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "keeps a newer same-source .* ordinary page"` failed both cases at the intended assertion: each expected one timer after the stale page resolved and received zero.

- [x] **Invalidate every pending page before a new run action and verify Task 3 again**

  Commit `1c264e7` (`fix(frontend): invalidate stale run page requests`) makes every new preview/apply action advance the page generation and consistently clear only pending offset, recovery ownership, and history-loading state. It preserves the committed rows/page/offset; stale page success, catch, and finally paths fail their existing context guard before they can overwrite a newer request or poll lifecycle. Existing ordinary pagination and action-scoped 409 recovery behavior remain covered.

  Targeted GREEN passed 2/2; the expanded pagination/409 lifecycle target passed 6/6; and the complete component file passed 43/43. Fresh required verification passed: focused three files 94/94, full frontend 39 files / 456 tests, `npm run build`, and `git diff --check`.

- [x] **Record the fourth review and add RED coverage for no-active history pages**

  The independent R3 re-review of `139cadb..95a0ee8` failed with **0 Critical / 2 Important / 0 Minor**. I1 showed that an ordinary page started while a preview/apply POST was pending could resolve without `latest_active_run` after the action established its poll and cancel that newer timer. I2 showed that a no-active non-first page could cancel an existing poll before its terminal detail was observed, leaving stale Running state and disabled action controls.

  Both reviewer probes were promoted to permanent tests: a parameterized preview/apply case for `action POST pending -> page starts -> action establishes poll -> no-active page resolves`, and a current-active case that commits page 2, preserves its selection, observes the terminal detail, and re-enables both action buttons. RED command `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "keeps polling authoritative"` failed 3/3 at the intended timer assertions: each expected one remaining timer and received zero.

- [x] **Keep established active polling authoritative and verify Task 3 again**

  Commit `dc36bd8` (`fix(frontend): keep active run polling authoritative`) makes history-page recovery preserve the independently owned poll and latest-active state when a response has `latest_active_run: null`. A non-null latest active may still adopt or replace the poll target; without an established poll, the existing page-zero terminal fallback remains unchanged. Detail polling still owns terminal/error shutdown, while source switch, unmount, and a different active ID retain their existing invalidation paths. The page/action generation guards from the preceding review rounds are unchanged.

  Targeted GREEN passed 3/3, and the expanded page/action/detail/poll/source lifecycle target passed 35/35. Fresh required verification passed: focused three files 97/97, full frontend 39 files / 459 tests, `npm run build`, and `git diff --check`.

- [x] **Record the fifth review and add RED coverage for settings feedback ownership**

  The independent R4 re-review of `139cadb..fa983d9` failed with **0 Critical / 1 Important / 0 Minor**. The remaining finding showed that enabled template and save interactions shared `clearFeedback()` with run-lifecycle invalidation, so either interaction could cancel a recovered preview/apply timer, clear its active state, and stop the browser from observing the backend run.

  The reviewer probe became permanent preview/apply template coverage plus a deferred save case. Initial RED command `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "keeps a recovered active .* (template|save)"` failed 3/3 at the intended timer assertion: each expected one timer after the interaction and received zero. The save test was then strengthened to select source B before saving; with the old first-source reload restored, it failed because the last history request used source 1 instead of the current source 2.

- [x] **Preserve active polling across settings feedback and verify Task 3 again**

  Commit `57f5211` (`fix(frontend): preserve polling across settings feedback`) removes poll and active-run ownership from generic feedback cleanup, adds an explicit run-lifecycle reset for actual source changes and new preview/apply actions, and makes source reload retain the current source ID when it still exists. Template editing and save start now preserve the same timer, active run, disabled action controls, and run-start message; successful same-source save reload adopts the same active ID without replacing its timer, and the later terminal detail remains authoritative. Existing detail terminal/error, source-switch, unmount, and different-active-ID shutdown paths remain intact; Validate stays disabled during an active run.

  Targeted GREEN passed 3/3 and the complete component lifecycle file passed 49/49. Fresh required verification passed: focused three files 100/100, full frontend 39 files / 462 tests, `npm run build`, and `git diff --check`.

- [x] **Record the final review findings and promote all pending-action probes to RED coverage**

  The first whole-branch final issue/spec review of `5f6c58e..821423f` failed with **0 Critical / 2 Important / 0 Minor**. I1 showed that a page request started during a pending action POST could capture older active run `430`, then replace the just-created run `431` poll after the action response established it. I2 showed that Preview and Apply were not single-flight during the POST-creation window, so a second action could invalidate the first successfully created run. The separate final standards review failed with **0 Critical / 1 Important / 0 Minor** because successful Save reload selected the same source but still advanced the action generation, discarding a pending Preview/Apply response for run `501`.

  All reviewer probes were promoted to permanent parameterized Preview/Apply tests before production changes. I1 RED command `cd frontend && npm test -- src/__tests__/directory-sync-settings.test.ts -t "older-active page started during its POST"` failed 2/2 with observed poll IDs `[431, 430]` instead of `[431, 431]`. I2 RED command with `-t "serializes .* first action POST is pending"` failed both Preview-then-Apply and Apply-then-Preview: both buttons remained enabled, the second API was called, and run `441` received no detail call or timer. The standards RED command with `-t "pending .* owned when Save reloads"` failed Preview and Apply because run `501` received no detail call or timer.

- [x] **Bind page recovery and action creation to explicit owners, then verify the final remediation**

  Each page request now captures `pollGeneration` plus `activePollRunId`; a response may adopt or replace a non-null active run only while that exact poll epoch and owner still match, while a null active response preserves any independently established poll. Preview/Apply POSTs now share a generation-owned pending state that disables both controls immediately, rejects a second submission, and clears only when that same POST settles. Same-source source reload no longer advances the action generation; actual source changes and unmount still invalidate pending ownership.

  The three targeted GREEN commands each passed 2/2, and the complete component lifecycle file passed 55/55. Fresh local verification passed the focused three files at 106/106, the full frontend at 39 files / 468 tests, `npm run build`, and `git diff --check 5f6c58e`. The environment-sensitive role E2E was rerun against this worktree's strict `[::1]:5173` listener (PID 88558) and passed 16/16; cleanup removed only that listener and left the pre-existing IPv4 PID 648 intact. Backend, `ae-cli`, release-embed, Ent drift, and PostgreSQL scale/plan commands were not rerun because this remediation changes only the Vue component, its tests, and this ledger; their fresh Task 4 Step 2 results remain recorded above.

---

### Task 4: Architecture, Full Verification, Reviews, And Draft PR

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md`
- Maintain: `docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md`
- Review only: all changes since `5f6c58e`

**Interfaces:**
- Consumes Tasks 1-3 and all task-review evidence.
- Produces current architecture/contract truth, full verification, final reviews, draft PR, and two green CI rounds.

- [x] **Step 1: Update current architecture and Directory Sync contract**

  Document summary pagination/projection/order, complete selected detail, page-independent latest-active recovery, and active-only polling. Update the current 2026-06-22 Runs section with the exact query/response contract and same-release frontend migration/no verified compatibility consumer. Do not rewrite earlier historical design rationale or claim unrelated #115 slices are complete.

- [x] **Step 2: Run generation drift and full repository verification**

  Run exactly:

  ```bash
  (cd backend && gofmt -w internal/directorysync/service.go internal/directorysync/service_test.go internal/directorysync/run_query_plan_test.go internal/handler/directory.go internal/handler/directory_test.go ent/schema/directory_sync_run.go)
  (cd backend && go generate ./ent)
  git diff --exit-code -- backend/ent
  (cd backend && go test ./...)
  (cd ae-cli && go test ./...)
  (cd frontend && npm test)
  (cd frontend && npm run build)
  (cd frontend && npm run test:e2e:role)
  bash deploy/test/release-frontend-embed-test.sh
  git diff --check
  ```

  Separately rerun and report the PostgreSQL scale/plan test and role E2E. Keep any unrun environment-sensitive item unchecked.

  Fresh local verification on 2026-07-15 ran every command above. The specified
  `gofmt`, Ent generation, and `git diff --exit-code -- backend/ent` passed;
  backend and `ae-cli` full `go test ./...` suites passed; frontend passed 39
  files / 462 tests and the production build; the release frontend embed test
  and final `git diff --check` passed. The separate 2,400-row PostgreSQL run
  reported 29,630,400 fixture diagnostic bytes, a 28,855-byte 100-row DTO, a
  12,771-byte populated detail, stable 2,400-ID traversal at page sizes 20/50/100,
  `directorysyncrun_source_id_started_at_id` for the primary page, and
  `directory_sync_runs_active_started_id` with no Sort for both active-present
  and no-active plans. Handler evidence was 28,935 wire bytes for 100 summaries,
  12,788 populated-detail wire bytes, and 192 queued-detail wire bytes.

  Role E2E was isolated from the other worktree already listening on
  `127.0.0.1:5173` as PID 648. This worktree started strict-port Vite on
  `[::1]:5173` as PID 47181 using
  `/opt/homebrew/Cellar/node/26.3.0/bin/node` with cwd at this worktree's
  `frontend`; `curl http://localhost:5173` connected to `::1`. Exact
  `npm run test:e2e:role` passed 16/16. Vite logged expected `ECONNREFUSED`
  proxy noise for unmocked APIs because no backend was started. Cleanup stopped
  only the owned IPv6 listener and confirmed PID 648 still listening on IPv4.

- [x] **Step 3: Verify consumers and complete independent reviews**

  Rerun repository and GitHub-organization searches for the run-history route/client:

  ```bash
  rg -n 'listDirectoryRuns|ListRuns\(|/sources/[^ ]*/runs|sources/:id/runs' --glob '!docs/**' --glob '!frontend/node_modules/**' .
  gh search code 'listDirectoryRuns' --owner LichKing-2234 --limit 100
  gh search code 'admin/directory/sources' --owner LichKing-2234 --limit 100
  ```

  Expected: only the migrated Vue API/component/tests plus this backend route/service; no non-frontend consumer requiring compatibility.

  Fresh consumer verification on 2026-07-15 ran all three commands exactly.
  Repository search found only the backend route/service/tests and the migrated
  Vue API/component/tests. Both GitHub organization searches returned only
  `LichKing-2234/ai-efficiency`, with the same Vue API/component/tests, backend
  route tests, and current spec; no non-frontend consumer or compatibility
  requirement was found. This consumer-audit portion is complete, but Step 3
  remains unchecked until the independent review gates below are green.

  The first final issue/spec gate reported **0 Critical / 2 Important / 0 Minor**
  and the first final standards gate reported **0 Critical / 1 Important / 0
  Minor**. Their three action-creation lifecycle findings and the completed
  remediation are recorded under Task 3 above. Both final gates must now rerun
  against the regenerated full working-tree package.

  Fresh final review evidence (2026-07-15): the regenerated package was 5,847
  lines / 272,419 bytes with SHA-256
  `60f99c921f064de872ebf17c62193bab9af6044879808abb7bf6b55979931ac8`
  and matched a fresh base-to-working-tree diff byte-for-byte. Fresh SPEC
  re-review returned **0 Critical / 0 Important / 0 Minor** after the six
  permanent remediation cases, 106/106 focused tests, 468/468 full frontend
  tests, build, 2,400-row scale/plan tests, handler wire tests, and adjacent
  custom probes passed. Fresh standards re-review returned **0 Critical / 0
  Important / 1 Minor**. The deferred Minor allows an older same-ID history
  summary to regress displayed progress briefly; it does not change the poll
  ID, timer, action controls, or terminal convergence, and the next detail poll
  repairs the display. No Critical or Important finding remains.

  Obtain independent spec/quality review for Tasks 1-3, resolving every Critical/Important finding. Then package the full `5f6c58e..HEAD` diff and obtain separate final issue/spec and `AGENTS.md`/standards gates. Review questions:

  ```text
  Can any list exceed 100 or select a diagnostic blob?
  Is order exactly started_at DESC NULLS FIRST, id DESC across ties/pages?
  Is latest_active_run page-independent and restricted to queued/running preview/apply?
  Do count and primary-page plans use the intended aggregate/ordered paths, does active-present use the partial ordered index, and does no-active avoid sorting/filtering/materializing 2,400 terminal history rows through a bounded predicate-compatible index path?
  Does selected detail remain complete?
  Can same-source page/detail races overwrite newer state, or can terminal selection disturb active polling?
  Are apply/preview/current-fact/offboarding semantics unchanged with 2,400 historical rows present?
  Do scale bytes and query-plan evidence prove bounded work without timing brittleness?
  ```

- [x] **Step 4: Commit docs and verified delivery state**

  Record exact commands, scale bytes/index, consumer audit, reviews, and environment notes. Set status to implementation/review complete with PR CI pending. First commit the final reviewed Vue remediation:

  ```bash
  git add frontend/src/components/settings/DirectorySyncSettings.vue frontend/src/__tests__/directory-sync-settings.test.ts
  git commit -m "fix(frontend): isolate directory run action ownership"
  ```

  Then commit current documentation and verified delivery state:

  ```bash
  git add docs/architecture.md docs/superpowers/specs/2026-06-22-configurable-directory-sync-design.md docs/superpowers/plans/2026-07-15-directory-sync-run-pagination.md
  git commit -m "docs(architecture): document bounded directory run history"
  ```

- [x] **Step 5: Push and open the stacked draft PR**

  Create ignored `.superpowers/sdd/pr-121.md` with `Closes #121`, dependency on #138, API/consumer migration, scale/bytes/plan evidence, verification/reviews, migration/rollback notes. Run:

  ```bash
  git push -u origin perf/directory-runs-121
  gh pr create --draft --base docs/performance-contracts-116 --head perf/directory-runs-121 --title "perf(directory): page sync runs and load detail on demand" --body-file .superpowers/sdd/pr-121.md
  gh pr view --json number,state,isDraft,baseRefName,headRefName,mergeable,mergeStateStatus,url
  ```

  Delivery evidence (2026-07-15): the traced push used GitHub address
  `20.29.134.23` with HTTP/1.1 and `http.curloptResolve`, exited 0, created
  `origin/perf/directory-runs-121`, and configured the local upstream. Draft PR
  #145 was created at <https://github.com/LichKing-2234/ai-efficiency/pull/145>
  with base `docs/performance-contracts-116`, head
  `perf/directory-runs-121`, and local/remote OID
  `fc992e74f707486f63333f0add55eee1bf9eab8f`.

- [x] **Step 6: Require first CI, commit final ledger, and require replacement CI**

  Wait for backend/frontend/ae-cli/deploy-static success. Only then mark complete and commit/push `docs(plan): record directory run pagination delivery`; wait for replacement CI to pass all four jobs.

  First CI evidence: run `29401353370` completed successfully on
  `fc992e74f707486f63333f0add55eee1bf9eab8f`; backend job `87306395319`,
  frontend job `87306395374`, ae-cli job `87306395376`, and deploy-static job
  `87306395358` all passed. The evidence commit
  `2081921275f31e30e6b24011153e60c0f11a98c2` was then pushed. Replacement
  run `29401677880` passed backend job `87307434561` in 2m48s, frontend job
  `87307434518` in 39s, ae-cli job `87307434531` in 30s, and deploy-static job
  `87307434501` in 14s on that exact commit.

- [x] **Step 7: Verify final branch and PR state**

  Require clean worktree, OPEN draft, exact base/head, local HEAD equal remote OID, mergeable/clean state, and all status checks successful. Keep the worktree; do not merge, tag, release, deploy, or run Helm.

  Final branch/PR evidence before this ledger-only commit: the tracked worktree
  was clean; local HEAD, `origin/perf/directory-runs-121`, and PR head all
  equaled `2081921275f31e30e6b24011153e60c0f11a98c2`; PR #145 was OPEN, draft,
  MERGEABLE/CLEAN, based on `docs/performance-contracts-116`, and all four
  replacement checks were successful. The worktree remains retained, and no
  merge, tag, release, deploy, or Helm action was performed.

#### Post-Integration Pending-Page Remediation

An integration review on 2026-07-17 reproduced one Important race that the
original committed-page case did not cover: when Next had started but had not
committed its response, terminal active-run polling reloaded the old committed
offset, advanced `pageRequestGeneration`, and discarded the user's pending
destination. The new regression holds that Next response pending while the
active run completes and proves the terminal refresh follows offset 20 rather
than returning to offset 0. The production fix selects
`pendingRunOffset ?? runOffset` for that terminal refresh. The RED test failed
with offsets `[0, 20, 0]`; after the fix, the complete
`directory-sync-settings.test.ts` suite passed 62/62 and `git diff --check`
passed. No API, backend query, polling-target, or source-selection contract
changed.

#### External Final-Current-Head Delivery Gate

This gate deliberately has no ledger checkbox because checking it would create
another unverified commit. After pushing the post-remediation commit, require
backend, frontend, ae-cli, and deploy-static to pass on its exact OID; then
recheck the clean worktree, local/remote/PR OID equality, OPEN draft state,
exact base/head, and MERGEABLE/CLEAN status. GitHub is the source of truth for
this mutable final gate. Do not edit this plan after that push unless another
new implementation or review finding changes the branch again.

## Self-Review Record

- Issue coverage: Task 1 covers bounds/order/projection/detail/latest active; Task 2 covers long-history DTO/wire bytes, primary/active/count plans, stable pages, complete detail, and apply/preview state semantics; Task 3 covers same-release consumer migration, same-source races, selection, and independent active polling; Task 4 covers docs/reviews/delivery.
- Consumer decision: current repository and organization searches found only the Vue consumer, so no deprecated unbounded compatibility path is planned.
- Type consistency: Task 1 produces `RunSummary`/`RunPage`; Task 3 mirrors them as `DirectoryRunSummary`/`DirectoryRunPage` while preserving full `DirectorySyncRun` detail.
- Ordering consistency: every backend, scale, frontend recovery, index, and review step uses `started_at DESC NULLS FIRST, id DESC`.
- Bound consistency: service and handler share default 20/max100/offset0; frontend page size is 20.
- Polling consistency: only page-independent latest active or a just-created active run is polled; terminal/history selection is detail-only.
- Placeholder scan: no TBD/TODO or unspecified implementation/test step remains.
- Data hygiene: all fixtures and examples are synthetic.
