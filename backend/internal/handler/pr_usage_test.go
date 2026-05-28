package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/scm"
)

type mockPRUsageRefresher struct {
	refreshFn func(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error)
}

func (m *mockPRUsageRefresher) RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error) {
	return m.refreshFn(ctx, provider, pr)
}

func attachRefreshUsageRoute(t *testing.T, env *mockTestEnv, repoSCM repoSCMProvider, usageSvc prUsageRefresher) {
	t.Helper()
	prHandler := NewPRHandler(env.client, repoSCM, nil, nil, usageSvc)
	api := env.router.Group("/api/v1")
	api.Use(auth.RequireAuth(env.authSvc))
	api.POST("/prs/:id/refresh-usage", prHandler.RefreshUsage)
}

func TestPRHandlerRefreshUsage_ReturnsUpdatedPR(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: id, FullName: "org/mock-repo"}, nil
		},
	}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(101).
		SetTitle("usage refresh").
		SetStatus("open").
		SaveX(context.Background())

	attachRefreshUsageRoute(t, env, repoSCM, &mockPRUsageRefresher{
		refreshFn: func(ctx context.Context, provider scm.SCMProvider, current *ent.PrRecord) (*prusage.Result, error) {
			refreshedAt := time.Now().UTC()
			env.client.PrRecord.UpdateOneID(current.ID).
				SetUsageInputTokens(120).
				SetUsageOutputTokens(45).
				SetUsageCommitCount(2).
				SetUsageRefreshedAt(refreshedAt).
				ExecX(ctx)
			env.client.PRCommitUsageSnapshot.Create().
				SetPrRecordID(current.ID).
				SetCommitSha("abc123").
				SetInputTokens(120).
				SetOutputTokens(45).
				SetSortOrder(0).
				SaveX(ctx)
			return &prusage.Result{
				PRRecordID: current.ID,
				Summary: prusage.Summary{
					InputTokens:  120,
					OutputTokens: 45,
					CommitCount:  2,
				},
				RefreshedAt: refreshedAt,
			}, nil
		},
	})

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/prs/%d/refresh-usage", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if got := data["usage_commit_count"]; got != float64(2) {
		t.Fatalf("usage_commit_count = %v, want 2", got)
	}
	edges := data["edges"].(map[string]interface{})
	commits := edges["pr_commit_usage_snapshots"].([]interface{})
	if len(commits) != 1 {
		t.Fatalf("commit snapshots = %d, want 1", len(commits))
	}
}

func TestPRHandlerGetIncludesUsageFreshness(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, nil, nil, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)
	pr := env.client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(202).
		SetTitle("freshness").
		SetStatus("open").
		SaveX(context.Background())

	prHandler := NewPRHandler(env.client, repoSCM, nil, nil, prusage.NewService(env.client))
	api := env.router.Group("/api/v1/freshness-test")
	api.Use(auth.RequireAuth(env.authSvc))
	api.GET("/prs/:id", prHandler.Get)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/freshness-test/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["usage_status"] != "no_checkpoint" {
		t.Fatalf("usage_status = %v, want no_checkpoint", data["usage_status"])
	}
}
