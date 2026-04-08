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

	"github.com/ai-efficiency/backend/ent/enttest"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// setupDebugTestEnv creates a full test environment with gin in DebugMode,
// so the dev-login route is registered.
func setupDebugTestEnv(t *testing.T) *fullTestEnv {
	t.Helper()

	gin.SetMode(gin.DebugMode)
	t.Cleanup(func() { gin.SetMode(gin.TestMode) })

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	analysisCloner := analysis.NewCloner(t.TempDir(), logger)
	analysisSvc := analysis.NewService(client, analysisCloner, nil, logger)

	rp := relay.NewSub2apiProvider(http.DefaultClient, "http://localhost:19876/v1", "http://localhost:19876", "sk-test-key-12345678", "gpt-4", logger)
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, rp, logger)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(configPath, []byte("analysis:\n  llm:\n    model: gpt-4\n"), 0o644)
	relayCfg := config.RelayConfig{URL: "http://localhost:19876", APIKey: "sk-test-key-12345678"}
	settingsHandler := NewSettingsHandler(configPath, relayCfg, llmAnalyzer, logger)

	chatHandler := NewChatHandler(client, llmAnalyzer, t.TempDir(), logger)
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
		nil,
	)

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

// =====================
// DevLogin tests
// =====================

func TestDevLoginDisabledByDefault(t *testing.T) {
	// In debug mode but AE_DEV_LOGIN_ENABLED not set -> 403
	t.Setenv("AE_DEV_LOGIN_ENABLED", "")
	env := setupDebugTestEnv(t)
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "dev login disabled") {
		t.Fatalf("unexpected message: %v", msg)
	}
}

func TestDevLoginEnabled(t *testing.T) {
	t.Setenv("AE_DEV_LOGIN_ENABLED", "true")
	env := setupDebugTestEnv(t)
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data in response, got: %v", resp)
	}
	if data["token"] == nil || data["token"] == "" {
		t.Fatal("expected non-empty token")
	}
	if data["refresh_token"] == nil || data["refresh_token"] == "" {
		t.Fatal("expected non-empty refresh_token")
	}
	user, ok := data["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected user in data, got: %v", data)
	}
	if user["username"] != "admin" {
		t.Fatalf("expected username admin, got: %v", user["username"])
	}
	if user["role"] != "admin" {
		t.Fatalf("expected role admin, got: %v", user["role"])
	}
}

func TestDevLoginReusesExistingAdmin(t *testing.T) {
	t.Setenv("AE_DEV_LOGIN_ENABLED", "true")
	env := setupDebugTestEnv(t)

	// First call — creates "admin" user
	w1 := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	resp1 := parseFullResponse(t, w1)
	data1 := resp1["data"].(map[string]interface{})
	user1 := data1["user"].(map[string]interface{})
	id1 := user1["id"]

	// Second call — reuses "admin" user
	w2 := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	resp2 := parseFullResponse(t, w2)
	data2 := resp2["data"].(map[string]interface{})
	user2 := data2["user"].(map[string]interface{})
	id2 := user2["id"]

	if id1 != id2 {
		t.Fatalf("expected same user ID on second call, got %v vs %v", id1, id2)
	}
}

