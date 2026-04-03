package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// =====================
// Chat handler — cover preview files path + history edge cases
// =====================

func TestChatWithPreviewFiles(t *testing.T) {
	// Chat with preview_files triggers the tool-calling path.
	// LLM is enabled (relay provider set) but server not running -> 503.
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	body := map[string]interface{}{
		"message": "remove the editorconfig file",
		"preview_files": []map[string]string{
			{"path": ".editorconfig", "new_content": "root = true"},
		},
	}
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), body)
	// LLM enabled but server unreachable -> 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestChatHistoryTruncation(t *testing.T) {
	// Send >20 history items, handler truncates to last 20
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	history := make([]map[string]string, 25)
	for i := 0; i < 25; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history[i] = map[string]string{"role": role, "content": fmt.Sprintf("msg %d", i)}
	}
	body := map[string]interface{}{
		"message": "hello",
		"history": history,
	}
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), body)
	// LLM enabled but server unreachable -> 503
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestChatHistoryContentTooLong2(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)

	body := map[string]interface{}{
		"message": "hello",
		"history": []map[string]string{
			{"role": "user", "content": strings.Repeat("x", 4001)},
		},
	}
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =====================
// Settings — TestLLMConnection with mock HTTP server
// =====================

func TestTestLLMConnectionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, nil, zap.NewNop())

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(configPath, []byte("analysis:\n  llm:\n    max_tokens_per_scan: 100000\n"), 0o644)
	relayCfg := config.RelayConfig{URL: server.URL, APIKey: "sk-test-key"}
	sh := NewSettingsHandler(configPath, relayCfg, analyzer, zap.NewNop())

	r := gin.New()
	r.POST("/test", sh.TestLLMConnection)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) {
		t.Errorf("expected success:true, got: %s", w.Body.String())
	}
}

func TestTestLLMConnectionAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, nil, zap.NewNop())

	relayCfg := config.RelayConfig{URL: server.URL, APIKey: "sk-bad-key"}
	sh := NewSettingsHandler(t.TempDir()+"/config.yaml", relayCfg, analyzer, zap.NewNop())

	r := gin.New()
	r.POST("/test", sh.TestLLMConnection)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"success":false`) {
		t.Errorf("expected success:false, got: %s", w.Body.String())
	}
}

// =====================
// Settings — persistLLMConfig error path
// =====================

func TestUpdateLLMConfigPersistError(t *testing.T) {
	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, nil, zap.NewNop())

	// Point to non-existent path so persistLLMConfig fails on ReadFile
	relayCfg := config.RelayConfig{URL: "http://localhost:1", APIKey: "sk-test"}
	sh := NewSettingsHandler("/nonexistent/path/config.yaml", relayCfg, analyzer, zap.NewNop())

	r := gin.New()
	r.PUT("/llm", sh.UpdateLLMConfig)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/llm", strings.NewReader(`{"max_tokens_per_scan":50000}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

// =====================
// Mock-based ListScans tests
// =====================

