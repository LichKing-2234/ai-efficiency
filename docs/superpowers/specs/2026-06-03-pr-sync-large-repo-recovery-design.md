# PR Sync Large Repo Recovery Design

- **Date:** 2026-06-03
- **Status:** Approved design, implementation not started
- **Scope:** `backend/`, `frontend/`, `docs/`
- **Related:**
  - [2026-05-28-pr-sync-job-progress-usage-freshness-design.md](./2026-05-28-pr-sync-job-progress-usage-freshness-design.md)
  - [2026-05-20-pr-usage-snapshots-design.md](./2026-05-20-pr-usage-snapshots-design.md)
  - [docs/architecture.md](../../architecture.md)

## Spec Relationship

This spec is a follow-up to the 2026-05-28 PR sync job progress design.

The earlier spec established the current product contract:

1. `POST /api/v1/repos/:id/sync-prs` starts or reuses a backend `pr_sync_jobs` row.
2. The backend process performs PR metadata sync and active PR usage refresh asynchronously.
3. The repo detail frontend polls `GET /api/v1/pr-sync-jobs/:id`.
4. PR list rows expose PR-level freshness while PR details expose commit-level freshness.

This spec keeps that model and closes large-repo usability gaps found after the first implementation. It does not replace the PR usage fact chain: `tool_usage_events -> commit_checkpoints -> pr_commit_usage_snapshots`.

## Problem Statement

Large repositories can still appear empty or stalled after a user starts `Sync PRs` and leaves the repo detail page.

The current behavior has four connected causes:

1. The repo detail page stores the active `job_id` only in component state. Leaving the page clears the polling timer and the page does not recover the latest repo-level job when it is opened again.
2. `ListByRepo` builds the PR summary by evaluating freshness for the full filtered result set. On a large repo this can require thousands of per-PR freshness checks before the first page renders.
3. The frontend currently catches the initial PR list request and silently substitutes an empty list. A slow or failed list call can look like "no PR records" instead of a loading or API failure.
4. The Bitbucket Server PR list adapter does not populate `scm.PR.CreatedAt` from Bitbucket's PR timestamps. Newly synced historical PRs therefore receive the database default `created_at`, so the default recent-window filter can include the full historical repository.

There is also a lifecycle gap: if the backend pod restarts while a job is running, the in-process worker is lost. The current first-version contract intentionally did not recover jobs across restarts, but a stale `running` row can block a future sync attempt.

## Goals

1. Reopen a repo detail page and show the latest active PR sync job without requiring the user to click `Sync PRs` again.
2. Keep large-repo PR list requests bounded to the requested page and lightweight aggregate queries.
3. Stop presenting PR list request failures as empty PR data.
4. Preserve correct recent-window filtering by ingesting Bitbucket PR timestamps.
5. Let stale `queued` or `running` jobs age out into `abandoned` so a new sync can start.
6. Keep the implementation within the current modular monolith and existing `pr_sync_jobs` schema.

## Non-Goals

1. Do not introduce Redis, a database-backed worker lease system, or an external queue in this iteration.
2. Do not guarantee automatic continuation of in-flight PR sync work after backend process restart.
3. Do not backfill historical usage for every closed PR as part of normal sync.
4. Do not change the PR usage fact model.
5. Do not add a standalone PR sync page.
6. Do not add a schema migration unless implementation discovers an unavoidable gap.

## User Decisions Captured

The accepted direction is a full product fix for the current PR sync surface:

1. Fix the immediate repo detail usability problem.
2. Optimize the large-repo list path.
3. Correct Bitbucket PR timestamp ingestion.
4. Add stale job handling using existing job states.
5. Defer a larger worker-runtime redesign.

## Approaches Considered

### Option A: Product Fix Inside Existing PR Sync Model

Add latest-job recovery, optimize the list/summary path, parse Bitbucket timestamps, and abandon stale jobs.

Benefits:

1. Directly fixes the observed user-facing issue.
2. Keeps scope local to `handler`, `prsync`, `prusage`, Bitbucket SCM provider, and repo detail UI.
3. Avoids a migration and avoids a new worker runtime.
4. Provides a clear upgrade path to a future durable worker.

