# PR Sync Large Repo Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make large-repo PR sync usable after page navigation by recovering active jobs, bounding PR list work, preserving Bitbucket PR timestamps, and preventing stale jobs from blocking new syncs.

**Architecture:** Keep the existing in-process `pr_sync_jobs` model. Add a repo-scoped latest-job read path, abandon stale active jobs before reuse, compute PR list summaries with bounded SQL aggregation, and let the repo detail frontend recover active jobs on mount. Do not introduce a queue, new worker runtime, or schema migration.

**Tech Stack:** Go, Gin, Ent, Bitbucket Server REST adapter, Vue 3 `<script setup>`, Pinia, Vitest, TailwindCSS.

---

## Status

Implementation not started. All task checkboxes are intentionally unchecked.

## File Structure

- Modify: `backend/internal/prsync/service.go`
  - Add stale active job handling.
  - Add latest repo job lookup.
- Modify: `backend/internal/prsync/job_test.go`
  - Cover stale job abandonment and latest job lookup.
- Modify: `backend/internal/handler/interfaces.go`
  - Extend `prSyncer` with latest repo job lookup.
- Modify: `backend/internal/handler/pr.go`
  - Add latest-job handler.
  - Add bounded PR list summary logic.
  - Reuse a shared job response serializer.
- Modify: `backend/internal/handler/router.go`
  - Register `GET /api/v1/repos/:id/pr-sync-job/latest`.
- Modify: `backend/internal/handler/pr_sync_job_test.go`
  - Cover latest-job endpoint.
- Modify: `backend/internal/handler/handler_extended_test.go`
  - Cover bounded summary semantics.
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
  - Parse `createdDate` and safe `closedDate`.
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`
  - Cover Bitbucket timestamp parsing and keep pagination coverage.
- Modify: `frontend/src/api/pr.ts`
  - Add `getLatestPRSyncJob(repoId)`.
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
  - Recover latest active job on mount.
  - Track PR list load errors explicitly.
  - Stop showing empty state for failed list calls.
- Modify: `frontend/src/i18n.ts`
  - Add retryable PR list error strings in English and Chinese.
- Modify: `frontend/src/__tests__/api-modules.test.ts`
  - Cover latest-job API helper.
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`
  - Cover job recovery and PR list error state.
- Modify: `docs/architecture.md`
  - Document latest-job recovery, bounded summaries, Bitbucket timestamps, and stale job abandonment.
- Modify: `docs/superpowers/specs/2026-06-03-pr-sync-large-repo-recovery-design.md`
  - Update status after implementation.

---

### Task 1: Backend Job Recovery And Stale Job Handling

**Files:**
- Modify: `backend/internal/prsync/service.go`
- Modify: `backend/internal/prsync/job_test.go`
- Modify: `backend/internal/handler/interfaces.go`
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/pr_sync_job_test.go`

- [x] **Step 1: Write stale job service tests**

Append these tests to `backend/internal/prsync/job_test.go`:

```go
func TestStartSyncJobAbandonsStaleRunningJob(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-stale-repo")
	staleUpdatedAt := time.Now().UTC().Add(-2 * time.Hour)
	existing := client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.PhaseRefreshingUsage).
		SetUpdatedAt(staleUpdatedAt).
		SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if reused {
		t.Fatalf("reused = true, want false for stale running job")
	}
	if job.ID == existing.ID {
		t.Fatalf("new job id = stale job id %d", existing.ID)
	}
	stale := client.PRSyncJob.GetX(ctx, existing.ID)
	if stale.Status != prsyncjob.StatusAbandoned {
		t.Fatalf("stale status = %s, want abandoned", stale.Status)
	}
	if stale.CompletedAt == nil {
		t.Fatalf("stale completed_at is nil, want abandonment timestamp")
	}
	if stale.LastError == nil || !strings.Contains(*stale.LastError, "abandoned") {
		t.Fatalf("stale last_error = %v, want abandonment reason", stale.LastError)
	}
}

