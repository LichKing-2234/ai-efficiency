# PR Sync Job Progress And Usage Freshness Design

- **Date:** 2026-05-28
- **Status:** Implemented current contract
- **Scope:** `backend/`, `frontend/`, `docs/`
- **Related:**
  - [2026-05-20-pr-usage-snapshots-design.md](./2026-05-20-pr-usage-snapshots-design.md)
  - [2026-05-13-sessionless-local-tool-attribution-design.md](./2026-05-13-sessionless-local-tool-attribution-design.md)
  - [2026-05-14-legacy-session-staged-cutover-design.md](./2026-05-14-legacy-session-staged-cutover-design.md)
  - [2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md](./2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md)
  - [docs/architecture.md](../../architecture.md)

## Spec Relationship

This spec extends the current PR usage snapshot contract instead of replacing it.

The 2026-05-20 PR usage snapshot spec defines the product surface: repo PR rows show usage summaries, PR details show commit-level usage snapshots, and `Sync PRs` refreshes active PR usage without turning every sync into a full historical recalculation.

This spec keeps that contract and changes the sync execution model:

1. `Sync PRs` becomes a backend job instead of one long blocking HTTP request.
2. The job reports stage-level progress while it fetches, upserts, labels, and refreshes usage.
3. PR rows and PR details expose usage freshness reasons so delayed commit usage is visible and explainable.

Historical attribution specs remain background context. They should not override this job-based PR sync contract.

Implementation has landed in the current codebase. `POST /api/v1/repos/:id/sync-prs` creates or reuses a persisted `pr_sync_jobs` row, repo detail recovers the latest repo job through `GET /api/v1/repos/:id/pr-sync-job/latest`, and `GET /api/v1/pr-sync-jobs/:id` exposes phase/progress counters for polling. The 2026-06-03 large-repo recovery follow-up further implemented latest-job recovery, bounded PR list summary queries, Bitbucket PR timestamp ingestion, and stale queued/running job abandonment after more than one hour without progress.

## Problem Statement

Large repos can still make `Sync PRs` feel stuck or fail by timeout even after Bitbucket pagination was fixed.

Current code already paginates Bitbucket PR listing by sending `limit` and `start`, and `prsync.Service.fetchAllPRs` requests pages of 100 PRs. However, the user-facing API is still a single blocking request:

1. Fetch all PR pages from SCM.
2. Upsert all PR records.
3. Run the existing labeler.
4. Refresh usage for active PRs.
5. Return only the final summary.

This has three user-visible problems:

1. A large repo can exceed browser, proxy, or frontend request timeout budgets.
2. The frontend can only show `Syncing...`, not the current page, phase, or partial progress.
3. When commit data has already been uploaded but PR usage still looks empty or stale, the UI does not explain whether the system is waiting for usage upload, checkpoint binding, SCM commit refresh, or a snapshot refresh.

## Goals

1. Make repo PR sync safe for large repos by moving work into a backend job.
2. Show stage-level progress for PR fetching, upserting, labeling, and usage refresh.
3. Preserve the existing PR usage snapshot product model.
4. Distinguish PR metadata changes from unchanged PRs during sync.
5. Refresh usage incrementally, with explicit skip counts and skip reasons.
6. Show a PR-level usage freshness status in the PR list.
7. Show commit-level usage freshness reasons in PR details.
8. Explain delayed commit usage without changing the core fact chain: `tool_usage_events -> commit_checkpoints -> pr_commit_usage_snapshots`.
9. Avoid duplicate sync work when users click `Sync PRs` repeatedly for the same repo.

## Non-Goals

1. Do not introduce an external queue system in the first version.
2. Do not add a new standalone PR sync page.
3. Do not change `tool_usage_events` into a different fact model.
4. Do not make unbound or uncheckpointed usage count as valid PR usage.
5. Do not recover in-progress jobs across backend process restarts in the first version.
6. Do not backfill all historical closed PR usage as part of every normal sync.

## Current Implementation Reality

The current implementation is partially incremental:

1. PR metadata fetching is not incremental. The backend fetches all PR pages from SCM before upserting.
2. Bitbucket PR pagination is already present. The provider computes `start = (page - 1) * pageSize`.
3. Usage refresh is partially incremental. `prsync.Service.Sync` refreshes usage for created PRs, open PRs, and PRs created inside the recent active window.
4. Existing PRs are currently counted as updated even if their meaningful metadata did not change.
5. The current sync response only reports final `created`, `updated`, and `total` counts.
6. Frontend detail expansion can refresh one PR when no snapshot exists, but the list does not explain why usage is empty or delayed.
7. `prusage.RefreshPR` only aggregates usage that is already bound to a matching commit checkpoint. That remains the correct data contract.

