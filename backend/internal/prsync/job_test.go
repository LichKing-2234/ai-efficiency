package prsync

import (
	"context"
	"testing"

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