func TestStartSyncJobReusesFreshRunningJobWithOldStart(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-fresh-old-start-repo")
	existing := client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.PhaseRefreshingUsage).
		SetStartedAt(time.Now().UTC().Add(-2 * time.Hour)).
		SetUpdatedAt(time.Now().UTC().Add(-5 * time.Minute)).
		SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if !reused || job.ID != existing.ID {
		t.Fatalf("job id=%d reused=%v, want reuse of fresh running job %d", job.ID, reused, existing.ID)
	}
}

func TestGetLatestSyncJobForRepo(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-latest-repo")
	client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusCompleted).
		SetPhase(prsyncjob.PhaseCompleted).
		SetCreatedAt(time.Now().UTC().Add(-time.Hour)).
		SaveX(ctx)
	latest := client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.PhaseFetchingPrs).
		SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	job, err := svc.GetLatestSyncJobForRepo(ctx, rc.ID)
	if err != nil {
		t.Fatalf("GetLatestSyncJobForRepo error: %v", err)
	}
	if job.ID != latest.ID {
		t.Fatalf("job id = %d, want latest %d", job.ID, latest.ID)
	}
}
```

Add `strings` to the import block in `backend/internal/prsync/job_test.go`.

- [x] **Step 2: Run stale job tests and verify failure**

Run:

```bash
cd backend && go test ./internal/prsync -run 'TestStartSyncJobAbandonsStaleRunningJob|TestStartSyncJobReusesFreshRunningJobWithOldStart|TestGetLatestSyncJobForRepo' -count=1
```

Expected: FAIL because `GetLatestSyncJobForRepo` and stale abandonment are not implemented yet.

- [x] **Step 3: Implement stale job handling and latest repo lookup**

In `backend/internal/prsync/service.go`, add near the top-level declarations:

```go
const staleSyncJobThreshold = time.Hour

const staleSyncJobMessage = "PR sync job was abandoned after no progress was recorded for more than 1h."
```

Replace the existing `StartSyncJob` active-job reuse block with logic equivalent to:

```go
existing, err := s.entClient.PRSyncJob.Query().
	Where(
		prsyncjob.RepoConfigIDEQ(rc.ID),
		prsyncjob.StatusIn(prsyncjob.StatusQueued, prsyncjob.StatusRunning),
	).
	Order(ent.Desc(prsyncjob.FieldCreatedAt)).
	First(ctx)
if err == nil {
	if s.isStaleSyncJob(existing, time.Now().UTC()) {
		if err := s.abandonStaleSyncJob(ctx, existing.ID); err != nil {
			return nil, false, err
		}
	} else {
		return existing, true, nil
	}
} else if !ent.IsNotFound(err) {
	return nil, false, fmt.Errorf("query running PR sync job: %w", err)
}
```

Add these helper methods:

```go
func (s *Service) isStaleSyncJob(job *ent.PRSyncJob, now time.Time) bool {
	if job == nil || job.CompletedAt != nil {
		return false
	}
	if job.Status != prsyncjob.StatusQueued && job.Status != prsyncjob.StatusRunning {
		return false
	}
	return now.Sub(job.UpdatedAt.UTC()) > staleSyncJobThreshold
}

func (s *Service) abandonStaleSyncJob(ctx context.Context, jobID int) error {
	msg := staleSyncJobMessage
	if err := s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusAbandoned).
		SetPhase(prsyncjob.PhaseFailed).
		SetLastError(msg).
		SetCompletedAt(time.Now().UTC()).
		Exec(ctx); err != nil {
		return fmt.Errorf("abandon stale PR sync job: %w", err)
	}
	return nil
}

