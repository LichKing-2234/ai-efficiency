package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/prusage"
)

// =====================
// Repo handler tests
// =====================

func TestRepoCreateDirect_InvalidBody(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/repos/direct", map[string]interface{}{"bad": true})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestRepoGet_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/repos/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestRepoGet_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/repos/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestRepoUpdate(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	updateBody := map[string]interface{}{
		"status":   "active",
		"group_id": "team-alpha",
		"name":     "updated-repo",
	}
	w := doRequest(env, "PUT", fmt.Sprintf("/api/v1/repos/%d", repoID), updateBody)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["group_id"] != "team-alpha" {
		t.Errorf("group_id = %v, want team-alpha", data["group_id"])
	}
}

func TestRepoUpdate_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "PUT", "/api/v1/repos/abc", map[string]interface{}{"status": "active"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestRepoUpdate_InvalidSCMProviderID(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "PUT", fmt.Sprintf("/api/v1/repos/%d", repoID), map[string]interface{}{
		"scm_provider_id": 99999,
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestRepoDelete_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "DELETE", "/api/v1/repos/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestRepoDelete_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "DELETE", "/api/v1/repos/99999", nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestDashboardWithData(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	repoID := createTestRepo(t, env.client)

	err := env.client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-dashboard-1").
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetToolSessionID("tool-sess-1").
		SetDedupeKey("dashboard:tool:1").
		SetUsageUnit("token").
		SetObservedStartAt(time.Now().Add(-time.Minute)).
		SetObservedEndAt(time.Now()).
		Exec(ctx)
	if err != nil {
		t.Fatalf("create tool usage event: %v", err)
	}

	// Create an AI PR so total_ai_prs > 0
	_, err = env.client.PrRecord.Create().
		SetScmPrID(100).
		SetTitle("AI-generated PR").
		SetAuthor("bot").
		SetSourceBranch("ai-fix").
		SetTargetBranch("main").
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetRepoConfigID(repoID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create PR record: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/efficiency/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})

	totalRepos := int(data["total_repos"].(float64))
	if totalRepos < 1 {
		t.Errorf("total_repos = %d, want >= 1", totalRepos)
	}

	trackedWorkflows := int(data["tracked_workflows"].(float64))
	if trackedWorkflows < 1 {
		t.Errorf("tracked_workflows = %d, want >= 1", trackedWorkflows)
	}

	totalAIPRs := int(data["total_ai_prs"].(float64))
	if totalAIPRs < 1 {
		t.Errorf("total_ai_prs = %d, want >= 1", totalAIPRs)
	}
}

func TestPRListByRepoIncludesAggregateUsageSummary(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	repoID := createTestRepo(t, env.client)
	now := time.Now().UTC()

	checkpoint := env.client.CommitCheckpoint.Create().
		SetEventID("summary-cp-1").
		SetWorkspaceID("ws-summary").
		SetRepoConfigID(repoID).
		SetCommitSha("summary-sha-1").
		SetParentShas([]string{}).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SaveX(ctx)

	prWithUsage := env.client.PrRecord.Create().
		SetRepoConfigID(repoID).
		SetScmPrID(201).
		SetTitle("with usage").
		SetAuthor("alice").
		SetStatus(prrecord.StatusMerged).
		SetCreatedAt(now.Add(-24 * time.Hour)).
		SetUsageInputTokens(100).
		SaveX(ctx)
	env.client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(prWithUsage.ID).
		SetCommitSha("summary-sha-1").
		SetCommitCheckpointID(checkpoint.ID).
		SetInputTokens(100).
		SetSortOrder(0).
		SaveX(ctx)
	env.client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-summary").
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetToolSessionID("summary-session").
		SetDedupeKey("summary:usage:1").
		SetUsageUnit("token").
		SetInputTokens(100).
		SetObservedStartAt(now.Add(-2 * time.Hour)).
		SetObservedEndAt(now.Add(-time.Hour)).
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	for i := 0; i < 2; i++ {
		env.client.PrRecord.Create().
			SetRepoConfigID(repoID).
			SetScmPrID(202 + i).
			SetTitle(fmt.Sprintf("missing checkpoint %d", i)).
			SetAuthor("bob").
			SetStatus(prrecord.StatusMerged).
			SetCreatedAt(now.Add(time.Duration(-i) * time.Hour)).
			SaveX(ctx)
	}

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?limit=1&months=3", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if total := int(data["total"].(float64)); total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want paginated 1", len(items))
	}
	summary, ok := data["summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("summary missing or wrong type: %T", data["summary"])
	}
	if got := int(summary["total"].(float64)); got != 3 {
		t.Fatalf("summary.total = %d, want 3", got)
	}
	if got := int(summary["with_usage"].(float64)); got != 1 {
		t.Fatalf("summary.with_usage = %d, want 1", got)
	}
	if got := int(summary["no_checkpoint"].(float64)); got != 2 {
		t.Fatalf("summary.no_checkpoint = %d, want 2", got)
	}
	if got := int(summary["pending_upload"].(float64)); got != 0 {
		t.Fatalf("summary.pending_upload = %d, want 0", got)
	}
	if got := int(summary["refresh_failed"].(float64)); got != 0 {
		t.Fatalf("summary.refresh_failed = %d, want 0", got)
	}
}