Costs:

1. In-flight work still does not survive pod restart.
2. Stale job thresholds are heuristic.

This is the recommended and approved option.

### Option B: Frontend-Only Job Recovery

Have the repo detail page call a latest-job endpoint and resume polling, without changing PR list performance or Bitbucket timestamps.

Benefits:

1. Smallest implementation.
2. Restores progress visibility for active jobs.

Costs:

1. Large repo list calls can still time out or silently show empty results.
2. Historical Bitbucket PRs can still pollute the recent filter window.
3. Does not address stale running jobs.

This is insufficient for the current large-repo symptom.

### Option C: Durable Worker Runtime

Move PR sync execution to a persistent job runner with leases, retries, and restart recovery.

Benefits:

1. Solves process restart recovery correctly.
2. Creates a foundation for other long-running backend jobs.

Costs:

1. Larger architectural change.
2. Requires careful lease semantics, retry policy, and operational visibility.
3. Delays the current product fix.

This remains a future direction, not this iteration.

## Backend Design

### Latest Repo Sync Job

Add a repo-scoped latest-job read path:

```text
GET /api/v1/repos/:id/pr-sync-job/latest
```

Response shape should match `GET /api/v1/pr-sync-jobs/:id`, with `null` or an explicit empty data payload when the repo has no job.

The backend should query `pr_sync_jobs` by `repo_config_id`, order by `created_at DESC`, and return the newest row. The endpoint should use the same protected route group and access expectations as repo detail and PR list reads.

The handler should not start work. It only reports durable job state.

### Stale Job Handling

`prsync.Service.StartSyncJob` currently reuses any `queued` or `running` job for the repo.

Before reusing an active job, the service should check whether it is stale:

1. `updated_at` is older than a configured or hard-coded conservative threshold.
2. The job is still `queued` or `running`.
3. The job has no `completed_at`.

For this iteration, a one-hour threshold is acceptable. It is long enough to avoid abandoning a slow active large-repo usage refresh, while preventing abandoned rows from blocking the next user action indefinitely.

When stale, update the row to:

```text
status = abandoned
phase = failed
last_error = "PR sync job was abandoned after no progress was recorded for more than 1h."
completed_at = now
```

Then create a new queued job.

This does not recover work. It only lets the user start over when the in-process worker is no longer making progress.

### Large-Repo PR List Summary

`PRHandler.ListByRepo` should stop calling `EvaluatePRFreshness` for every PR in the full filtered result.

The list response still returns:

```json
{
  "items": [],
  "summary": {
    "total": 0,
    "with_usage": 0,
    "pending_upload": 0,
    "no_checkpoint": 0,
    "refresh_failed": 0
  },
  "total": 0
}
```

But `summary` should be computed with bounded SQL queries:

1. `total`: count of the filtered PR query.
2. `with_usage`: count filtered PRs where token, credit, or request totals are non-zero.
3. `no_checkpoint`: count filtered PRs where `usage_refreshed_at IS NULL` and no usage totals exist.
4. `refresh_failed`: keep this as zero in this iteration because there is no durable per-PR refresh failure marker. Job-level usage failures remain visible on `pr_sync_jobs.usage_failed_prs`.
5. `pending_upload`: repo-level pending unbound usage count should not require scanning every PR. If the repo has pending unbound usage events and a row has no snapshot, current-page item freshness may show `pending_upload`; the aggregate may use a cheap approximate count or remain conservative.

The important contract is that list response time must not scale with every filtered PR's commit-level freshness evaluation.

Current-page `items` can still call `EvaluatePRFreshness` for the page size, because the page size is small.

### Bitbucket PR Timestamps

Update `backend/internal/scm/bitbucket.Provider.ListPRs` to parse Bitbucket Server timestamp fields:

1. `createdDate` into `scm.PR.CreatedAt`.
2. `closedDate` into `scm.PR.MergedAt` only when the PR state maps to `merged`, if Bitbucket payload exposes enough state to distinguish merged from declined.

Bitbucket Server timestamps are epoch milliseconds. The adapter should convert them to `time.UnixMilli(...).UTC()`.

