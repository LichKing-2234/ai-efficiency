package prsync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func TestStartSyncJobCreatesQueuedJob(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-create-repo")
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if reused {
		t.Fatalf("reused = true, want false")
	}
	if job.RepoConfigID != rc.ID || job.Status != prsyncjob.StatusQueued || job.Phase != prsyncjob.PhaseQueued {
		t.Fatalf("job = %+v, want queued job for repo %d", job, rc.ID)
	}
}

func TestStartSyncJobReusesRunningJobForRepo(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-reuse-repo")
	existing := client.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.PhaseFetchingPrs).
		SaveX(ctx)
	svc := NewService(client, nil, zap.NewNop())

	job, reused, err := svc.StartSyncJob(ctx, &mockSCMProvider{}, rc)
	if err != nil {
		t.Fatalf("StartSyncJob error: %v", err)
	}
	if !reused {
		t.Fatalf("reused = false, want true")
	}
	if job.ID != existing.ID {
		t.Fatalf("job id = %d, want existing %d", job.ID, existing.ID)
	}
}

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

func TestUpsertPRDistinguishesChangedAndUnchanged(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "upsert-state-repo")
	svc := NewService(client, nil, zap.NewNop())

	first := &scm.PR{ID: 7, Title: "Initial", Author: "alice", SourceBranch: "feat", TargetBranch: "main", State: "open", URL: "https://example.com/pr/7"}
	_, state, err := svc.upsertPR(ctx, rc.ID, first)
	if err != nil {
		t.Fatalf("first upsert error: %v", err)
	}
	if state != UpsertCreated {
		t.Fatalf("first state = %s, want %s", state, UpsertCreated)
	}

	_, state, err = svc.upsertPR(ctx, rc.ID, first)
	if err != nil {
		t.Fatalf("second upsert error: %v", err)
	}
	if state != UpsertUnchanged {
		t.Fatalf("second state = %s, want %s", state, UpsertUnchanged)
	}

	changed := *first
	changed.Title = "Changed"
	_, state, err = svc.upsertPR(ctx, rc.ID, &changed)
	if err != nil {
		t.Fatalf("third upsert error: %v", err)
	}
	if state != UpsertChanged {
		t.Fatalf("third state = %s, want %s", state, UpsertChanged)
	}
}

func TestUpsertPRIgnoresDatabaseTimestampPrecision(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "upsert-timestamp-precision-repo")
	svc := NewService(client, nil, zap.NewNop())

	incomingCreatedAt := time.Date(2026, 5, 28, 9, 0, 0, 123456789, time.UTC)
	storedCreatedAt := incomingCreatedAt.Round(time.Microsecond)
	client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(77).
		SetScmPrURL("https://example.com/pr/77").
		SetTitle("old closed").
		SetAuthor("legacy").
		SetSourceBranch("legacy").
		SetTargetBranch("main").
		SetStatus("closed").
		SetCreatedAt(storedCreatedAt).
		SaveX(ctx)

	pr := &scm.PR{
		ID:           77,
		Title:        "old closed",
		Author:       "legacy",
		SourceBranch: "legacy",
		TargetBranch: "main",
		State:        "closed",
		URL:          "https://example.com/pr/77",
		CreatedAt:    incomingCreatedAt,
	}
	_, state, err := svc.upsertPR(ctx, rc.ID, pr)
	if err != nil {
		t.Fatalf("upsertPR error: %v", err)
	}
	if state != UpsertUnchanged {
		t.Fatalf("state = %s, want %s", state, UpsertUnchanged)
	}
}

func TestRunSyncJobUpdatesProgressAndCompletes(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()
	ctx := context.Background()
	rc := createTestRepo(t, ctx, client, "job-progress-repo")
	svc := NewService(client, nil, zap.NewNop())
	job := client.PRSyncJob.Create().SetRepoConfigID(rc.ID).SaveX(ctx)

	callCount := 0
	mock := &mockSCMProvider{
		listPRsFunc: func(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
			callCount++
			if callCount == 1 {
				items := make([]*scm.PR, 100)
				for i := range items {
					items[i] = &scm.PR{ID: i + 1, Title: "PR", Author: "alice", SourceBranch: "feat", TargetBranch: "main", State: "open"}
				}
				return items, nil
			}
			return []*scm.PR{{ID: 101, Title: "Last PR", Author: "bob", SourceBranch: "feat", TargetBranch: "main", State: "open"}}, nil
		},
	}

	result, err := svc.RunSyncJob(ctx, job.ID, mock, rc)
	if err != nil {
		t.Fatalf("RunSyncJob error: %v", err)
	}
	if result.Total != 101 {
		t.Fatalf("total = %d, want 101", result.Total)
	}
	loaded := client.PRSyncJob.GetX(ctx, job.ID)
	if loaded.Status != prsyncjob.StatusCompleted || loaded.Phase != prsyncjob.PhaseCompleted {
		t.Fatalf("job status=%s phase=%s, want completed/completed", loaded.Status, loaded.Phase)
	}
	if loaded.FetchedPrs != 101 || loaded.ProcessedPrs != 101 {
		t.Fatalf("progress fetched=%d processed=%d, want 101/101", loaded.FetchedPrs, loaded.ProcessedPrs)
	}
}
