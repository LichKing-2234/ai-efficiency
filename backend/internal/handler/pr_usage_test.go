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

type spyPRUsageService struct {
	refreshFn func(context.Context, scm.SCMProvider, *ent.PrRecord) (*prusage.Result, error)
	singleFn  func(context.Context, int) (*prusage.PRFreshness, error)
	pageFn    func(context.Context, int, []*ent.PrRecord) (map[int]*prusage.PRFreshness, error)

	refreshCalls     int
	singleCalls      []int
	pageCalls        int
	pageRepoConfigID []int
	pagePRIDs        [][]int
	pageContexts     []context.Context
}

type singleOnlyPRUsageService struct {
	singleCalls []int
	freshness   *prusage.PRFreshness
}

func (s *singleOnlyPRUsageService) RefreshPR(context.Context, scm.SCMProvider, *ent.PrRecord) (*prusage.Result, error) {
	return nil, fmt.Errorf("unexpected PR usage refresh")
}

func (s *singleOnlyPRUsageService) EvaluatePRFreshness(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
	s.singleCalls = append(s.singleCalls, prID)
	return s.freshness, nil
}

func (s *spyPRUsageService) RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error) {
	s.refreshCalls++
	if s.refreshFn == nil {
		return nil, fmt.Errorf("unexpected PR usage refresh")
	}
	return s.refreshFn(ctx, provider, pr)
}

func (s *spyPRUsageService) EvaluatePRFreshness(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
	s.singleCalls = append(s.singleCalls, prID)
	if s.singleFn == nil {
		return nil, fmt.Errorf("unexpected single PR freshness evaluation")
	}
	return s.singleFn(ctx, prID)
}

func (s *spyPRUsageService) EvaluatePRFreshnessPage(ctx context.Context, repoConfigID int, prs []*ent.PrRecord) (map[int]*prusage.PRFreshness, error) {
	s.pageCalls++
	s.pageRepoConfigID = append(s.pageRepoConfigID, repoConfigID)
	prIDs := make([]int, 0, len(prs))
	for _, pr := range prs {
		prIDs = append(prIDs, pr.ID)
	}
	s.pagePRIDs = append(s.pagePRIDs, prIDs)
	s.pageContexts = append(s.pageContexts, ctx)
	if s.pageFn == nil {
		return nil, fmt.Errorf("unexpected PR page freshness evaluation")
	}
	return s.pageFn(ctx, repoConfigID, prs)
}

func completePRFreshness(checkedAt time.Time) *prusage.PRFreshness {
	return &prusage.PRFreshness{
		Status:    prusage.UsageStatusStaleSnapshot,
		Reason:    "Usage events newer than the PR snapshot are bound to this checkpoint.",
		CheckedAt: checkedAt,
		Commits: []prusage.CommitFreshness{
			{
				CommitSHA:       "fresh-commit",
				Status:          prusage.UsageStatusFresh,
				Reason:          "Usage events were included.",
				CheckpointFound: true,
				UsageEventFound: true,
			},
			{
				CommitSHA:       "missing-checkpoint-commit",
				Status:          prusage.UsageStatusNoCheckpoint,
				Reason:          "No checkpoint matched this PR commit.",
				CheckpointFound: false,
				UsageEventFound: false,
			},
		},
	}
}