func TestPRListByRepoBatchesFreshnessForCurrentPage(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	repoID := createTestRepo(t, env.client)
	now := time.Date(2026, time.July, 15, 6, 0, 0, 0, time.UTC)
	type prSpec struct {
		title        string
		status       prrecord.Status
		createdAt    time.Time
		inputTokens  int64
		outputTokens int64
		requests     int
	}
	specs := []prSpec{
		{title: "closed newest", status: prrecord.StatusClosed, createdAt: now},
		{title: "merged older", status: prrecord.StatusMerged, createdAt: now.Add(-6 * time.Hour)},
		{title: "open older", status: prrecord.StatusOpen, createdAt: now.Add(-5 * time.Hour)},
		{title: "merged newer", status: prrecord.StatusMerged, createdAt: now.Add(-2 * time.Hour)},
		{title: "open newer", status: prrecord.StatusOpen, createdAt: now.Add(-time.Hour), inputTokens: 505, outputTokens: 55, requests: 5},
	}
	specByTitle := make(map[string]prSpec, len(specs))
	idByTitle := make(map[string]int, len(specs))
	for i, spec := range specs {
		pr := env.client.PrRecord.Create().
			SetRepoConfigID(repoID).
			SetScmPrID(9000 + i).
			SetTitle(spec.title).
			SetAuthor("alice").
			SetStatus(spec.status).
			SetCreatedAt(spec.createdAt).
			SetUsageInputTokens(spec.inputTokens).
			SetUsageOutputTokens(spec.outputTokens).
			SetUsageRequestCount(spec.requests).
			SaveX(ctx)
		specByTitle[spec.title] = spec
		idByTitle[spec.title] = pr.ID
	}
	for i := 0; i < 20; i++ {
		env.client.PrRecord.Create().
			SetRepoConfigID(repoID).
			SetScmPrID(10000 + i).
			SetTitle(fmt.Sprintf("older closed PR %d", i)).
			SetAuthor("bob").
			SetStatus(prrecord.StatusClosed).
			SetCreatedAt(now.Add(-time.Duration(24+i) * time.Hour)).
			SaveX(ctx)
	}

	wantTitles := []string{"open newer", "open older", "merged newer", "merged older", "closed newest"}
	wantIDs := make([]int, 0, len(wantTitles))
	freshnessByPR := make(map[int]*prusage.PRFreshness, len(wantTitles))
	freshnessStatuses := []prusage.UsageStatus{
		prusage.UsageStatusFresh,
		prusage.UsageStatusPendingUpload,
		prusage.UsageStatusNoCheckpoint,
		prusage.UsageStatusNoUsageEvents,
		prusage.UsageStatusStaleSnapshot,
	}
	for i, title := range wantTitles {
		prID := idByTitle[title]
		wantIDs = append(wantIDs, prID)
		freshnessByPR[prID] = &prusage.PRFreshness{
			Status:    freshnessStatuses[i],
			Reason:    fmt.Sprintf("page freshness %d", i),
			CheckedAt: now.Add(time.Duration(i) * time.Minute),
			Commits: []prusage.CommitFreshness{
				{
					CommitSHA:       fmt.Sprintf("commit-%d", i),
					Status:          prusage.UsageStatusFresh,
					Reason:          "Usage events were included.",
					CheckpointFound: true,
					UsageEventFound: true,
				},
			},
		}
	}

	usageSvc := &spyPRUsageService{
		singleFn: func(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
			return completePRFreshness(now), nil
		},
		pageFn: func(ctx context.Context, repoConfigID int, prs []*ent.PrRecord) (map[int]*prusage.PRFreshness, error) {
			return freshnessByPR, nil
		},
	}
	prHandler := NewPRHandler(env.client, nil, nil, nil, usageSvc)
	group := env.router.Group("/api/v1/test-bounded-summary")
	group.GET("/repos/:id/prs", prHandler.ListByRepo)

	contextKey := struct{ name string }{name: "pr-list-request"}
	requestCtx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey, "request-bound"))
	defer cancel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/test-bounded-summary/repos/%d/prs?limit=5&months=0", repoID),
		nil,
	).WithContext(requestCtx)
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if usageSvc.pageCalls != 1 {
		t.Fatalf("page freshness calls = %d, want 1", usageSvc.pageCalls)
	}
	if !slices.Equal(usageSvc.pageRepoConfigID, []int{repoID}) {
		t.Fatalf("page repo_config_id calls = %v, want [%d]", usageSvc.pageRepoConfigID, repoID)
	}
	if len(usageSvc.pagePRIDs) != 1 || !slices.Equal(usageSvc.pagePRIDs[0], wantIDs) {
		t.Fatalf("page PR IDs = %v, want %v", usageSvc.pagePRIDs, wantIDs)
	}
	if len(usageSvc.singleCalls) != 0 {
		t.Fatalf("single freshness calls = %v, want none", usageSvc.singleCalls)
	}
	if len(usageSvc.pageContexts) != 1 || usageSvc.pageContexts[0].Value(contextKey) != "request-bound" {
		t.Fatalf("page evaluator did not receive the request context")
	}
	if usageSvc.pageContexts[0].Done() != requestCtx.Done() {
		t.Fatalf("page evaluator context is not bound to request cancellation")
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != len(wantTitles) {
		t.Fatalf("items = %d, want %d", len(items), len(wantTitles))
	}
	jsonInt64 := func(value interface{}) int64 {
		if value == nil {
			return 0
		}
		return int64(value.(float64))
	}
	for i, raw := range items {
		item := raw.(map[string]interface{})
		wantSpec := specByTitle[wantTitles[i]]
		wantFreshness := freshnessByPR[wantIDs[i]]
		if got := int(item["id"].(float64)); got != wantIDs[i] {
			t.Fatalf("items[%d].id = %d, want %d", i, got, wantIDs[i])
		}
		if got := item["title"]; got != wantSpec.title {
			t.Fatalf("items[%d].title = %v, want %q", i, got, wantSpec.title)
		}
		if got := item["status"]; got != string(wantSpec.status) {
			t.Fatalf("items[%d].status = %v, want %q", i, got, wantSpec.status)
		}
		if got := jsonInt64(item["usage_input_tokens"]); got != wantSpec.inputTokens {
			t.Fatalf("items[%d].usage_input_tokens = %d, want %d", i, got, wantSpec.inputTokens)
		}
		if got := jsonInt64(item["usage_output_tokens"]); got != wantSpec.outputTokens {
			t.Fatalf("items[%d].usage_output_tokens = %d, want %d", i, got, wantSpec.outputTokens)
		}
		if got := int(jsonInt64(item["usage_request_count"])); got != wantSpec.requests {
			t.Fatalf("items[%d].usage_request_count = %d, want %d", i, got, wantSpec.requests)
		}
		if got := item["usage_status"]; got != string(wantFreshness.Status) {
			t.Fatalf("items[%d].usage_status = %v, want %q", i, got, wantFreshness.Status)
		}
		if got := item["usage_status_reason"]; got != wantFreshness.Reason {
			t.Fatalf("items[%d].usage_status_reason = %v, want %q", i, got, wantFreshness.Reason)
		}
		if got := item["usage_status_checked_at"]; got != wantFreshness.CheckedAt.Format(time.RFC3339Nano) {
			t.Fatalf("items[%d].usage_status_checked_at = %v, want %s", i, got, wantFreshness.CheckedAt.Format(time.RFC3339Nano))
		}
		if _, ok := item["commit_freshness"]; ok {
			t.Fatalf("items[%d].commit_freshness must be absent from list rows", i)
		}
	}
	if got := int(data["total"].(float64)); got != 25 {
		t.Fatalf("total = %d, want 25", got)
	}
	summary := data["summary"].(map[string]interface{})
	if got := int(summary["total"].(float64)); got != 25 {
		t.Fatalf("summary.total = %d, want 25", got)
	}
	if got := int(summary["with_usage"].(float64)); got != 1 {
		t.Fatalf("summary.with_usage = %d, want 1", got)
	}
	if got := int(summary["no_checkpoint"].(float64)); got != 24 {
		t.Fatalf("summary.no_checkpoint = %d, want 24", got)
	}
	if got := int(summary["pending_upload"].(float64)); got != 0 {
		t.Fatalf("summary.pending_upload = %d, want 0", got)
	}
	if got := int(summary["refresh_failed"].(float64)); got != 0 {
		t.Fatalf("summary.refresh_failed = %d, want 0", got)
	}
}