func TestListScans_MockSuccess(t *testing.T) {
	scanner := &mockAnalysisScanner{
		listScansFn: func(_ context.Context, _ int, _ int) ([]*ent.AiScanResult, error) {
			return []*ent.AiScanResult{{ID: 1, Score: 80}, {ID: 2, Score: 90}}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestListScans_MockError(t *testing.T) {
	scanner := &mockAnalysisScanner{
		listScansFn: func(_ context.Context, _ int, _ int) ([]*ent.AiScanResult, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestListScans_MockWithLimit(t *testing.T) {
	scanner := &mockAnalysisScanner{
		listScansFn: func(_ context.Context, _ int, limit int) ([]*ent.AiScanResult, error) {
			if limit != 5 {
				return nil, fmt.Errorf("expected limit 5, got %d", limit)
			}
			return []*ent.AiScanResult{{ID: 1, Score: 80}}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans?limit=5", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// =====================
// Mock-based LatestScan success
// =====================

func TestLatestScan_MockSuccess(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 95}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans/latest", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// =====================
// Aggregate — nil aggregator and error paths
// =====================

func TestAggregateNilAggregatorViaFull(t *testing.T) {
	// setupTestEnv has nil aggregator
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/efficiency/aggregate", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// =====================
// SCM Provider — additional coverage for List, Create error paths
// =====================

func TestSCMProviderListEmpty(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/scm-providers", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSCMProviderCreateMissingType(t *testing.T) {
	env := setupTestEnv(t)
	body := map[string]interface{}{
		"name":        "test",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "t"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSCMProviderCreateMissingName(t *testing.T) {
	env := setupTestEnv(t)
	body := map[string]interface{}{
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "t"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSCMProviderCreateMissingCredentials(t *testing.T) {
	env := setupTestEnv(t)
	body := map[string]interface{}{
		"name":     "test",
		"type":     "github",
		"base_url": "https://api.github.com",
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =====================
// SCM Provider Test — not found via ID
// =====================

func TestSCMProviderTestNotFoundByID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/scm-providers/99999/test", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =====================
// Optimize — mock ListScans error path in Optimize
// =====================

func TestOptimize_MockListScansError(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
		listScansFn: func(_ context.Context, _ int, _ int) ([]*ent.AiScanResult, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	opt := &mockOptimizer{
		createPRFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizeResult, error) {
			return &analysis.OptimizeResult{BranchName: "b", FilesAdded: 1}, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	// ListScans error doesn't affect Optimize (it uses GetLatestScan)
	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// =====================
// DevLogin — cover more branches
// =====================

func TestDevLoginExistingAdminUser(t *testing.T) {
	// Create admin user first, then DevLogin should find and reuse it
	t.Setenv("AE_DEV_LOGIN_ENABLED", "true")
	env := setupDebugTestEnv(t)

	// Create "admin" user manually
	_, err := env.client.User.Create().
		SetUsername("admin").SetEmail("admin@dev.local").
		SetAuthSource("sub2api_sso").SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["token"] == nil {
		t.Error("expected token in response")
	}
}

// =====================
// Session AddInvocation — cover more paths
// =====================

func TestSessionAddInvocationWithEndTime(t *testing.T) {
	env := setupTestEnv(t)

	// Create repo + session
	provider, _ := env.client.ScmProvider.Create().
		SetName("gh").SetType("github").
		SetBaseURL("https://api.github.com").SetCredentials("enc").
		Save(context.Background())
	rc, _ := env.client.RepoConfig.Create().
		SetScmProviderID(provider.ID).SetName("r").SetFullName("org/r").
		SetCloneURL("https://github.com/org/r.git").SetDefaultBranch("main").
		Save(context.Background())

	sessionReq := map[string]interface{}{
		"id":             "550e8400-e29b-41d4-a716-446655440020",
		"repo_full_name": rc.FullName,
		"branch":         "main",
	}
	w := doRequest(env, "POST", "/api/v1/sessions", sessionReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create session: %d, %s", w.Code, w.Body.String())
	}

	// Add invocation with end time
	invReq := map[string]interface{}{
		"tool":  "cursor",
		"start": "2026-03-17T10:00:00Z",
		"end":   "2026-03-17T10:30:00Z",
	}
	w = doRequest(env, "POST", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440020/invocations", invReq)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Add second invocation
	invReq2 := map[string]interface{}{
		"tool":  "claude-code",
		"start": "2026-03-17T11:00:00Z",
		"end":   "2026-03-17T11:15:00Z",
	}
	w = doRequest(env, "POST", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440020/invocations", invReq2)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// =====================
// Repo CreateDirect — success with valid provider
// =====================

func TestRepoCreateDirectSuccess(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider first
	provReq := map[string]interface{}{
		"name":        "GH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_fake"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", provReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: %d, %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	providerID := int(resp["data"].(map[string]interface{})["id"].(float64))

	// Create repo direct
	repoReq := map[string]interface{}{
		"scm_provider_id": providerID,
		"name":            "new-repo",
		"full_name":       "org/new-repo",
		"clone_url":       "https://github.com/org/new-repo.git",
		"default_branch":  "main",
	}
	w = doRequest(env, "POST", "/api/v1/repos/direct", repoReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

// =====================
// Repo List — with pagination
// =====================

func TestRepoListPagination(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider + 3 repos
	provider, _ := env.client.ScmProvider.Create().
		SetName("gh").SetType("github").
		SetBaseURL("https://api.github.com").SetCredentials("enc").
		Save(context.Background())
	for i := 0; i < 3; i++ {
		env.client.RepoConfig.Create().
			SetScmProviderID(provider.ID).
			SetName(fmt.Sprintf("repo-%d", i)).
			SetFullName(fmt.Sprintf("org/repo-%d", i)).
			SetCloneURL(fmt.Sprintf("https://github.com/org/repo-%d.git", i)).
			SetDefaultBranch("main").
			SaveX(context.Background())
	}

	w := doRequest(env, "GET", "/api/v1/repos?page=1&page_size=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("items = %d, want 2", len(items))
	}
	total := int(data["total"].(float64))
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

// =====================
// Efficiency — RepoMetrics and Trend error paths via invalid period
// =====================

func TestEfficiencyRepoMetricsSuccess(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/metrics?period=weekly", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestEfficiencyTrendSuccess(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/trend?period=daily&limit=5", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// =====================
// PR ListByRepo — offset and limit params
// =====================

func TestListPRsByRepoWithOffsetLimit(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	// Create 5 PRs
	for i := 1; i <= 5; i++ {
		env.client.PrRecord.Create().
			SetScmPrID(i).SetTitle(fmt.Sprintf("PR #%d", i)).
			SetAuthor("dev").SetSourceBranch(fmt.Sprintf("f-%d", i)).
			SetTargetBranch("main").SetRepoConfigID(repoID).
			SaveX(context.Background())
	}

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?months=0&limit=2&offset=1", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Errorf("items = %d, want 2", len(items))
	}
}

// =====================
// Session Stop — invalid UUID
// =====================

func TestSessionStopInvalidUUID2(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/sessions/not-a-uuid/stop", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =====================
// SCM Provider Update — with status field
// =====================

func TestSCMProviderUpdateStatus(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider
	createReq := map[string]interface{}{
		"name":        "GH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_test"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	resp := parseResponse(t, w)
	providerID := int(resp["data"].(map[string]interface{})["id"].(float64))

	// Update with status
	updateReq := map[string]interface{}{
		"status": "active",
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// =====================
// SCM Provider Delete — success then not found
// =====================

func TestSCMProviderDeleteTwice(t *testing.T) {
	env := setupTestEnv(t)

	createReq := map[string]interface{}{
		"name":        "GH2",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_test"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	resp := parseResponse(t, w)
	providerID := int(resp["data"].(map[string]interface{})["id"].(float64))

	// First delete — success
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("first delete: %d", w.Code)
	}

	// Second delete — not found
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("second delete: %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =====================
// DevLogin — cover create-new-admin path (no existing "admin" user)
// =====================

func TestDevLoginCreatesNewAdmin(t *testing.T) {
	t.Setenv("AE_DEV_LOGIN_ENABLED", "true")
	env := setupDebugTestEnv(t)

	// No "admin" user exists yet (setupDebugTestEnv creates "fulladmin", not "admin")
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	user := data["user"].(map[string]interface{})
	if user["username"] != "admin" {
		t.Errorf("username = %v, want admin", user["username"])
	}
	if data["token"] == nil || data["token"] == "" {
		t.Error("expected non-empty token")
	}
	if data["refresh_token"] == nil || data["refresh_token"] == "" {
		t.Error("expected non-empty refresh_token")
	}
}

// =====================
// SCM Provider — full CRUD cycle covering more branches
// =====================

func TestSCMProviderFullCRUDCycle(t *testing.T) {
	env := setupTestEnv(t)

	// Create with all fields
	createReq := map[string]interface{}{
		"name":        "BitbucketServer",
		"type":        "bitbucket_server",
		"base_url":    "https://bitbucket.example.com",
		"credentials": map[string]string{"token": "bb_test_token"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d, %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	providerID := int(resp["data"].(map[string]interface{})["id"].(float64))

	// List — should have 1 provider
	w = doRequest(env, "GET", "/api/v1/scm-providers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	resp = parseResponse(t, w)
	providers := resp["data"].([]interface{})
	if len(providers) != 1 {
		t.Errorf("providers count = %d, want 1", len(providers))
	}

	// Update name + base_url + credentials + status
	updateReq := map[string]interface{}{
		"name":        "BB Updated",
		"base_url":    "https://bb2.example.com",
		"credentials": map[string]string{"token": "bb_new_token"},
		"status":      "active",
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Errorf("update: %d, %s", w.Code, w.Body.String())
	}

	// Test connection
	w = doRequest(env, "POST", fmt.Sprintf("/api/v1/scm-providers/%d/test", providerID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("test: %d, %s", w.Code, w.Body.String())
	}

	// Delete
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("delete: %d", w.Code)
	}
}

// =====================
// Aggregate — with period param and single repo
// =====================

func TestAggregateWithWeeklyPeriod(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/efficiency/aggregate?period=weekly", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestAggregateSingleRepoWithPeriod(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/efficiency/aggregate?repo_id=%d&period=weekly", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// =====================
// Repo — Get and Delete success paths
// =====================

func TestRepoGetSuccess(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["full_name"] != "org/test-repo" {
		t.Errorf("full_name = %v, want org/test-repo", data["full_name"])
	}
}

func TestRepoDeleteSuccess(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "DELETE", fmt.Sprintf("/api/v1/repos/%d", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// =====================
// Session — full lifecycle covering more branches
// =====================

func TestSessionFullLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	// Create repo
	provider, _ := env.client.ScmProvider.Create().
		SetName("gh").SetType("github").
		SetBaseURL("https://api.github.com").SetCredentials("enc").
		Save(context.Background())
	rc, _ := env.client.RepoConfig.Create().
		SetScmProviderID(provider.ID).SetName("lc-repo").SetFullName("org/lc-repo").
		SetCloneURL("https://github.com/org/lc-repo.git").SetDefaultBranch("main").
		Save(context.Background())

	sessionID := "550e8400-e29b-41d4-a716-446655440030"

	// Create session
	w := doRequest(env, "POST", "/api/v1/sessions", map[string]interface{}{
		"id": sessionID, "repo_full_name": rc.FullName, "branch": "feature-lc",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d, %s", w.Code, w.Body.String())
	}
	if err := env.client.Session.UpdateOneID(mustParseUUID(sessionID)).
		SetUserID(env.userID).
		Exec(context.Background()); err != nil {
		t.Fatalf("set session owner: %v", err)
	}

	// List sessions
	w = doRequest(env, "GET", "/api/v1/sessions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}

	// Get session
	w = doRequest(env, "GET", "/api/v1/sessions/"+sessionID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}

	// Heartbeat
	w = doRequest(env, "PUT", "/api/v1/sessions/"+sessionID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("heartbeat: %d", w.Code)
	}

	// Add invocation
	w = doRequest(env, "POST", "/api/v1/sessions/"+sessionID+"/invocations", map[string]interface{}{
		"tool": "copilot", "start": "2026-03-17T10:00:00Z", "end": "2026-03-17T10:10:00Z",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("invocation: %d, %s", w.Code, w.Body.String())
	}

	// Stop
	w = doRequest(env, "POST", "/api/v1/sessions/"+sessionID+"/stop", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("stop: %d", w.Code)
	}

	// List with status filter
	w = doRequest(env, "GET", "/api/v1/sessions?status=completed", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list completed: %d", w.Code)
	}

	// List with repo_id filter
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/sessions?repo_id=%d", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list by repo: %d", w.Code)
	}
}

// =====================
// PR Get — success path
// =====================

func TestPRGetSuccess(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	pr, _ := env.client.PrRecord.Create().
		SetScmPrID(100).SetTitle("Test PR").SetAuthor("dev").
		SetSourceBranch("feature").SetTargetBranch("main").
		SetRepoConfigID(repoID).
		Save(context.Background())

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["title"] != "Test PR" {
		t.Errorf("title = %v, want Test PR", data["title"])
	}
}

// =====================
// Settings — GetLLMConfig response validation
// =====================

func TestGetLLMConfigResponseFields(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "GET", "/api/v1/settings/llm", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})

	// Verify all expected fields
	for _, key := range []string{"relay_url", "relay_api_key", "relay_admin_api_key", "model", "enabled", "system_prompt", "user_prompt_template"} {
		if _, ok := data[key]; !ok {
			t.Errorf("missing key %q in LLM config response", key)
		}
	}
	// system_prompt and user_prompt_template should have defaults
	if data["system_prompt"] == "" {
		t.Error("system_prompt should have default value")
	}
	if data["user_prompt_template"] == "" {
		t.Error("user_prompt_template should have default value")
	}
}