func assertCompleteCommitFreshness(t *testing.T, data map[string]interface{}, checkedAt time.Time) {
	t.Helper()
	if got := data["usage_status"]; got != string(prusage.UsageStatusStaleSnapshot) {
		t.Fatalf("usage_status = %v, want %q", got, prusage.UsageStatusStaleSnapshot)
	}
	if got := data["usage_status_reason"]; got != "Usage events newer than the PR snapshot are bound to this checkpoint." {
		t.Fatalf("usage_status_reason = %v", got)
	}
	if got := data["usage_status_checked_at"]; got != checkedAt.Format(time.RFC3339Nano) {
		t.Fatalf("usage_status_checked_at = %v, want %s", got, checkedAt.Format(time.RFC3339Nano))
	}
	commits, ok := data["commit_freshness"].([]interface{})
	if !ok {
		t.Fatalf("commit_freshness missing or wrong type: %T", data["commit_freshness"])
	}
	if len(commits) != 2 {
		t.Fatalf("commit_freshness = %d rows, want 2", len(commits))
	}
	want := []map[string]interface{}{
		{
			"commit_sha":          "fresh-commit",
			"usage_status":        string(prusage.UsageStatusFresh),
			"usage_status_reason": "Usage events were included.",
			"checkpoint_found":    true,
			"usage_event_found":   true,
		},
		{
			"commit_sha":          "missing-checkpoint-commit",
			"usage_status":        string(prusage.UsageStatusNoCheckpoint),
			"usage_status_reason": "No checkpoint matched this PR commit.",
			"checkpoint_found":    false,
			"usage_event_found":   false,
		},
	}
	for i, raw := range commits {
		got := raw.(map[string]interface{})
		for key, wantValue := range want[i] {
			if got[key] != wantValue {
				t.Fatalf("commit_freshness[%d].%s = %v, want %v", i, key, got[key], wantValue)
			}
		}
	}
}

func attachRefreshUsageRoute(t *testing.T, env *mockTestEnv, repoSCM repoSCMProvider, usageSvc prUsageRefresher) {
	t.Helper()
	prHandler := NewPRHandler(env.client, repoSCM, nil, nil, usageSvc)
	api := env.router.Group("/api/v1")
	api.Use(auth.RequireAuth(env.authSvc))
	api.POST("/prs/:id/refresh-usage", prHandler.RefreshUsage)
}

func TestPRHandlerRefreshUsageReturnsFreshnessAndUpdatedPR(t *testing.T) {
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

	checkedAt := time.Date(2026, time.July, 15, 3, 4, 5, 0, time.UTC)
	usageSvc := &spyPRUsageService{
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
		singleFn: func(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
			return completePRFreshness(checkedAt), nil
		},
		pageFn: func(ctx context.Context, repoConfigID int, prs []*ent.PrRecord) (map[int]*prusage.PRFreshness, error) {
			return nil, fmt.Errorf("page evaluator must not run for refresh")
		},
	}
	attachRefreshUsageRoute(t, env, repoSCM, usageSvc)

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
	if usageSvc.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", usageSvc.refreshCalls)
	}
	if len(usageSvc.singleCalls) != 1 || usageSvc.singleCalls[0] != pr.ID {
		t.Fatalf("single freshness calls = %v, want [%d]", usageSvc.singleCalls, pr.ID)
	}
	if usageSvc.pageCalls != 0 {
		t.Fatalf("page freshness calls = %d, want 0", usageSvc.pageCalls)
	}
	assertCompleteCommitFreshness(t, data, checkedAt)
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

	checkedAt := time.Date(2026, time.July, 15, 4, 5, 6, 0, time.UTC)
	usageSvc := &spyPRUsageService{
		singleFn: func(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
			return completePRFreshness(checkedAt), nil
		},
		pageFn: func(ctx context.Context, repoConfigID int, prs []*ent.PrRecord) (map[int]*prusage.PRFreshness, error) {
			return nil, fmt.Errorf("page evaluator must not run for detail")
		},
	}
	prHandler := NewPRHandler(env.client, repoSCM, nil, nil, usageSvc)
	api := env.router.Group("/api/v1/freshness-test")
	api.Use(auth.RequireAuth(env.authSvc))
	api.GET("/prs/:id", prHandler.Get)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/freshness-test/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if len(usageSvc.singleCalls) != 1 || usageSvc.singleCalls[0] != pr.ID {
		t.Fatalf("single freshness calls = %v, want [%d]", usageSvc.singleCalls, pr.ID)
	}
	if usageSvc.pageCalls != 0 {
		t.Fatalf("page freshness calls = %d, want 0", usageSvc.pageCalls)
	}
	assertCompleteCommitFreshness(t, data, checkedAt)
}
