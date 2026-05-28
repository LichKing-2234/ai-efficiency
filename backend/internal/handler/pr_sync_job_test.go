package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/scm"
)

type mockPRSyncJobber struct {
	startFn func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error)
	runFn   func(ctx context.Context, jobID int, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	getFn   func(ctx context.Context, id int) (*ent.PRSyncJob, error)
}

func (m *mockPRSyncJobber) Sync(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error) {
	return nil, nil
}

func (m *mockPRSyncJobber) StartSyncJob(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
	return m.startFn(ctx, provider, rc)
}

func (m *mockPRSyncJobber) RunSyncJob(ctx context.Context, jobID int, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error) {
	if m.runFn == nil {
		return nil, nil
	}
	return m.runFn(ctx, jobID, provider, rc)
}

func (m *mockPRSyncJobber) GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error) {
	return m.getFn(ctx, id)
}

func attachPRSyncJobRoutes(t *testing.T, env *mockTestEnv, repoSCM repoSCMProvider, syncer prSyncer) {
	t.Helper()
	prHandler := NewPRHandler(env.client, repoSCM, syncer, nil)
	api := env.router.Group("/api/v1")
	api.Use(auth.RequireAuth(env.authSvc))
	api.POST("/repos/:id/sync-prs", prHandler.SyncPRs)
	api.GET("/pr-sync-jobs/:id", prHandler.GetSyncJob)
}

func TestSyncPRsStartsJobAndReturnsJobID(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: id, FullName: "org/repo"}, nil
		},
	}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
			return &ent.PRSyncJob{ID: 44, RepoConfigID: rc.ID, Status: prsyncjob.StatusQueued, Phase: prsyncjob.PhaseQueued}, false, nil
		},
		getFn: func(ctx context.Context, id int) (*ent.PRSyncJob, error) { return nil, nil },
	})

	w := doMockRequest(env, "POST", "/api/v1/repos/9/sync-prs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["job_id"] != float64(44) || data["status"] != "queued" {
		t.Fatalf("data = %+v, want job_id=44 status=queued", data)
	}
}

func TestGetPRSyncJobReturnsProgress(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	attachPRSyncJobRoutes(t, env, repoSCM, &mockPRSyncJobber{
		startFn: func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
			return nil, false, nil
		},
		getFn: func(ctx context.Context, id int) (*ent.PRSyncJob, error) {
			return &ent.PRSyncJob{ID: id, RepoConfigID: 9, Status: prsyncjob.StatusRunning, Phase: prsyncjob.PhaseRefreshingUsage, FetchedPrs: 120, ProcessedPrs: 100}, nil
		},
	})

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/pr-sync-jobs/%d", 44), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["phase"] != "refreshing_usage" || data["fetched_prs"] != float64(120) {
		t.Fatalf("data = %+v, want phase refreshing_usage fetched_prs 120", data)
	}
}