## User Decisions Captured

The accepted design direction is:

1. Use a background job for `Sync PRs`.
2. Keep progress visible in the existing repo detail page.
3. Show both PR-level and commit-level usage freshness reasons.
4. Keep the implementation scoped to PR sync progress and usage freshness, not a broader attribution rewrite.

## Approaches Considered

### Option A: Backend Job Plus Stage Progress And Freshness Reasons

`POST /api/v1/repos/:id/sync-prs` creates or attaches to a repo-level job and returns immediately. The frontend polls the job endpoint and renders stage progress. The PR list and details expose usage freshness status.

Benefits:

1. Solves request timeout risk.
2. Makes long syncs observable.
3. Explains delayed PR usage.
4. Keeps the current repo detail workflow.

Costs:

1. Requires a job table and in-process worker lifecycle.
2. Requires frontend polling and job state rendering.

This is the recommended and approved option.

### Option B: Blocking Request With Better Messages

Keep the current sync endpoint blocking and add more response details.

Benefits:

1. Smallest backend change.
2. No job lifecycle.

Costs:

1. Does not solve long-request timeout.
2. Does not show live progress.
3. Still leaves users waiting for a final response.

This does not satisfy the chosen direction.

### Option C: Job With Percent Only

Move sync to a job, but only expose a single percentage.

Benefits:

1. Solves blocking timeout.
2. Simpler UI.

Costs:

1. Does not explain whether sync is blocked on SCM, DB upsert, labeler, or usage refresh.
2. Does not explain commit usage delay.

This under-solves the actual user problem.

## Architecture

The backend adds a repo-scoped PR sync job layer on top of the existing modular monolith.

```text
RepoDetailView
  POST /api/v1/repos/:id/sync-prs
    -> PRHandler creates or reuses running pr_sync_job
    -> response contains job_id

RepoDetailView
  GET /api/v1/pr-sync-jobs/:id
    -> polls status, phase, counters, errors

In-process worker
  -> fetch PR pages through SCMProvider
  -> upsert pr_records
  -> run existing labeler
  -> refresh usage through prusage.Service
  -> update pr_sync_jobs progress
```

The job layer should stay in a narrow backend package, for example `backend/internal/prsync`, and should continue to depend on `scm.SCMProvider`, `efficiency.Labeler`, and `prusage.Service` through existing interfaces.

The frontend stays in `RepoDetailView.vue`; no new top-level route is required.

## Data Model

### `pr_sync_jobs`

Add a new Ent schema for repo PR sync jobs.

Fields:

- `repo_config_id`
- `status`: `queued`, `running`, `completed`, `failed`, `cancelled`, `abandoned`
- `phase`: `queued`, `fetching_prs`, `upserting_prs`, `labeling`, `refreshing_usage`, `completed`, `failed`
- `page_size`
- `current_page`
- `fetched_prs`
- `total_prs`
- `processed_prs`
- `created_prs`
- `changed_prs`
- `unchanged_prs`
- `upsert_failed_prs`
- `labeled_prs`
- `label_failed_prs`
- `usage_total_prs`
- `usage_refreshed_prs`
- `usage_skipped_prs`
- `usage_failed_prs`
- `last_error`
- `error_summary`
- `started_at`
- `completed_at`
- `created_at`
- `updated_at`

`error_summary` should be compact JSON. It can store a bounded list of failed PR ids, operation names, and short messages. It must not store secrets or raw upstream response bodies.

### PR Usage Freshness Fields

The first version can compute these fields at read time and optionally persist them later if query cost becomes a problem.

PR-level response fields:

- `usage_status`: `fresh`, `pending_upload`, `no_checkpoint`, `no_usage_events`, `unbound`, `stale_snapshot`, `refresh_failed`, `unknown`
- `usage_status_reason`
- `usage_status_checked_at`

Commit-level response fields in PR details:

- `usage_status`
- `usage_status_reason`
- `checkpoint_found`
- `usage_event_found`

The existing `usage_refreshed_at` and `usage_commit_snapshot_hash` fields remain part of the snapshot contract.

## API Contract

### `POST /api/v1/repos/:id/sync-prs`

New behavior:

1. If no job is running for the repo, create a new `pr_sync_jobs` row and start an in-process worker.
2. If a job is already running for the repo, return that job instead of starting duplicate work.
3. Return immediately.

Response:

```json
{
  "job_id": 123,
  "status": "queued",
  "phase": "queued"
}
```

This endpoint intentionally stops returning final `created/updated/total` as its primary response. Final counts live on the job detail.

### `GET /api/v1/pr-sync-jobs/:id`

Returns the full job status and progress counters.

