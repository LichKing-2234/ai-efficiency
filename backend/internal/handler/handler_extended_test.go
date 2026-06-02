package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
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