func TestDevLoginNotRegisteredInTestMode(t *testing.T) {
	// In TestMode (default), the dev-login route should not exist -> 404
	env := setupFullTestEnv(t)
	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in test mode, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// Chat handler tests
// =====================

func TestChatInvalidID(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/repos/abc/chat", map[string]interface{}{"message": "hello"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatInvalidBody(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	// Send a string instead of JSON object — ShouldBindJSON will fail
	w := doFullRequestWithToken(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), "not json", env.token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatEmptyMessage(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatWhitespaceOnlyMessage(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{"message": "   \t\n  "})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatMessageTooLong(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	longMsg := strings.Repeat("a", 4001)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{"message": longMsg})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatInvalidHistoryRole(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{
		"message": "hello",
		"history": []map[string]string{{"role": "system", "content": "hack"}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatHistoryContentTooLong(t *testing.T) {
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{
		"message": "hello",
		"history": []map[string]string{{"role": "user", "content": strings.Repeat("x", 4001)}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatRepoNotFound(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, "POST", "/api/v1/repos/99999/chat", map[string]interface{}{"message": "hello"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChatLLMServiceUnavailable(t *testing.T) {
	// LLM is enabled (relay provider set), repo exists, but localhost:19876 not running -> 503
	env := setupFullTestEnv(t)
	repoID := createFullTestRepo(t, env.client)
	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{"message": "hello"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "LLM service unavailable") {
		t.Fatalf("unexpected message: %v", msg)
	}
}

func TestChatLLMNotConfigured(t *testing.T) {
	// Create a fullTestEnv-like setup but with empty LLM config so Enabled() returns false
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	analysisCloner := analysis.NewCloner(t.TempDir(), logger)
	analysisSvc := analysis.NewService(client, analysisCloner, nil, logger)

	// Empty LLM config -> Enabled() returns false
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, logger)
	chatHandler := NewChatHandler(client, llmAnalyzer, t.TempDir(), logger)

	router := SetupRouter(
		client, authSvc, repoSvc, analysisSvc, webhookHandler,
		nil, nil, chatHandler, nil, nil,
		"0000000000000000000000000000000000000000000000000000000000000000",
		middleware.CORS(nil),
		nil, nil, nil, nil, nil,
		nil,
	)

	u, err := client.User.Create().
		SetUsername("chatadmin").
		SetEmail("chatadmin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID: u.ID, Username: "chatadmin", Role: "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	env := &fullTestEnv{client: client, router: router, authSvc: authSvc, token: pair.AccessToken}
	repoID := createFullTestRepo(t, env.client)

	w := doFullRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/chat", repoID), map[string]interface{}{"message": "hello"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "LLM not configured") {
		t.Fatalf("unexpected message: %v", msg)
	}
}

// =====================
// Repo Create tests
// =====================

func TestRepoCreate_InvalidBody(t *testing.T) {
	env := setupTestEnv(t)
	// Missing required fields (scm_provider_id, full_name)
	w := doRequest(env, "POST", "/api/v1/repos", map[string]interface{}{"bad": true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRepoCreate_ProviderNotFound(t *testing.T) {
	env := setupTestEnv(t)
	// Valid shape but non-existent provider -> 500 from service
	w := doRequest(env, "POST", "/api/v1/repos", map[string]interface{}{
		"scm_provider_id": 99999,
		"full_name":       "org/some-repo",
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// Repo TriggerScan tests (repo handler, not analysis handler)
// =====================

func TestRepoTriggerScan_InvalidID(t *testing.T) {
	// repoHandler.TriggerScan is not wired to the main router,
	// so we test it by registering it on a temporary router.
	env := setupTestEnv(t)
	repoHandler := NewRepoHandler(repo.NewService(env.client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop()))

	r := gin.New()
	r.POST("/test/:id/scan", repoHandler.TriggerScan)

	w := doCustomRequest(r, "POST", "/test/abc/scan", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRepoTriggerScan_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	repoHandler := NewRepoHandler(repo.NewService(env.client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop()))

	r := gin.New()
	r.POST("/test/:id/scan", repoHandler.TriggerScan)

	w := doCustomRequest(r, "POST", "/test/99999/scan", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRepoTriggerScan_Success(t *testing.T) {
	env := setupTestEnv(t)
	repoSvc := repo.NewService(env.client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop())
	repoHandler := NewRepoHandler(repoSvc)

	repoID := createTestRepo(t, env.client)

	r := gin.New()
	r.POST("/test/:id/scan", repoHandler.TriggerScan)

	w := doCustomRequest(r, "POST", fmt.Sprintf("/test/%d/scan", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// =====================
// Aggregate nil aggregator test
// =====================

func TestAggregateNilAggregator(t *testing.T) {
	// setupTestEnv passes nil for aggregator -> 503
	env := setupTestEnv(t)
	w := doRequest(env, "POST", "/api/v1/efficiency/aggregate", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "aggregator not available") {
		t.Fatalf("unexpected message: %v", msg)
	}
}

// =====================
// DevLogin with pre-existing "admin" user
// =====================

func TestDevLoginFindsExistingAdminUser(t *testing.T) {
	t.Setenv("AE_DEV_LOGIN_ENABLED", "true")
	env := setupDebugTestEnv(t)

	// Pre-create an "admin" user in the DB before calling dev-login
	_, err := env.client.User.Create().
		SetUsername("admin").
		SetEmail("admin@pre-existing.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("pre-create admin: %v", err)
	}

	w := doFullRequestWithToken(env, "POST", "/api/v1/auth/dev-login", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	user := data["user"].(map[string]interface{})
	if user["email"] != "admin@pre-existing.com" {
		t.Fatalf("expected pre-existing admin email, got: %v", user["email"])
	}

	// Verify only one "admin" user exists
	count, err := env.client.User.Query().Where(entuser.UsernameEQ("admin")).Count(context.Background())
	if err != nil {
		t.Fatalf("count admin users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 admin user, got %d", count)
	}
}

// =====================
// Helper for custom router tests
// =====================

func doCustomRequest(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	w := httptest.NewRecorder()
	var req *http.Request
	if buf != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
