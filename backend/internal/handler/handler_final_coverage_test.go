package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/aiscanresult"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// =====================
// 1. auth.go:68 Me — verify response body contains user info
// =====================

func TestAuthMeResponseBody(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/auth/me", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T: %v", resp["data"], resp)
	}
	if data["username"] != "admin" {
		t.Errorf("expected username 'admin', got %v", data["username"])
	}
	if data["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", data["role"])
	}
	userID, ok := data["user_id"].(float64)
	if !ok || userID == 0 {
		t.Errorf("expected non-zero user_id, got %v", data["user_id"])
	}
}

// =====================
// 2. chat.go:274 buildChatSystemPrompt — with non-nil scan result
// =====================

func TestBuildChatSystemPromptWithScanResult(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create a scan result with Score, Dimensions, and Suggestions
	_, err := env.client.AiScanResult.Create().
		SetRepoConfigID(repoID).
		SetScore(75).
		SetScanType(aiscanresult.ScanTypeStatic).
		SetDimensions(map[string]interface{}{
			"ci_cd":    85,
			"security": 60,
			"testing":  70,
		}).
		SetSuggestions([]map[string]interface{}{
			{"title": "Add SAST scanning", "severity": "high"},
			{"title": "Improve test coverage", "severity": "medium"},
		}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create scan result: %v", err)
	}

	// Load the scan result from DB
	scan, err := env.client.AiScanResult.Query().
		Where(aiscanresult.HasRepoConfigWith()).
		Order().
		First(ctx)
	if err != nil {
		t.Fatalf("query scan: %v", err)
	}

	rc, err := env.client.RepoConfig.Get(ctx, repoID)
	if err != nil {
		t.Fatalf("get repo config: %v", err)
	}

	ch := &ChatHandler{
		dataDir: t.TempDir(),
	}

	prompt := ch.buildChatSystemPrompt(rc, scan)

	if !strings.Contains(prompt, "Score: 75/100") {
		t.Errorf("prompt should contain 'Score: 75/100', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "ci_cd") {
		t.Errorf("prompt should contain dimension 'ci_cd', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Add SAST scanning") {
		t.Errorf("prompt should contain suggestion 'Add SAST scanning', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Latest Scan Results") {
		t.Errorf("prompt should contain 'Latest Scan Results', got:\n%s", prompt)
	}
}

// =====================
// 3. session.go:40 Create — duplicate session ID
// =====================

func TestSessionCreateDuplicateID(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider and repo
	provider, err := env.client.ScmProvider.Create().
		SetName("dup-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	_, err = env.client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("dup-repo").
		SetFullName("org/dup-repo").
		SetCloneURL("https://github.com/org/dup-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	sessionID := "660e8400-e29b-41d4-a716-446655440000"
	createReq := map[string]interface{}{
		"id":             sessionID,
		"repo_full_name": "org/dup-repo",
		"branch":         "main",
	}

	// First create should succeed
	w := doRequest(env, "POST", "/api/v1/sessions", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second create with same ID should fail
	w = doRequest(env, "POST", "/api/v1/sessions", createReq)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("duplicate create: expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 4. session.go:110 Stop — invalid UUID
// =====================

func TestSessionStopInvalidUUID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/sessions/not-a-valid-uuid/stop", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 5. repo.go:99 Update — invalid body (bad JSON)
// =====================

func TestRepoUpdateInvalidBody(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	// Send a string that will be marshaled as a JSON string, not an object
	w := doRequest(env, "PUT", fmt.Sprintf("/api/v1/repos/%d", repoID), "not-a-json-object")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 6. repo.go:23 List — with filters (scm_provider_id, status, group_id)
// =====================

func TestRepoListWithFilters(t *testing.T) {
	env := setupFullTestEnv(t)
	ctx := context.Background()

	// Create two providers
	p1, err := env.client.ScmProvider.Create().
		SetName("filter-gh-1").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider 1: %v", err)
	}
	p2, err := env.client.ScmProvider.Create().
		SetName("filter-gh-2").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider 2: %v", err)
	}

	// Create repos with different providers, statuses, and group_ids
	_, err = env.client.RepoConfig.Create().
		SetScmProviderID(p1.ID).
		SetName("repo-a").
		SetFullName("org/repo-a").
		SetCloneURL("https://github.com/org/repo-a.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SetGroupID("team-alpha").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo-a: %v", err)
	}

	_, err = env.client.RepoConfig.Create().
		SetScmProviderID(p2.ID).
		SetName("repo-b").
		SetFullName("org/repo-b").
		SetCloneURL("https://github.com/org/repo-b.git").
		SetDefaultBranch("main").
		SetStatus("inactive").
		SetGroupID("team-beta").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo-b: %v", err)
	}

	// Filter by scm_provider_id
	w := doFullRequest(env, "GET", fmt.Sprintf("/api/v1/repos?scm_provider_id=%d", p1.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 1 {
		t.Errorf("filter by scm_provider_id: total = %d, want 1", total)
	}

	// Filter by status
	w = doFullRequest(env, "GET", "/api/v1/repos?status=active", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = parseFullResponse(t, w)
	data = resp["data"].(map[string]interface{})
	total = int(data["total"].(float64))
	if total != 1 {
		t.Errorf("filter by status=active: total = %d, want 1", total)
	}

	// Filter by group_id
	w = doFullRequest(env, "GET", "/api/v1/repos?group_id=team-beta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = parseFullResponse(t, w)
	data = resp["data"].(map[string]interface{})
	total = int(data["total"].(float64))
	if total != 1 {
		t.Errorf("filter by group_id=team-beta: total = %d, want 1", total)
	}
}

// =====================
// 7. scmprovider.go:44 List — multiple providers
// =====================

func TestSCMProviderListMultiple(t *testing.T) {
	env := setupTestEnv(t)

	// Create two providers
	for i := 0; i < 2; i++ {
		createReq := map[string]interface{}{
			"name":        fmt.Sprintf("Provider-%d", i),
			"type":        "github",
			"base_url":    "https://api.github.com",
			"credentials": map[string]string{"token": fmt.Sprintf("ghp_test%d", i)},
		}
		w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
		if w.Code != http.StatusCreated {
			t.Fatalf("create provider %d: expected 201, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	// List should return both
	w := doRequest(env, "GET", "/api/v1/scm-providers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be a list, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 providers, got %d", len(data))
	}
}

// =====================
// 9. scmprovider.go:85 Update — with status field and invalid body
// =====================

func TestSCMProviderUpdateWithStatus(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider
	createReq := map[string]interface{}{
		"name":        "StatusGH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_status"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))

	// Update with status field
	updateReq := map[string]interface{}{
		"status": "inactive",
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Fatalf("update with status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = parseResponse(t, w)
	updatedData := resp["data"].(map[string]interface{})
	if updatedData["status"] != "inactive" {
		t.Errorf("expected status 'inactive', got %v", updatedData["status"])
	}
}

func TestSCMProviderUpdateInvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider
	createReq := map[string]interface{}{
		"name":        "BadBodyGH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_badbody"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))

	// Send invalid JSON string as body — doRequest marshals it as a JSON string "bad"
	// which won't bind to updateSCMProviderRequest struct properly.
	// Actually, a JSON string is valid JSON but won't bind to a struct.
	// We need to use doFullRequestWithToken-style to send raw bad JSON.
	// Let's use doCustomRequest with a custom router approach instead.
	// Actually, doRequest will marshal "bad" to `"bad"` which is valid JSON but not an object.
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update with invalid body: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 10. efficiency.go:59 RepoMetrics — invalid period param
// =====================

func TestRepoMetricsWithInvalidPeriod(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	// Use a non-standard period value — the handler passes it through to the query
	// which should still succeed (returns empty results) since it's just a filter
	w := doFullRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/metrics?period=bogus", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	// Should return empty data since no metrics exist with period_type "bogus"
	data := resp["data"]
	if data == nil {
		return // nil is acceptable for empty
	}
	if items, ok := data.([]interface{}); ok && len(items) != 0 {
		t.Errorf("expected empty metrics for bogus period, got %d items", len(items))
	}
}

func TestRepoMetricsWithWeeklyPeriod(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	w := doFullRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/metrics?period=weekly", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 11. efficiency.go:85 Trend — custom limit param
// =====================

func TestTrendWithCustomLimit(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	w := doFullRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/trend?limit=5", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTrendWithDailyPeriod(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	w := doFullRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/trend?period=daily&limit=3", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// 12. settings.go:141 TestLLMConnection — LLM not configured (empty URL/key)
// =====================

func TestTestLLMConnectionNotConfigured(t *testing.T) {
	// Create a minimal env with empty LLM config
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(configPath, []byte("analysis:\n  llm:\n    model: gpt-4\n"), 0o644)
	sh := NewSettingsHandler(configPath, config.RelayConfig{}, llmAnalyzer, zap.NewNop())

	// Create a minimal router with auth
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)

	u, err := client.User.Create().
		SetUsername("llmtestadmin").
		SetEmail("llmtestadmin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID: u.ID, Username: "llmtestadmin", Role: "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	r := gin.New()
	r.Use(auth.RequireAuth(authSvc))
	r.POST("/api/v1/settings/llm/test", sh.TestLLMConnection)

	w := doCustomRequest(r, "POST", "/api/v1/settings/llm/test", nil)
	// Without auth token, we get 401. Use a helper with token.
	req := buildRequestWithToken("POST", "/api/v1/settings/llm/test", nil, pair.AccessToken)
	w2 := serveRequest(r, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	resp := parseCustomResponse(t, w2)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", resp["data"])
	}
	success, _ := data["success"].(bool)
	if success {
		t.Error("expected success=false when LLM not configured")
	}
	msg, _ := data["message"].(string)
	if !strings.Contains(msg, "not configured") {
		t.Errorf("expected message containing 'not configured', got %q", msg)
	}

	_ = w // suppress unused
}

// =====================
// 13. pr.go:35 ListByRepo — custom months param
// =====================

func TestListPRsByRepoCustomMonths(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create a PR with a date 6 months ago
	sixMonthsAgo := time.Now().AddDate(0, -6, 0)
	_, err := env.client.PrRecord.Create().
		SetScmPrID(200).
		SetTitle("Old PR").
		SetAuthor("dev").
		SetSourceBranch("old-branch").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetRepoConfigID(repoID).
		SetCreatedAt(sixMonthsAgo).
		Save(ctx)
	if err != nil {
		t.Fatalf("create old PR: %v", err)
	}

	// Create a recent PR
	_, err = env.client.PrRecord.Create().
		SetScmPrID(201).
		SetTitle("Recent PR").
		SetAuthor("dev").
		SetSourceBranch("new-branch").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusMerged).
		SetRepoConfigID(repoID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create recent PR: %v", err)
	}

	// months=1 should only return the recent PR
	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?months=1", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 1 {
		t.Errorf("months=1: total = %d, want 1", total)
	}

	// months=12 should return both
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?months=12", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = parseResponse(t, w)
	data = resp["data"].(map[string]interface{})
	total = int(data["total"].(float64))
	if total != 2 {
		t.Errorf("months=12: total = %d, want 2", total)
	}
}

// =====================
// Additional: session.go Create — repo found by clone_url fallback
// =====================

func TestSessionCreateByCloneURL(t *testing.T) {
	env := setupTestEnv(t)

	provider, err := env.client.ScmProvider.Create().
		SetName("clone-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	_, err = env.client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("clone-repo").
		SetFullName("org/clone-repo").
		SetCloneURL("https://github.com/org/clone-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	sessionID := uuid.New().String()
	createReq := map[string]interface{}{
		"id":             sessionID,
		"repo_full_name": "https://github.com/org/clone-repo.git",
		"branch":         "main",
	}
	w := doRequest(env, "POST", "/api/v1/sessions", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// Additional: efficiency Aggregate with period param
// =====================

func TestAggregateWithPeriodParam(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/efficiency/aggregate?period=weekly", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["period"] != "weekly" {
		t.Errorf("expected period 'weekly', got %v", data["period"])
	}
}

// =====================
// Additional: repo Update with not-found ID
// =====================

func TestRepoUpdateNotFound(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "PUT", "/api/v1/repos/99999", map[string]interface{}{"status": "active"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// auth.go:68 Me — nil user context (bypass middleware)
// =====================

func TestAuthMeNilUserContext(t *testing.T) {
	env := setupTestEnv(t)
	ah := NewAuthHandler(env.authSvc)

	// Register Me handler WITHOUT auth middleware so GetUserContext returns nil
	r := gin.New()
	r.GET("/me", ah.Me)

	w := doCustomRequest(r, "GET", "/me", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// session.go List — error paths for count/list
// =====================

func TestSessionListPaginationEdgeCases(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create a session
	sid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440050")
	_, err := env.client.Session.Create().
		SetID(sid).
		SetRepoConfigID(repoID).
		SetBranch("main").
		SetStartedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// page=0 should be clamped to 1
	w := doRequest(env, "GET", "/api/v1/sessions?page=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// page_size=0 should be clamped to 20
	w = doRequest(env, "GET", "/api/v1/sessions?page_size=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// page_size=200 should be clamped to 20
	w = doRequest(env, "GET", "/api/v1/sessions?page_size=200", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// Helpers for the TestLLMConnection unconfigured test
// =====================

func buildRequestWithToken(method, path string, body interface{}, token string) *http.Request {
	var req *http.Request
	if body != nil {
		buf, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func serveRequest(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func parseCustomResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v, body: %s", err, w.Body.String())
	}
	return resp
}