func (s *Service) GetLatestSyncJobForRepo(ctx context.Context, repoID int) (*ent.PRSyncJob, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("get latest PR sync job: ent client is required")
	}
	job, err := s.entClient.PRSyncJob.Query().
		Where(prsyncjob.RepoConfigIDEQ(repoID)).
		Order(ent.Desc(prsyncjob.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest PR sync job for repo %d: %w", repoID, err)
	}
	return job, nil
}
```

- [x] **Step 4: Run stale job tests and verify pass**

Run:

```bash
cd backend && go test ./internal/prsync -run 'TestStartSyncJobAbandonsStaleRunningJob|TestStartSyncJobReusesFreshRunningJobWithOldStart|TestGetLatestSyncJobForRepo' -count=1
```

Expected: PASS.

- [x] **Step 5: Write latest-job handler tests**

Update `backend/internal/handler/pr_sync_job_test.go`.

Add a `latestFn` to `mockPRSyncJobber`:

```go
latestFn func(ctx context.Context, repoID int) (*ent.PRSyncJob, error)
```

Add this method:

```go
func (m *mockPRSyncJobber) GetLatestSyncJobForRepo(ctx context.Context, repoID int) (*ent.PRSyncJob, error) {
	if m.latestFn == nil {
		return nil, nil
	}
	return m.latestFn(ctx, repoID)
}
```

Register the route in `attachPRSyncJobRoutes`:

```go
api.GET("/repos/:id/pr-sync-job/latest", prHandler.GetLatestSyncJobForRepo)
```

Append these tests:

```go
func TestGetLatestPRSyncJobForRepoReturnsProgress(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
			return nil, false, nil
		},
		getFn: func(ctx context.Context, id int) (*ent.PRSyncJob, error) { return nil, nil },
		latestFn: func(ctx context.Context, repoID int) (*ent.PRSyncJob, error) {
			return &ent.PRSyncJob{ID: 77, RepoConfigID: repoID, Status: prsyncjob.StatusRunning, Phase: prsyncjob.PhaseRefreshingUsage, FetchedPrs: 500, ProcessedPrs: 450}, nil
		},
	})

	w := doMockRequest(env, "GET", "/api/v1/repos/9/pr-sync-job/latest", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["id"] != float64(77) || data["status"] != "running" || data["phase"] != "refreshing_usage" {
		t.Fatalf("data = %+v, want running latest job 77", data)
	}
}

func TestGetLatestPRSyncJobForRepoReturnsNullWhenMissing(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
			return nil, false, nil
		},
		getFn:    func(ctx context.Context, id int) (*ent.PRSyncJob, error) { return nil, nil },
		latestFn: func(ctx context.Context, repoID int) (*ent.PRSyncJob, error) { return nil, nil },
	})

	w := doMockRequest(env, "GET", "/api/v1/repos/9/pr-sync-job/latest", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	if resp["data"] != nil {
		t.Fatalf("data = %#v, want nil", resp["data"])
	}
}
```

- [x] **Step 6: Run latest-job handler tests and verify failure**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestGetLatestPRSyncJobForRepo' -count=1
```

Expected: FAIL because handler/interface/route are not implemented yet.

- [x] **Step 7: Implement latest-job handler and shared serializer**

Modify `backend/internal/handler/interfaces.go`:

```go
type prSyncer interface {
	Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	StartSyncJob(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error)
	RunSyncJob(ctx context.Context, jobID int, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error)
	GetLatestSyncJobForRepo(ctx context.Context, repoID int) (*ent.PRSyncJob, error)
}
```

In `backend/internal/handler/pr.go`, add:

```go
func serializePRSyncJob(job *ent.PRSyncJob) gin.H {
	if job == nil {
		return nil
	}
	return gin.H{
		"id":                  job.ID,
		"repo_config_id":      job.RepoConfigID,
		"status":              string(job.Status),
		"phase":               string(job.Phase),
		"current_page":        job.CurrentPage,
		"page_size":           job.PageSize,
		"fetched_prs":         job.FetchedPrs,
		"total_prs":           job.TotalPrs,
		"processed_prs":       job.ProcessedPrs,
		"created_prs":         job.CreatedPrs,
		"changed_prs":         job.ChangedPrs,
		"unchanged_prs":       job.UnchangedPrs,
		"usage_total_prs":     job.UsageTotalPrs,
		"usage_refreshed_prs": job.UsageRefreshedPrs,
		"usage_skipped_prs":   job.UsageSkippedPrs,
		"usage_failed_prs":    job.UsageFailedPrs,
		"last_error":          job.LastError,
	}
}
```

Replace the response body in `GetSyncJob` with:

```go
pkg.Success(c, serializePRSyncJob(job))
```

Add the handler:

