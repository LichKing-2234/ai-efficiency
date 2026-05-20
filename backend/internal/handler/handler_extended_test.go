package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

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

// =====================
// Efficiency handler tests
// =====================

func TestAggregateAdminSuccess(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/efficiency/aggregate", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "ok" {
		t.Errorf("status = %v, want ok", data["status"])
	}
}

func TestAggregateSingleRepo(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/efficiency/aggregate?repo_id=%d", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "ok" {
		t.Errorf("status = %v, want ok", data["status"])
	}
	if int(data["repo_id"].(float64)) != repoID {
		t.Errorf("repo_id = %v, want %d", data["repo_id"], repoID)
	}
}

func TestAggregateInvalidRepoID(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/efficiency/aggregate?repo_id=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
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
