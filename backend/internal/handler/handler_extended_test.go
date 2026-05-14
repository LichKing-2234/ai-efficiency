package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/google/uuid"
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

func TestSessionUpdate_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "PUT", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440099", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestSessionUpdate_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "PUT", "/api/v1/sessions/bad-id", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSessionAddInvocation_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/sessions/bad-id/invocations", map[string]interface{}{
		"tool":  "claude-code",
		"start": "2026-03-17T10:00:00Z",
		"end":   "2026-03-17T10:05:00Z",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSessionAddInvocation_InvalidBody(t *testing.T) {
	env := setupTestEnv(t)
	// Valid UUID but missing required fields (tool, start)
	w := doRequest(env, "POST", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440010/invocations", map[string]interface{}{
		"bad_field": "value",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSessionAddInvocation_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440099/invocations", map[string]interface{}{
		"tool":  "claude-code",
		"start": "2026-03-17T10:00:00Z",
		"end":   "2026-03-17T10:05:00Z",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
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

func TestRepoMetrics_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/efficiency/repos/abc/metrics", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestTrend_InvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/efficiency/repos/abc/trend", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestDashboardWithData(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()
	repoID := createTestRepo(t, env.client)

	// Create a session so active_sessions > 0
	sessionID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440030")
	_, err := env.client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repoID).
		SetBranch("main").
		SetStartedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create session: %v", err)
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

	activeSessions := int(data["active_sessions"].(float64))
	if activeSessions < 1 {
		t.Errorf("active_sessions = %d, want >= 1", activeSessions)
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

	_, err := env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440100")).
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetBranch("main").
		SetStartedAt(time.Now()).
		SetStatus("active").
		Save(ctx)
	if err != nil {
		t.Fatalf("create active session: %v", err)
	}

	_, err = env.client.Session.Create().
		SetID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440101")).
		SetRepoConfigID(repoID).
		SetUserID(env.userID).
		SetBranch("release").
		SetStartedAt(time.Now()).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		t.Fatalf("create completed session: %v", err)
	}

	w := doRequest(env, "GET", "/api/v1/efficiency/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	activeSessions := int(data["active_sessions"].(float64))
	if activeSessions != 1 {
		t.Fatalf("active_sessions = %d, want %d", activeSessions, 1)
	}
}