```go
// GetLatestSyncJobForRepo handles GET /api/v1/repos/:id/pr-sync-job/latest.
func (h *PRHandler) GetLatestSyncJobForRepo(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.syncService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr sync service is not configured")
		return
	}
	job, err := h.syncService.GetLatestSyncJobForRepo(c.Request.Context(), repoID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, serializePRSyncJob(job))
}
```

In `backend/internal/handler/router.go`, add under the repo group:

```go
repoGroup.GET("/:id/pr-sync-job/latest", prHandler.GetLatestSyncJobForRepo)
```

Run this check to find any additional handler test mocks that now need the new interface method:

```bash
rg -n "type .*prSync|mockPRSync|GetSyncJob\\(|StartSyncJob" backend/internal/handler
```

Expected: every mock type used as `prSyncer` has a `GetLatestSyncJobForRepo` method. In the current codebase this is `mockPRSyncJobber` in `backend/internal/handler/pr_sync_job_test.go`.

- [x] **Step 8: Run backend job recovery tests**

Run:

```bash
cd backend && go test ./internal/prsync ./internal/handler -run 'TestStartSyncJobAbandonsStaleRunningJob|TestStartSyncJobReusesFreshRunningJobWithOldStart|TestGetLatestSyncJobForRepo|TestGetLatestPRSyncJobForRepo' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit backend job recovery**

Run:

```bash
git add backend/internal/prsync/service.go backend/internal/prsync/job_test.go backend/internal/handler/interfaces.go backend/internal/handler/pr.go backend/internal/handler/router.go backend/internal/handler/pr_sync_job_test.go
git commit -m "fix(prsync): recover latest repo sync jobs"
```

---

### Task 2: Bounded PR List Summary

**Files:**
- Modify: `backend/internal/handler/pr.go`
- Modify: `backend/internal/handler/handler_extended_test.go`

- [x] **Step 1: Write a summary regression test with a counting freshness evaluator**

Add this helper type to `backend/internal/handler/handler_extended_test.go` near the PR summary tests:

```go
type countingFreshnessEvaluator struct {
	calls int
}

func (e *countingFreshnessEvaluator) EvaluatePRFreshness(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
	e.calls++
	return &prusage.PRFreshness{
		Status:    prusage.UsageStatusNoCheckpoint,
		Reason:    "test evaluator",
		CheckedAt: time.Now().UTC(),
	}, nil
}
```

Add imports:

```go
github.com/ai-efficiency/backend/internal/prusage
```

Append this test:

```go
func TestPRListByRepoEvaluatesFreshnessOnlyForCurrentPage(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	repoID := createTestRepo(t, env.client)
	now := time.Now().UTC()
	for i := 0; i < 25; i++ {
		create := env.client.PrRecord.Create().
			SetRepoConfigID(repoID).
			SetScmPrID(9000 + i).
			SetTitle(fmt.Sprintf("large repo pr %d", i)).
			SetAuthor("alice").
			SetStatus(prrecord.StatusMerged).
			SetCreatedAt(now.Add(-time.Duration(i) * time.Hour))
		if i == 0 {
			create.SetUsageInputTokens(10)
		}
		create.SaveX(ctx)
	}

	evaluator := &countingFreshnessEvaluator{}
	prHandler := NewPRHandler(env.client, nil, nil, nil)
	prHandler.usageFreshness = evaluator
	group := env.router.Group("/api/v1/test-bounded-summary")
	group.GET("/repos/:id/prs", prHandler.ListByRepo)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/test-bounded-summary/repos/%d/prs?limit=5&months=3", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if evaluator.calls != 5 {
		t.Fatalf("freshness calls = %d, want only current page size 5", evaluator.calls)
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	summary := data["summary"].(map[string]interface{})
	if got := int(summary["total"].(float64)); got != 25 {
		t.Fatalf("summary.total = %d, want 25", got)
	}
	if got := int(summary["with_usage"].(float64)); got != 1 {
		t.Fatalf("summary.with_usage = %d, want 1", got)
	}
}
```

- [x] **Step 2: Run bounded summary test and verify failure**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestPRListByRepoEvaluatesFreshnessOnlyForCurrentPage' -count=1
```

Expected: FAIL because current `buildPRListSummary` evaluates all 25 PRs.

- [x] **Step 3: Implement bounded summary queries**