If a timestamp is missing or zero, leave the existing zero-value behavior unchanged. Do not add a new persisted PR updated-time field in this iteration.

Because `prsync.upsertPR` already sets `created_at` when `scm.PR.CreatedAt` is non-zero, the next sync can repair historical rows that were previously created with the database default.

### Active Usage Refresh Scope

The existing active usage refresh rule should remain:

1. Newly created PRs.
2. Changed PRs.
3. Open PRs.
4. PRs whose SCM-created timestamp is inside the recent active window.

After Bitbucket timestamps are fixed, old closed PRs should no longer be treated as recent just because they were first seen by the database today.

## Frontend Design

### Recover Active Job On Page Load

`RepoDetailView` should call the latest-job endpoint during mount.

If the latest job is non-terminal:

1. Set `syncJob`.
2. Set `syncing = true`.
3. Start polling that job id.

If the latest job is terminal:

1. Optionally show the most recent terminal status in a compact message if it is recent enough to be useful.
2. Do not mark the sync button as active.

The sync button should still call `POST /repos/:id/sync-prs`. If a job is already active, the backend returns the reused job and the same polling path continues.

### Do Not Hide PR List Failures

The initial `listPRs` load should track an explicit PR list state:

```text
idle | loading | loaded | error
```

When the request fails, the UI should show a retryable error state rather than replacing data with `[]`.

The empty state should be shown only after a successful list response with zero rows.

### Keep Page Rendering Bounded

The UI should continue to request small pages. The summary cards should use the backend aggregate `summary` when present, and should not derive total counts from the current page except as a fallback for older API responses.

## Error Handling

1. Latest-job lookup failure should not redirect away from repo detail. It should show a non-blocking sync-status error and still try to load repo metadata and PR rows.
2. PR list failure should show a retry affordance and preserve any existing loaded PR rows until a successful refresh replaces them.
3. Stale job abandonment should be visible through `last_error`.
4. A single PR usage refresh failure inside a running job should continue incrementing `usage_failed_prs` and should not fail the whole job unless the current implementation already treats that error as fatal.

## Testing

### Backend

1. Bitbucket `ListPRs` parses `createdDate` into `scm.PR.CreatedAt`.
2. Bitbucket pagination still translates page number to `start` offset.
3. `StartSyncJob` reuses a fresh running job.
4. `StartSyncJob` abandons a stale running job and creates a new queued job.
5. Latest-job endpoint returns the newest job for a repo.
6. Latest-job endpoint returns an empty result for a repo with no jobs.
7. `ListByRepo` summary uses bounded aggregate behavior and still returns current-page freshness fields.
8. `ListByRepo` applies default recent-window filtering using stored PR `created_at`.

### Frontend

1. Repo detail mount recovers a running latest job and starts polling.
2. Repo detail does not recover a terminal old job as active.
3. `Sync PRs` still starts or reuses a job and renders progress.
4. PR list failure renders an error state, not the empty state.
5. Successful empty PR list renders the empty state.

### Verification Commands

Use the existing repo defaults:

```text
cd backend && go test ./internal/scm/bitbucket ./internal/prsync ./internal/handler
cd frontend && pnpm test
```

If backend tests need Postgres-specific behavior, run them separately with the repo's documented local test DSN.

## Documentation Updates

Implementation should update:

1. `docs/architecture.md` to mention latest job recovery, bounded list summaries, and stale job abandonment.
2. This spec's status once implemented.

The older 2026-05-28 spec should remain as historical design context. Do not rewrite it wholesale. If implementation changes the current contract beyond this spec, add a new spec or update this follow-up spec.

## Open Risks

1. `pending_upload` aggregate semantics may need a conservative approximation because this iteration does not add durable per-PR pending status.
2. One hour may be too short for extreme usage refresh jobs. If production evidence shows legitimate jobs exceeding this without progress updates, the threshold should become configurable.
3. Correctly distinguishing Bitbucket merged versus declined states depends on the exact PR list payload. The implementation should only set `MergedAt` when the payload provides enough information.
4. Existing rows with incorrect `created_at` will be repaired only after another successful sync with timestamp parsing enabled.