Response:

```json
{
  "id": 123,
  "repo_config_id": 5,
  "status": "running",
  "phase": "refreshing_usage",
  "current_page": 4,
  "page_size": 100,
  "fetched_prs": 350,
  "processed_prs": 350,
  "created_prs": 12,
  "changed_prs": 38,
  "unchanged_prs": 300,
  "usage_total_prs": 50,
  "usage_refreshed_prs": 41,
  "usage_skipped_prs": 8,
  "usage_failed_prs": 1,
  "last_error": ""
}
```

### `GET /api/v1/repos/:id/prs`

Keep the existing pagination and month filters.

Add usage freshness fields to each PR row. The list should expose one concise status per PR, not per-commit details.

The response also includes a top-level `summary` object for the full filtered result set, independent of `limit` and `offset` pagination:

```json
{
  "items": [],
  "total": 25,
  "summary": {
    "total": 25,
    "with_usage": 4,
    "pending_upload": 2,
    "no_checkpoint": 18,
    "refresh_failed": 1
  }
}
```

The frontend PR usage summary cards must use this aggregate `summary` when present, so the cards match the result total instead of only the current page.

### `GET /api/v1/prs/:id`

Keep existing PR details and commit snapshots.

Add commit-level freshness fields so expanded rows can explain missing or stale usage per commit.

### `POST /api/v1/prs/:id/refresh-usage`

Keep this endpoint for explicit single-PR refresh from details. It should return the same freshness fields as `GET /api/v1/prs/:id`.

## Worker Flow

### Phase 1: `fetching_prs`

The worker fetches SCM PRs page by page.

Rules:

1. Update `current_page` after each requested page.
2. Update `fetched_prs` after each successful page.
3. Stop when the page has fewer items than `page_size`, matching current `fetchAllPRs` behavior.
4. On page-level SCM failure, mark the job `failed` with the page number and error message.

The first implementation may keep PRs in memory after fetching all pages before upserting. Later optimization can stream page results into upsert. Progress must still update after each page in the first version.

### Phase 2: `upserting_prs`

Upsert PR metadata into `pr_records`.

The upsert result must distinguish:

- `created`: no prior PR record existed.
- `changed`: a prior record existed and meaningful stored fields changed.
- `unchanged`: a prior record existed and no meaningful stored fields changed.
- `failed`: this PR could not be upserted.

Meaningful fields include title, author, source branch, target branch, status, URL, lines added, lines deleted, labels, created time, and merged time.

### Phase 3: `labeling`

Run the existing labeler for synced PRs as today.

Labeler errors should:

1. Increment label failure counters.
2. Add a bounded error summary entry.
3. Not fail the whole job.

### Phase 4: `refreshing_usage`

Build the usage refresh candidate set from:

1. Created PRs.
2. Changed PRs.
3. Open PRs.
4. PRs in the active recent window.
5. PRs whose current SCM commit set hash differs from `usage_commit_snapshot_hash`.

Refresh only candidates. Count non-candidates as skipped with reason `unchanged_inactive`.

For each candidate:

1. Call `SCMProvider.ListPRCommits`.
2. Compare commit hash with existing `usage_commit_snapshot_hash`.
3. Refresh through `prusage.Service.RefreshPR` when needed.
4. Update usage counters and freshness status.

The sync job should continue when a single PR usage refresh fails. A failure on one PR must not hide progress for the rest of the repo.

## Usage Freshness Rules

The canonical PR usage number remains the snapshot produced from checkpoint-bound usage events.

Freshness status is diagnostic metadata that explains why a snapshot is missing, stale, or zero.

### PR-Level Status

`fresh`:

- `usage_refreshed_at` is set.
- Current PR commit set hash matches `usage_commit_snapshot_hash`.
- Refresh did not fail.

`pending_upload`:

- PR commits exist.
- Matching checkpoint or workspace scope suggests this repo has local activity.
- No matching usage events are available yet.
- This usually means async `ae-cli` usage upload has not completed.

`no_checkpoint`:

- PR commits exist.
- No matching `commit_checkpoints` can be found for the PR commit candidates, including rewrite expansion.

`no_usage_events`:

- Matching checkpoint exists.
- No `tool_usage_events` are bound to the checkpoint.

`unbound`:

- Related checkpoint or usage evidence exists, but repo/workspace scope does not match the PR repo scope.

`stale_snapshot`:

- A prior snapshot exists.
- The current PR commit set differs from `usage_commit_snapshot_hash`, or there is evidence of newer matching usage since `usage_refreshed_at`.

`refresh_failed`:

- The last explicit refresh attempt for this PR failed.

`unknown`:

- The system lacks enough evidence to choose a more specific status.