In `backend/internal/handler/pr.go`, replace `buildPRListSummary` with aggregate count logic.

Use a helper that accepts the filtered query and `total`:

```go
func (h *PRHandler) buildPRListSummary(ctx context.Context, query *ent.PrRecordQuery, total int) (prListSummary, error) {
	summary := prListSummary{Total: total}
	withUsage, err := query.Clone().
		Where(prrecord.Or(
			prrecord.UsageInputTokensGT(0),
			prrecord.UsageOutputTokensGT(0),
			prrecord.UsageCachedInputTokensGT(0),
			prrecord.UsageReasoningTokensGT(0),
			prrecord.UsageCreditUsageGT(0),
			prrecord.UsageRequestCountGT(0),
		)).
		Count(ctx)
	if err != nil {
		return summary, err
	}
	summary.WithUsage = withUsage

	noCheckpoint, err := query.Clone().
		Where(
			prrecord.UsageRefreshedAtIsNil(),
			prrecord.UsageInputTokensEQ(0),
			prrecord.UsageOutputTokensEQ(0),
			prrecord.UsageCachedInputTokensEQ(0),
			prrecord.UsageReasoningTokensEQ(0),
			prrecord.UsageCreditUsageEQ(0),
			prrecord.UsageRequestCountEQ(0),
		).
		Count(ctx)
	if err != nil {
		return summary, err
	}
	summary.NoCheckpoint = noCheckpoint
	summary.PendingUpload = 0
	summary.RefreshFailed = 0
	return summary, nil
}
```

Keep `buildPRResponse` unchanged so current page items still include freshness.

- [x] **Step 4: Run summary tests**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestPRListByRepoIncludesAggregateUsageSummary|TestPRListByRepoEvaluatesFreshnessOnlyForCurrentPage' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit bounded summary**

Run:

```bash
git add backend/internal/handler/pr.go backend/internal/handler/handler_extended_test.go
git commit -m "fix(backend): bound PR list summary work"
```

---

### Task 3: Bitbucket PR Timestamp Ingestion

**Files:**
- Modify: `backend/internal/scm/bitbucket/bitbucket.go`
- Modify: `backend/internal/scm/bitbucket/bitbucket_test.go`

- [ ] **Step 1: Write Bitbucket timestamp parsing test**

Modify `TestListPRs` in `backend/internal/scm/bitbucket/bitbucket_test.go` so the test payload includes timestamps:

```json
{
  "values": [
    {
      "id": 1,
      "title": "T",
      "state": "MERGED",
      "createdDate": 1714540800123,
      "closedDate": 1714627200456,
      "author": { "user": { "name": "alice" } },
      "fromRef": { "displayId": "f" },
      "toRef": { "displayId": "main" },
      "links": { "self": [{ "href": "http://bb/pr/1" }] }
    }
  ]
}
```

Add assertions after `prs` is returned:

```go
if prs[0].CreatedAt.IsZero() || prs[0].CreatedAt.UnixMilli() != int64(1714540800123) {
	t.Fatalf("CreatedAt = %v, want epoch millis 1714540800123", prs[0].CreatedAt)
}
if prs[0].MergedAt.IsZero() || prs[0].MergedAt.UnixMilli() != int64(1714627200456) {
	t.Fatalf("MergedAt = %v, want epoch millis 1714627200456", prs[0].MergedAt)
}
```

Add a second test:

