package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// setupFullTestEnv creates a test environment with all handlers wired up,
// including settings, chat, and aggregator (unlike setupTestEnv which passes nil).
func setupFullTestEnv(t *testing.T) *fullTestEnv {
	return setupFullTestEnvWithDeployment(t, nil)
}

func setupFullTestEnvWithDeployment(t *testing.T, deploymentHandler *DeploymentHandler) *fullTestEnv {
	t.Helper()

	client := testdb.Open(t)

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	analysisCloner := analysis.NewCloner(t.TempDir(), logger)
	analysisSvc := analysis.NewService(client, analysisCloner, nil, logger)

	// LLM analyzer with config — use a relay provider pointing to a non-listening address
	// so Enabled()=true but actual LLM calls fail (connection refused).
	rp := relay.NewSub2apiProvider(http.DefaultClient, "http://localhost:19876/v1", "http://localhost:19876", "sk-test-key-12345678", "gpt-4", logger)
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, rp, logger)

	// Settings handler with temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(configPath, []byte("analysis:\n  llm:\n    max_tokens_per_scan: 100000\n"), 0o644)
	relayCfg := config.RelayConfig{URL: "http://localhost:19876", APIKey: "sk-test-key-12345678", Model: "gpt-4"}
	settingsHandler := NewSettingsHandler(configPath, relayCfg, llmAnalyzer, logger)

	// Chat handler
	chatHandler := NewChatHandler(client, llmAnalyzer, t.TempDir(), logger)

	// Aggregator
	aggregator := efficiency.NewAggregator(client, logger)

	router := SetupRouter(
		client,
		authSvc,
		repoSvc,
		analysisSvc,
		webhookHandler,
		nil, // syncService
		settingsHandler,
		chatHandler,
		aggregator,
		nil, // optimizer
		"0000000000000000000000000000000000000000000000000000000000000000",
		middleware.CORS(nil),
		nil, nil, nil, nil, nil,
		deploymentHandler,
	)

	// Create admin user
	u, err := client.User.Create().
		SetUsername("fulladmin").
		SetEmail("fulladmin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "fulladmin",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return &fullTestEnv{
		client:     client,
		router:     router,
		authSvc:    authSvc,
		token:      pair.AccessToken,
		configPath: configPath,
	}
}

type fullTestEnv struct {
	client     *ent.Client
	router     *gin.Engine
	authSvc    *auth.Service
	token      string
	configPath string
}

func doFullRequest(env *fullTestEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *httptest.ResponseRecorder
	_ = reqBody
	return doFullRequestWithToken(env, method, path, body, env.token)
}

func doFullRequestWithToken(env *fullTestEnv, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	w := httptest.NewRecorder()
	var req *http.Request
	if buf != nil {
		req, _ = http.NewRequest(method, path, httptest.NewRecorder().Body)
		// Re-create properly with body
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	env.router.ServeHTTP(w, req)
	return w
}

func parseFullResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v, body: %s", err, w.Body.String())
	}
	return resp
}

func createFullTestRepo(t *testing.T, client *ent.Client) int {
	t.Helper()
	ctx := context.Background()

	provider, err := client.ScmProvider.Create().
		SetName("test-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("cov-repo").
		SetFullName("org/cov-repo").
		SetCloneURL("https://github.com/org/cov-repo.git").
		SetDefaultBranch("main").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return rc.ID
}

func createFullNonAdminToken(t *testing.T, env *fullTestEnv) string {
	t.Helper()
	u, err := env.client.User.Create().
		SetUsername("covuser").
		SetEmail("covuser@test.com").
		SetAuthSource("ldap").
		SetRole("user").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create regular user: %v", err)
	}

	pair, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "covuser",
		Role:     "user",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return pair.AccessToken
}

// =====================
// Auth handler tests
// =====================

func TestLoginInvalidBody(t *testing.T) {
	env := setupFullTestEnv(t)

	// Empty body -> 400
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/login", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "invalid request body" {
		t.Fatalf("unexpected error message: %v", msg)
	}
}

func TestLoginNoProviders(t *testing.T) {
	env := setupFullTestEnv(t)

	// Valid body but no auth providers registered -> 401
	body := map[string]string{
		"username": "fulladmin",
		"password": "secret123",
	}
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/login", body, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefreshInvalidBody(t *testing.T) {
	env := setupFullTestEnv(t)

	// Empty body -> 400
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/refresh", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); msg != "invalid request body" {
		t.Fatalf("unexpected error message: %v", msg)
	}
}

func TestRefreshValidToken(t *testing.T) {
	env := setupFullTestEnv(t)

	// The "fulladmin" user already exists from setupFullTestEnv. Query it to get the ID.
	u, err := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).Only(context.Background())
	if err != nil {
		t.Fatalf("query fulladmin user: %v", err)
	}

	// Generate a refresh token for this user
	pair, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "fulladmin",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("generate token pair: %v", err)
	}

	body := map[string]string{
		"refresh_token": pair.RefreshToken,
	}
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/refresh", body, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data in response, got: %v", resp)
	}
	tokens, ok := data["tokens"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tokens in data, got: %v", data)
	}
	if tokens["access_token"] == nil || tokens["access_token"] == "" {
		t.Fatal("expected non-empty access_token in response")
	}
	if tokens["refresh_token"] == nil || tokens["refresh_token"] == "" {
		t.Fatal("expected non-empty refresh_token in response")
	}
	userResp, ok := data["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected user in data, got: %v", data)
	}
	if userResp["username"] != "fulladmin" {
		t.Fatalf("expected username fulladmin, got: %v", userResp["username"])
	}
}

func TestRefreshInvalidToken(t *testing.T) {
	env := setupFullTestEnv(t)

	body := map[string]string{
		"refresh_token": "garbage-token-value",
	}
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/refresh", body, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