### Commit-Level Status

Commit details should show a status per commit snapshot row or per current PR commit when details are refreshed.

Commit-level statuses:

- `fresh`: usage events were found and included.
- `no_checkpoint`: no checkpoint candidate matched this commit or its rewrite chain.
- `no_usage_events`: checkpoint exists but has no bound usage events.
- `pending_upload`: workspace evidence suggests usage may still arrive.
- `unbound`: evidence exists under a different repo/workspace scope.
- `refresh_failed`: commit status could not be evaluated because the PR refresh failed.

## Frontend Contract

`RepoDetailView.vue` remains the primary surface.

### Sync Button Behavior

Clicking `Sync PRs`:

1. Calls `POST /api/v1/repos/:id/sync-prs`.
2. Stores `job_id`.
3. Starts polling `GET /api/v1/pr-sync-jobs/:id`.
4. Updates the button or status line with the current phase.
5. Stops polling when the job reaches a terminal state.
6. Refreshes the PR list after completion.

If the backend returns an existing running job, the frontend attaches to that job and starts polling it.

### Progress Display

The repo detail page should show:

- Current phase label.
- Page progress when fetching.
- PR processed count.
- Created, changed, unchanged counts.
- Usage refreshed, skipped, failed counts.
- Final result or failure message.

The UI should not rely on percentage alone because total PR count may not be known until fetching completes.

### PR List Usage Status

The PR table should add a compact usage status indicator.

Suggested labels:

- `Fresh`
- `Pending`
- `No checkpoint`
- `No usage`
- `Unbound`
- `Stale`
- `Failed`

The list should show one PR-level status only. Commit-level details belong in the expanded row.

### PR Details Commit Reasons

The expanded PR details should show a short reason next to each commit when usage is empty, missing, or stale.

Examples:

- `No checkpoint for this commit`
- `Checkpoint found, no usage events`
- `Waiting for local usage upload`
- `Snapshot stale, refresh needed`

## Error Handling

SCM page fetch failure:

- Mark the job `failed`.
- Keep `current_page`, `fetched_prs`, and `last_error`.

Single PR upsert failure:

- Continue the job.
- Increment `upsert_failed_prs`.
- Add a bounded error summary entry.

Label failure:

- Continue the job.
- Increment `label_failed_prs`.

Single PR usage refresh failure:

- Continue the job.
- Increment `usage_failed_prs`.
- Mark that PR `refresh_failed` when practical.

Backend restart:

- On startup or first job query, jobs left in `queued` or `running` from a previous process may be marked `abandoned`.
- The user can start a new sync.

Repeated clicks:

- Return the existing non-terminal job for the repo.
- Do not start a second worker.

Authorization:

- Creating and reading jobs should follow the same access expectations as repo PR sync and repo detail reads.

## Testing

### Backend

1. Creating a sync job returns immediately and starts job execution.
2. A second sync request for the same repo returns the existing running job.
3. Fetch progress updates after each SCM page.
4. Bitbucket still sends the correct `start` offset for later pages.
5. Upsert distinguishes `created`, `changed`, and `unchanged`.
6. Usage refresh candidate selection skips unchanged inactive PRs.
7. Single PR upsert, label, and usage refresh failures are counted without stopping the job.
8. SCM page failure marks the whole job failed.
9. Abandoned jobs are marked after process restart or stale running state.
10. PR list returns PR-level usage freshness fields.
11. PR details return commit-level freshness reasons.

### Frontend

1. `Sync PRs` starts a job and polls job detail.
2. Existing running job is reused and displayed.
3. Phase and counters render during polling.
4. Completed job refreshes the PR list.
5. Failed job shows the job error.
6. PR-level freshness badges render in the list.
7. Commit-level reasons render in the expanded detail row.

### Regression

1. Existing `GET /api/v1/repos/:id/prs` pagination and month filters continue to work.
2. Existing `POST /api/v1/prs/:id/refresh-usage` still refreshes a single PR.
3. Existing PR usage summary fields remain compatible with frontend formatting.
4. PR usage summary cards use the aggregate list `summary` and do not derive totals or freshness counts from the paginated current page when the backend summary is present.

## Rollout Notes

Recommended rollout order:

1. Add backend job schema, service, and handler tests.
2. Refactor `prsync.Service` into a progress-reporting execution path while preserving existing core logic.
3. Add usage freshness evaluation helpers.
4. Update PR list and detail API responses.
5. Update frontend polling and progress UI.
6. Update docs and architecture once implementation changes the current runtime relationship.

Because this spec changes a current product contract, implementation should also review `docs/architecture.md` after code lands. The architecture doc should be updated when the backend job worker exists in code, not during this design-only commit.