```go
func TestListPRsDoesNotSetMergedAtForDeclinedPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"values":[{"id":2,"title":"Declined","state":"DECLINED","createdDate":1714540800000,"closedDate":1714627200000,"author":{"user":{"name":"alice"}},"fromRef":{"displayId":"f"},"toRef":{"displayId":"main"},"links":{"self":[{"href":"http://bb/pr/2"}]}}]}`)
	}))
	defer srv.Close()

	p := New(srv.URL, "token", zap.NewNop())
	prs, err := p.ListPRs(context.Background(), "P/r", scm.PRListOpts{State: "all", PageSize: 10})
	if err != nil {
		t.Fatalf("ListPRs error: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("len(prs) = %d, want 1", len(prs))
	}
	if prs[0].CreatedAt.IsZero() {
		t.Fatalf("CreatedAt is zero, want parsed createdDate")
	}
	if !prs[0].MergedAt.IsZero() {
		t.Fatalf("MergedAt = %v, want zero for declined PR", prs[0].MergedAt)
	}
}
```

- [ ] **Step 2: Run Bitbucket timestamp tests and verify failure**

Run:

```bash
cd backend && go test ./internal/scm/bitbucket -run 'TestListPRs|TestListPRsDoesNotSetMergedAtForDeclinedPR' -count=1
```

Expected: FAIL because `createdDate` and `closedDate` are not parsed.

- [ ] **Step 3: Implement timestamp parsing**

In `backend/internal/scm/bitbucket/bitbucket.go`, add timestamp fields to the `Values` struct in `ListPRs`:

```go
CreatedDate int64 `json:"createdDate"`
ClosedDate  int64 `json:"closedDate"`
```

When constructing `scm.PR`, use a local variable:

```go
state := strings.ToLower(v.State)
item := &scm.PR{
	ID:           v.ID,
	Title:        v.Title,
	Author:       v.Author.User.Name,
	SourceBranch: v.FromRef.DisplayID,
	TargetBranch: v.ToRef.DisplayID,
	State:        state,
	URL:          url,
}
if v.CreatedDate > 0 {
	item.CreatedAt = time.UnixMilli(v.CreatedDate).UTC()
}
if state == "merged" && v.ClosedDate > 0 {
	item.MergedAt = time.UnixMilli(v.ClosedDate).UTC()
}
prs = append(prs, item)
```

Add `time` to the import block if it is not already present.

- [ ] **Step 4: Run Bitbucket tests**

Run:

```bash
cd backend && go test ./internal/scm/bitbucket -run 'TestListPRs|TestListPRsUsesStartOffsetForPage|TestListPRsDoesNotSetMergedAtForDeclinedPR' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Bitbucket timestamp ingestion**

Run:

```bash
git add backend/internal/scm/bitbucket/bitbucket.go backend/internal/scm/bitbucket/bitbucket_test.go
git commit -m "fix(scm): ingest Bitbucket PR timestamps"
```

---

### Task 4: Frontend Job Recovery And PR List Error State

**Files:**
- Modify: `frontend/src/api/pr.ts`
- Modify: `frontend/src/views/repos/RepoDetailView.vue`
- Modify: `frontend/src/i18n.ts`
- Modify: `frontend/src/__tests__/api-modules.test.ts`
- Modify: `frontend/src/__tests__/repo-detail-view.test.ts`

- [ ] **Step 1: Add API helper test**

In `frontend/src/__tests__/api-modules.test.ts`, update the import from `@/api/pr` to include `getLatestPRSyncJob`.

Add this test near the PR API tests:

```ts
it('getLatestPRSyncJob fetches the latest repo PR sync job', async () => {
  const { getLatestPRSyncJob } = await import('@/api/pr')
  await getLatestPRSyncJob(5)
  expect(mockClient.get).toHaveBeenCalledWith('/repos/5/pr-sync-job/latest')
})
```