func TestPRListByRepoUsesBoundedFreshnessFallbacks(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		wantPageCalls int
		wantProvided  []bool
	}{
		{name: "page success", mode: "success", wantPageCalls: 1, wantProvided: []bool{true, true}},
		{name: "page evaluator error", mode: "error", wantPageCalls: 1, wantProvided: []bool{false, false}},
		{name: "page result missing requested PR", mode: "missing", wantPageCalls: 1, wantProvided: []bool{true, false}},
		{name: "single evaluator without page capability", mode: "single-only", wantPageCalls: 0, wantProvided: []bool{false, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setupTestEnv(t)
			ctx := context.Background()
			repoID := createTestRepo(t, env.client)
			now := time.Date(2026, time.July, 15, 7, 0, 0, 0, time.UTC)
			first := env.client.PrRecord.Create().
				SetRepoConfigID(repoID).
				SetScmPrID(9101).
				SetTitle("first PR").
				SetAuthor("alice").
				SetStatus(prrecord.StatusOpen).
				SetCreatedAt(now).
				SaveX(ctx)
			second := env.client.PrRecord.Create().
				SetRepoConfigID(repoID).
				SetScmPrID(9102).
				SetTitle("second PR").
				SetAuthor("bob").
				SetStatus(prrecord.StatusMerged).
				SetCreatedAt(now.Add(-time.Hour)).
				SaveX(ctx)

			provided := &prusage.PRFreshness{
				Status:    prusage.UsageStatusFresh,
				Reason:    "Usage snapshot is current.",
				CheckedAt: now,
				Commits: []prusage.CommitFreshness{
					{CommitSHA: "list-only", Status: prusage.UsageStatusFresh, Reason: "Usage events were included.", CheckpointFound: true, UsageEventFound: true},
				},
			}

			var usageSvc prUsageRefresher
			var pageSpy *spyPRUsageService
			var singleOnly *singleOnlyPRUsageService
			if tt.mode == "single-only" {
				singleOnly = &singleOnlyPRUsageService{freshness: provided}
				usageSvc = singleOnly
			} else {
				pageSpy = &spyPRUsageService{
					singleFn: func(ctx context.Context, prID int) (*prusage.PRFreshness, error) {
						return provided, nil
					},
					pageFn: func(ctx context.Context, repoConfigID int, prs []*ent.PrRecord) (map[int]*prusage.PRFreshness, error) {
						switch tt.mode {
						case "success":
							return map[int]*prusage.PRFreshness{first.ID: provided, second.ID: provided}, nil
						case "missing":
							return map[int]*prusage.PRFreshness{first.ID: provided}, nil
						default:
							return nil, errors.New("page freshness failed")
						}
					},
				}
				usageSvc = pageSpy
			}

			prHandler := NewPRHandler(env.client, nil, nil, nil, usageSvc)
			group := env.router.Group("/api/v1/test-freshness-fallback")
			group.GET("/repos/:id/prs", prHandler.ListByRepo)
			w := doRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/test-freshness-fallback/repos/%d/prs?limit=2&months=0", repoID), nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
			}

			resp := parseResponse(t, w)
			items := resp["data"].(map[string]interface{})["items"].([]interface{})
			if len(items) != 2 {
				t.Fatalf("items = %d, want 2", len(items))
			}
			for i, raw := range items {
				item := raw.(map[string]interface{})
				if tt.wantProvided[i] {
					if got := item["usage_status"]; got != string(provided.Status) {
						t.Fatalf("items[%d].usage_status = %v, want %q", i, got, provided.Status)
					}
					if got := item["usage_status_reason"]; got != provided.Reason {
						t.Fatalf("items[%d].usage_status_reason = %v, want %q", i, got, provided.Reason)
					}
					if got := item["usage_status_checked_at"]; got != now.Format(time.RFC3339Nano) {
						t.Fatalf("items[%d].usage_status_checked_at = %v, want %s", i, got, now.Format(time.RFC3339Nano))
					}
				} else {
					if got := item["usage_status"]; got != string(prusage.UsageStatusUnknown) {
						t.Fatalf("items[%d].usage_status = %v, want %q", i, got, prusage.UsageStatusUnknown)
					}
					if got := item["usage_status_reason"]; got != "Usage freshness has not been evaluated." {
						t.Fatalf("items[%d].usage_status_reason = %v", i, got)
					}
					if _, ok := item["usage_status_checked_at"]; ok {
						t.Fatalf("items[%d].usage_status_checked_at must be absent for fallback", i)
					}
				}
				if _, ok := item["commit_freshness"]; ok {
					t.Fatalf("items[%d].commit_freshness must be absent from list rows", i)
				}
			}

			if pageSpy != nil {
				if pageSpy.pageCalls != tt.wantPageCalls {
					t.Fatalf("page freshness calls = %d, want %d", pageSpy.pageCalls, tt.wantPageCalls)
				}
				if len(pageSpy.singleCalls) != 0 {
					t.Fatalf("single freshness calls = %v, want none", pageSpy.singleCalls)
				}
			}
			if singleOnly != nil && len(singleOnly.singleCalls) != 0 {
				t.Fatalf("single freshness calls = %v, want none", singleOnly.singleCalls)
			}
		})
	}
}

func TestDashboardCountsOnlyActiveSessions(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	err := env.client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-dashboard-a").
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetToolSessionID("tool-a").
		SetDedupeKey("dashboard:tool:a").
		SetUsageUnit("token").
		SetObservedStartAt(time.Now().Add(-2 * time.Minute)).
		SetObservedEndAt(time.Now().Add(-time.Minute)).
		Exec(ctx)
	if err != nil {
		t.Fatalf("create first tool usage event: %v", err)
	}

	err = env.client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID("ws-dashboard-a").
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetToolSessionID("tool-b").
		SetDedupeKey("dashboard:tool:b").
		SetUsageUnit("token").
		SetObservedStartAt(time.Now().Add(-30 * time.Second)).
		SetObservedEndAt(time.Now()).
		Exec(ctx)
	if err != nil {
		t.Fatalf("create second tool usage event: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/efficiency/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	trackedWorkflows := int(data["tracked_workflows"].(float64))
	if trackedWorkflows != 1 {
		t.Fatalf("tracked_workflows = %d, want %d", trackedWorkflows, 1)
	}
}
