package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic("invalid UUID: " + s)
	}
	return id
}

func init() {
	gin.SetMode(gin.TestMode)
}

type testEnv struct {
	client  *ent.Client
	router  *gin.Engine
	authSvc *auth.Service
	token   string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	analysisCloner := analysis.NewCloner(t.TempDir(), logger)
	analysisSvc := analysis.NewService(client, analysisCloner, nil, logger)

	router := SetupRouter(
		client,
		authSvc,
		repoSvc,
		analysisSvc,
		webhookHandler,
		nil, // syncService
		nil, // settingsHandler
		nil, // chatHandler
		nil, // aggregator
		nil, // optimizer
		"0000000000000000000000000000000000000000000000000000000000000000",
		middleware.CORS(nil),
		nil, nil, nil, nil, nil,
	)

	// Create a test admin user
	u, err := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	// Generate JWT token for the admin user
	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "admin",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return &testEnv{
		client:  client,
		router:  router,
		authSvc: authSvc,
		token:   pair.AccessToken,
	}
}

func doRequest(env *testEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if env.token != "" {
		req.Header.Set("Authorization", "Bearer "+env.token)
	}
	env.router.ServeHTTP(w, req)
	return w
}

func parseResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v, body: %s", err, w.Body.String())
	}
	return resp
}

// --- Health ---

func TestHealthEndpoint(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/health", nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := parseResponse(t, w)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

// --- Auth ---

func TestAuthMeWithValidToken(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/auth/me", nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestAuthMeWithoutToken(t *testing.T) {
	env := setupTestEnv(t)
	env.token = "" // clear token
	w := doRequest(env, "GET", "/api/v1/auth/me", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- SCM Providers ---

func TestSCMProviderCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// Create
	createReq := map[string]interface{}{
		"name":        "GitHub",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_test123"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))

	// List
	w = doRequest(env, "GET", "/api/v1/scm-providers", nil)
	if w.Code != http.StatusOK {
		t.Errorf("list status = %d, want %d", w.Code, http.StatusOK)
	}

	// Update
	updateReq := map[string]interface{}{
		"name": "GitHub Enterprise",
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Errorf("update status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Delete
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("delete status = %d, want %d", w.Code, http.StatusOK)
	}

	// Delete again — should 404
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete again status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestSCMProviderRequiresAdmin(t *testing.T) {
	env := setupTestEnv(t)

	// Create a regular user token
	u, _ := env.client.User.Create().
		SetUsername("regularuser").
		SetEmail("user@test.com").
		SetAuthSource("ldap").
		SetRole("user").
		Save(context.Background())

	pair, _ := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "regularuser",
		Role:     "user",
	})
	env.token = pair.AccessToken

	w := doRequest(env, "GET", "/api/v1/scm-providers", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// --- Sessions ---

func TestSessionLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	// First create an SCM provider and repo for the session
	provider, err := env.client.ScmProvider.Create().
		SetName("test-github").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	rc, err := env.client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("test-repo").
		SetFullName("org/test-repo").
		SetCloneURL("https://github.com/org/test-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	_ = rc

	// Create session
	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	createReq := map[string]interface{}{
		"id":             sessionID,
		"repo_full_name": "org/test-repo",
		"branch":         "feature-x",
	}
	w := doRequest(env, "POST", "/api/v1/sessions", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create session status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	// Heartbeat
	w = doRequest(env, "PUT", "/api/v1/sessions/"+sessionID, nil)
	if w.Code != http.StatusOK {
		t.Errorf("heartbeat status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Add invocation
	invReq := map[string]interface{}{
		"tool":  "claude-code",
		"start": "2026-03-17T10:00:00Z",
		"end":   "2026-03-17T10:05:00Z",
	}
	w = doRequest(env, "POST", "/api/v1/sessions/"+sessionID+"/invocations", invReq)
	if w.Code != http.StatusOK {
		t.Errorf("add invocation status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Stop session
	w = doRequest(env, "POST", "/api/v1/sessions/"+sessionID+"/stop", nil)
	if w.Code != http.StatusOK {
		t.Errorf("stop session status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify session is completed
	s, err := env.client.Session.Get(context.Background(), mustParseUUID(sessionID))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if s.Status != "completed" {
		t.Errorf("session status = %s, want completed", s.Status)
	}
	if s.EndedAt == nil {
		t.Error("session ended_at should not be nil")
	}
	if len(s.ToolInvocations) != 1 {
		t.Errorf("tool_invocations len = %d, want 1", len(s.ToolInvocations))
	}
}

func TestSessionCreateInvalidUUID(t *testing.T) {
	env := setupTestEnv(t)

	req := map[string]interface{}{
		"id":             "not-a-uuid",
		"repo_full_name": "org/repo",
		"branch":         "main",
	}
	w := doRequest(env, "POST", "/api/v1/sessions", req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSessionCreateUnknownRepo(t *testing.T) {
	env := setupTestEnv(t)

	req := map[string]interface{}{
		"id":             "550e8400-e29b-41d4-a716-446655440001",
		"repo_full_name": "org/nonexistent",
		"branch":         "main",
	}
	w := doRequest(env, "POST", "/api/v1/sessions", req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// --- Repos (direct create, no SCM validation) ---

func TestRepoDirectCreateAndList(t *testing.T) {
	env := setupTestEnv(t)

	// Create SCM provider first
	providerReq := map[string]interface{}{
		"name":        "GitHub",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_fake"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", providerReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider status = %d, body: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	providerData := resp["data"].(map[string]interface{})
	providerID := int(providerData["id"].(float64))

	// Create repo via direct endpoint (skips SCM validation)
	createReq := map[string]interface{}{
		"scm_provider_id": providerID,
		"name":            "test-repo",
		"full_name":       "org/test-repo",
		"clone_url":       "https://github.com/org/test-repo.git",
		"default_branch":  "main",
	}
	w = doRequest(env, "POST", "/api/v1/repos/direct", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("direct create repo status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp = parseResponse(t, w)
	repoData := resp["data"].(map[string]interface{})
	if repoData["full_name"] != "org/test-repo" {
		t.Errorf("full_name = %v, want org/test-repo", repoData["full_name"])
	}

	// List repos
	w = doRequest(env, "GET", "/api/v1/repos", nil)
	if w.Code != http.StatusOK {
		t.Errorf("list repos status = %d, want %d", w.Code, http.StatusOK)
	}
	resp = parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Errorf("repos count = %d, want 1", len(items))
	}

	// Get single repo
	repoID := int(repoData["id"].(float64))
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("get repo status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Delete repo
	w = doRequest(env, "DELETE", fmt.Sprintf("/api/v1/repos/%d", repoID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("delete repo status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestSessionStopNotFound(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/sessions/550e8400-e29b-41d4-a716-446655440099/stop", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