- [ ] **Step 2: Run API helper test and verify failure**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts -t 'getLatestPRSyncJob'
```

Expected: FAIL because the helper does not exist.

- [ ] **Step 3: Implement API helper**

In `frontend/src/api/pr.ts`, add:

```ts
export function getLatestPRSyncJob(repoId: number) {
  return client.get<ApiResponse<PRSyncJob | null>>(`/repos/${repoId}/pr-sync-job/latest`)
}
```

- [ ] **Step 4: Add repo detail recovery and error tests**

Update the `vi.mock('@/api/pr')` block in `frontend/src/__tests__/repo-detail-view.test.ts`:

```ts
getLatestPRSyncJob: vi.fn(),
```

In `mountRepoDetail`, import and default the mock:

```ts
const { listPRs, getPR, refreshPRUsage, getLatestPRSyncJob } = await import('@/api/pr')
;(getLatestPRSyncJob as any).mockResolvedValue({ data: { data: null } })
```

Append these tests:

```ts
it('recovers a running PR sync job on page load', async () => {
  vi.useFakeTimers()
  const { getLatestPRSyncJob, getPRSyncJob } = await import('@/api/pr')
  ;(getLatestPRSyncJob as any).mockResolvedValue({
    data: { data: { id: 77, repo_config_id: 9, status: 'running', phase: 'refreshing_usage', current_page: 126, page_size: 100, fetched_prs: 12556, total_prs: 12556, processed_prs: 12556, created_prs: 12556, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 12556, usage_refreshed_prs: 3085, usage_skipped_prs: 0, usage_failed_prs: 0 } },
  })
  ;(getPRSyncJob as any).mockResolvedValue({
    data: { data: { id: 77, repo_config_id: 9, status: 'running', phase: 'refreshing_usage', current_page: 126, page_size: 100, fetched_prs: 12556, total_prs: 12556, processed_prs: 12556, created_prs: 12556, changed_prs: 0, unchanged_prs: 0, usage_total_prs: 12556, usage_refreshed_prs: 3086, usage_skipped_prs: 0, usage_failed_prs: 0 } },
  })

  try {
    const { wrapper } = await mountRepoDetail()
    expect(wrapper.text()).toContain('Refreshing usage')
    expect(wrapper.text()).toContain('12,556 fetched')
    const syncButton = wrapper.findAll('button').find((b) => b.text() === 'Syncing...')
    expect(syncButton).toBeTruthy()
  } finally {
    vi.useRealTimers()
  }
})

it('shows PR list load errors instead of empty state', async () => {
  const { listPRs } = await import('@/api/pr')
  ;(listPRs as any).mockRejectedValue(new Error('backend timeout'))

  const { wrapper } = await mountRepoDetail()

  expect(wrapper.text()).toContain('Failed to load pull requests')
  expect(wrapper.text()).toContain('Retry')
  expect(wrapper.text()).not.toContain('No pull requests recorded yet.')
})
```

- [ ] **Step 5: Run repo detail tests and verify failure**

Run:

```bash
cd frontend && pnpm test -- repo-detail-view.test.ts -t 'recovers a running PR sync job|shows PR list load errors'
```

Expected: FAIL because job recovery and explicit PR list error state are not implemented.

- [ ] **Step 6: Implement frontend recovery and error state**

In `frontend/src/api/pr.ts`, keep the helper added in Step 3.

In `frontend/src/views/repos/RepoDetailView.vue`, update the import:

```ts
import { getLatestPRSyncJob, getPR, getPRSyncJob, listPRs, refreshPRUsage, syncPRs } from '@/api/pr'
```

Add state near PR list state:

```ts
const prsLoading = ref(false)
const prsLoadError = ref('')
```

Replace the initial `listPRs(...).catch(() => ({ data: { data: { items: [] } } }))` pattern with explicit calls:

```ts
onMounted(async () => {
  try {
    await Promise.all([
      refreshRepo(),
      loadPRs(),
      recoverLatestSyncJob(),
    ])
    if (auth.isAdmin) {
      const providersRes = await listProviders().catch(() => ({ data: { data: [] } }))
      const providerData = providersRes.data.data
      providers.value = Array.isArray(providerData) ? providerData : (providerData as any)?.items ?? []
    }
  } catch {
    router.push('/repos')
  } finally {
    loading.value = false
  }
})
```

Update `loadPRs`:

```ts
async function loadPRs() {
  prsLoading.value = true
  prsLoadError.value = ''
  try {
    const prsRes = await listPRs(repoId, { limit: prsPageSize, offset: prsPage.value * prsPageSize, months: prsMonths.value })
    const prData = prsRes.data.data
    prs.value = prData && 'items' in prData ? prData.items : []
    prsTotal.value = prData && 'total' in prData ? prData.total : 0
    prsSummary.value = prData && 'summary' in prData && prData.summary ? prData.summary : null
  } catch (error: any) {
    prsLoadError.value = error?.response?.data?.message || error?.message || t('repoDetail.prListLoadFailed')
  } finally {
    prsLoading.value = false
  }
}
```

Add recovery helper:

```ts
async function recoverLatestSyncJob() {
  try {
    const res = await getLatestPRSyncJob(repoId)
    const job = res.data.data ?? null
    if (!job) return
    syncJob.value = job
    if (!isTerminalJob(job)) {
      syncing.value = true
      syncPollTimer.value = window.setTimeout(() => pollSyncJob(job.id), 1500)
    }
  } catch (error: any) {
    syncMessageTone.value = 'error'
    syncMessage.value = error?.response?.data?.message || t('repoDetail.syncProgressFailed')
  }
}
```

Add template states near the PR list area:

```vue
<div v-if="prsLoadError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
  <div>{{ t('repoDetail.prListLoadFailed') }}</div>
  <button class="mt-2 rounded-md border border-red-200 px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-100" @click="loadPRs">
    {{ t('repoDetail.retry') }}
  </button>
</div>
```

Only show the empty PR state when `!prsLoadError && !prsLoading && prs.length === 0`.

Add i18n keys in `frontend/src/i18n.ts`:

```ts
'repoDetail.prListLoadFailed': 'Failed to load pull requests',
'repoDetail.retry': 'Retry',
```

and Chinese:

```ts
'repoDetail.prListLoadFailed': '加载 Pull Request 失败',
'repoDetail.retry': '重试',
```

- [ ] **Step 7: Run frontend targeted tests**

Run:

```bash
cd frontend && pnpm test -- api-modules.test.ts repo-detail-view.test.ts
```

Expected: PASS.

- [ ] **Step 8: Commit frontend recovery**

Run:

```bash
git add frontend/src/api/pr.ts frontend/src/views/repos/RepoDetailView.vue frontend/src/i18n.ts frontend/src/__tests__/api-modules.test.ts frontend/src/__tests__/repo-detail-view.test.ts
git commit -m "fix(frontend): recover PR sync job progress"
```

---

### Task 5: Documentation And Final Verification

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-06-03-pr-sync-large-repo-recovery-design.md`

- [ ] **Step 1: Update architecture documentation**

In `docs/architecture.md`, update the PR sync paragraph that currently describes `POST /api/v1/repos/:id/sync-prs` and frontend polling.

Use this replacement paragraph:

```markdown
`POST /api/v1/repos/:id/sync-prs` creates or reuses a backend `pr_sync_jobs` record and the backend process performs PR metadata sync plus active PR usage refresh asynchronously. Repo detail pages recover the latest repo-level sync job through `GET /api/v1/repos/:id/pr-sync-job/latest`, then poll `GET /api/v1/pr-sync-jobs/:id` while the job is active. `StartSyncJob` abandons stale queued/running jobs that have not recorded progress for more than one hour, which prevents a lost in-process worker from permanently blocking a new sync attempt. PR list summaries use bounded aggregate queries, while only the current page rows receive PR-level freshness evaluation. Bitbucket Server PR sync records SCM `createdDate` so recent-window filters are based on actual PR age rather than first ingestion time.
```

- [ ] **Step 2: Update spec status**

In `docs/superpowers/specs/2026-06-03-pr-sync-large-repo-recovery-design.md`, change:

```markdown
- **Status:** Approved design, implementation not started
```

to:

```markdown
- **Status:** Implemented
```

Append under `## Documentation Updates`:

```markdown
Implementation updated `docs/architecture.md` to reflect the current runtime contract.
```

- [ ] **Step 3: Run backend targeted tests**

Run:

```bash
cd backend && go test ./internal/scm/bitbucket ./internal/prsync ./internal/handler
```

Expected: PASS.

- [ ] **Step 4: Run frontend tests**

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS.

- [ ] **Step 5: Check working tree and generated files**

Run:

```bash
git status --short
```

Expected: only intended source, test, and docs files are modified. No generated debug scripts, secrets, or temporary files should be present.

- [ ] **Step 6: Commit documentation and verification state**

Run:

```bash
git add docs/architecture.md docs/superpowers/specs/2026-06-03-pr-sync-large-repo-recovery-design.md
git commit -m "docs(architecture): update PR sync recovery contract"
```

- [ ] **Step 7: Final integration check**

Run:

```bash
git log --oneline -5
git status --short
```

Expected: recent commits include the four implementation commits and docs commit. Working tree is clean, unless unrelated user changes existed before implementation.
