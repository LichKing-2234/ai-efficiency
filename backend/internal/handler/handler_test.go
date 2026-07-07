package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	userID  int
}

func setupTestEnv(t *testing.T) *testEnv {
	return setupTestEnvWithOAuth(t, nil)
}

func setupTestEnvWithOAuth(t *testing.T, oauthHandler *oauth.Handler) *testEnv {
	t.Helper()

	client := testdb.Open(t)

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)

	router := SetupRouter(
		client,
		nil,
		authSvc,
		repoSvc,
		webhookHandler,
		nil, // syncService
		nil, // settingsHandler
		"0000000000000000000000000000000000000000000000000000000000000000",
		"",
		middleware.CORS(nil),
		oauthHandler, nil, nil, nil,
		nil,
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
		userID:  u.ID,
	}
}

func issueTokenForUser(t *testing.T, env *testEnv, id int, username, role string) string {
	t.Helper()

	pair, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       id,
		Username: username,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return pair.AccessToken
}

func doRequest(env *testEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	return doRequestWithToken(env, method, path, body, env.token)
}

func doRequestWithToken(env *testEnv, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["email"] != "admin@test.com" {
		t.Fatalf("email = %v, want admin@test.com", data["email"])
	}
	if data["auth_source"] != "sub2api_sso" {
		t.Fatalf("auth_source = %v, want sub2api_sso", data["auth_source"])
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

func TestListProvidersForUserWithValidToken(t *testing.T) {
	env := setupTestEnvWithProvider(t)
	w := doRequest(env, "GET", "/api/v1/providers", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got: %v", resp)
	}
	if _, ok := data["providers"]; !ok {
		t.Fatalf("expected providers field, got: %v", data)
	}
}

func TestAdminRelayProviderCreateAndUpdateMaskAdminAPIKey(t *testing.T) {
	env := setupTestEnvWithProvider(t)

	createReq := map[string]interface{}{
		"name":          "sub2api-main",
		"display_name":  "Sub2API Main",
		"base_url":      "https://sub2api.agoraio.cn",
		"relay_type":    "sub2api",
		"admin_api_key": "admin-secret-key",
		"default_model": "gpt-5.4",
		"is_primary":    true,
		"enabled":       true,
	}

	w := doRequest(env, "POST", "/api/v1/admin/providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if got := data["admin_api_key"]; got != "***" {
		t.Fatalf("admin_api_key = %v, want ***", got)
	}

	providerID := int(data["id"].(float64))
	updateReq := map[string]interface{}{
		"display_name": "Sub2API Updated",
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/admin/providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	data = resp["data"].(map[string]interface{})
	if got := data["admin_api_key"]; got != "***" {
		t.Fatalf("updated admin_api_key = %v, want ***", got)
	}
	if got := data["display_name"]; got != "Sub2API Updated" {
		t.Fatalf("display_name = %v, want Sub2API Updated", got)
	}
}

func TestAdminRelayProviderTestRouteRemoved(t *testing.T) {
	env := setupTestEnvWithProvider(t)
	provider := env.client.RelayProvider.Create().
		SetName("relay-main").
		SetDisplayName("Relay Main").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("***").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(context.Background())

	w := doRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/providers/%d/test", provider.ID), map[string]interface{}{
		"platform": "openai",
		"model":    "gpt-5.4",
		"prompt":   "Hi",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestUserRelayProviderTestAllowsRegularUserOwnAPIKey(t *testing.T) {
	var chatAuth string
	var chatModel string
	var chatPrompt string

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/42/api-keys":
			if r.Header.Get("X-API-Key") != "test-admin-key" {
				t.Fatalf("list keys api key = %q, want admin key", r.Header.Get("X-API-Key"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":         9,
							"user_id":    42,
							"key":        "sk-user-openai",
							"name":       "alice",
							"status":     "active",
							"created_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
							"group": map[string]any{
								"id":       5,
								"name":     "Group Alpha",
								"platform": "openai",
							},
						},
					},
					"page":  1,
					"pages": 1,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			chatAuth = r.Header.Get("Authorization")
			var body struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode chat body: %v", err)
			}
			chatModel = body.Model
			if len(body.Messages) > 0 {
				chatPrompt = body.Messages[0].Content
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]any{"content": "pong"}},
				},
				"usage": map[string]any{"total_tokens": 3},
			})
		default:
			t.Fatalf("unexpected relay request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer relayServer.Close()

	env := setupTestEnvWithProvider(t)
	ctx := context.Background()
	env.client.User.UpdateOneID(env.userID).
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	env.token = issueTokenForUser(t, env, env.userID, "alice@example.com", "user")

	adminKey, err := encryptAESGCM("test-admin-key", "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	provider := env.client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL(relayServer.URL).
		SetAdminAPIKey(adminKey).
		SetDefaultModel("default-model").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(ctx)

	w := doRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/user/providers/%d/test", provider.ID), map[string]any{
		"platform": "openai",
		"group_id": "5",
		"model":    "gpt-5.4",
		"prompt":   "Say hello",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]any)
	if data["success"] != true || data["response"] != "pong" {
		t.Fatalf("unexpected response data: %#v", data)
	}
	if chatAuth != "Bearer sk-user-openai" {
		t.Fatalf("chat auth = %q, want user api key", chatAuth)
	}
	if chatModel != "gpt-5.4" || chatPrompt != "Say hello" {
		t.Fatalf("chat request = (%q, %q), want model and prompt", chatModel, chatPrompt)
	}
}

func TestUserRelayProviderTestUsesAnthropicMessagesEndpoint(t *testing.T) {
	var messagesAuth string
	var messagesAPIKey string
	var messagesVersion string
	var messagesModel string
	var messagesPrompt string

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/42/api-keys":
			if r.Header.Get("X-API-Key") != "test-admin-key" {
				t.Fatalf("list keys api key = %q, want admin key", r.Header.Get("X-API-Key"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":         9,
							"user_id":    42,
							"key":        "sk-user-anthropic",
							"name":       "alice",
							"status":     "active",
							"created_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
							"group": map[string]any{
								"id":       5,
								"name":     "Group Alpha",
								"platform": "anthropic",
							},
						},
					},
					"page":  1,
					"pages": 1,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			messagesAuth = r.Header.Get("Authorization")
			messagesAPIKey = r.Header.Get("x-api-key")
			messagesVersion = r.Header.Get("anthropic-version")
			var body struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode messages body: %v", err)
			}
			messagesModel = body.Model
			if len(body.Messages) > 0 {
				messagesPrompt = body.Messages[0].Content
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "pong"},
				},
				"usage": map[string]any{
					"input_tokens":  2,
					"output_tokens": 1,
				},
			})
		default:
			t.Fatalf("unexpected relay request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer relayServer.Close()

	env := setupTestEnvWithProvider(t)
	ctx := context.Background()
	env.client.User.UpdateOneID(env.userID).
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	env.token = issueTokenForUser(t, env, env.userID, "alice@example.com", "user")

	adminKey, err := encryptAESGCM("test-admin-key", "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	provider := env.client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL(relayServer.URL).
		SetAdminAPIKey(adminKey).
		SetDefaultModel("default-model").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(ctx)

	w := doRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/user/providers/%d/test", provider.ID), map[string]any{
		"platform": "anthropic",
		"group_id": "5",
		"model":    "claude-sonnet-4-6",
		"prompt":   "Say hello",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]any)
	if data["success"] != true || data["response"] != "pong" {
		t.Fatalf("unexpected response data: %#v", data)
	}
	if messagesAuth != "Bearer sk-user-anthropic" {
		t.Fatalf("messages auth = %q, want user api key", messagesAuth)
	}
	if messagesAPIKey != "sk-user-anthropic" {
		t.Fatalf("messages x-api-key = %q, want user api key", messagesAPIKey)
	}
	if messagesVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", messagesVersion)
	}
	if messagesModel != "claude-sonnet-4-6" || messagesPrompt != "Say hello" {
		t.Fatalf("messages request = (%q, %q), want model and prompt", messagesModel, messagesPrompt)
	}
}

func TestUserRelayProviderModelsUsesSelectedGroupAPIKeyAndPlatformEndpoint(t *testing.T) {
	var modelsAuth string
	var modelsGoogleKey string

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/42/api-keys":
			if r.Header.Get("X-API-Key") != "test-admin-key" {
				t.Fatalf("list keys api key = %q, want admin key", r.Header.Get("X-API-Key"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":         9,
							"user_id":    42,
							"key":        "sk-user-gemini",
							"name":       "alice",
							"status":     "active",
							"created_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
							"group": map[string]any{
								"id":       5,
								"name":     "Group Alpha",
								"platform": "gemini",
							},
						},
					},
					"page":  1,
					"pages": 1,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1beta/models":
			modelsAuth = r.Header.Get("Authorization")
			modelsGoogleKey = r.Header.Get("x-goog-api-key")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"models": []any{
					map[string]any{
						"name":        "models/gemini-2.5-flash",
						"displayName": "Gemini 2.5 Flash",
						"supportedGenerationMethods": []string{
							"generateContent",
							"streamGenerateContent",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected relay request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer relayServer.Close()

	env := setupTestEnvWithProvider(t)
	ctx := context.Background()
	env.client.User.UpdateOneID(env.userID).
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	env.token = issueTokenForUser(t, env, env.userID, "alice@example.com", "user")

	adminKey, err := encryptAESGCM("test-admin-key", "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	provider := env.client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL(relayServer.URL).
		SetAdminAPIKey(adminKey).
		SetDefaultModel("default-model").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(ctx)

	w := doRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/user/providers/%d/groups/5/models?platform=gemini", provider.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]any)
	models := data["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models len = %d, want 1; data=%#v", len(models), data)
	}
	model := models[0].(map[string]any)
	if model["id"] != "gemini-2.5-flash" || model["display_name"] != "Gemini 2.5 Flash" {
		t.Fatalf("unexpected model: %#v", model)
	}
	if modelsAuth != "Bearer sk-user-gemini" {
		t.Fatalf("models auth = %q, want user api key", modelsAuth)
	}
	if modelsGoogleKey != "sk-user-gemini" {
		t.Fatalf("models x-goog-api-key = %q, want user api key", modelsGoogleKey)
	}
}

func TestUserRelayProviderTestRequiresSelectedGroupAPIKey(t *testing.T) {
	chatCalled := false

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/42/api-keys":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []any{
						map[string]any{
							"id":         9,
							"user_id":    42,
							"key":        "sk-wrong-group-openai",
							"name":       "alice",
							"status":     "active",
							"created_at": time.Now().Add(-time.Minute).Format(time.RFC3339),
							"group": map[string]any{
								"id":       5,
								"name":     "Group Alpha",
								"platform": "openai",
							},
						},
					},
					"page":  1,
					"pages": 1,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			chatCalled = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": "pong"}}},
			})
		default:
			t.Fatalf("unexpected relay request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer relayServer.Close()

	env := setupTestEnvWithProvider(t)
	ctx := context.Background()
	env.client.User.UpdateOneID(env.userID).
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	env.token = issueTokenForUser(t, env, env.userID, "alice@example.com", "user")

	adminKey, err := encryptAESGCM("test-admin-key", "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	provider := env.client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL(relayServer.URL).
		SetAdminAPIKey(adminKey).
		SetDefaultModel("default-model").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(ctx)

	w := doRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/user/providers/%d/test", provider.ID), map[string]any{
		"platform": "openai",
		"group_id": "6",
		"model":    "gpt-5.4",
		"prompt":   "Say hello",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]any)
	if data["success"] != false {
		t.Fatalf("success = %v, want false; data=%#v", data["success"], data)
	}
	if chatCalled {
		t.Fatal("chat completion was called with a key from a different group")
	}
}

func setupTestEnvWithProvider(t *testing.T) *testEnv {
	t.Helper()

	client := testdb.Open(t)

	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	providerHandler := NewProviderHandler(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)

	router := SetupRouter(
		client,
		nil,
		authSvc,
		repoSvc,
		webhookHandler,
		nil,
		nil,
		"0000000000000000000000000000000000000000000000000000000000000000",
		"",
		middleware.CORS(nil),
		nil, providerHandler, nil, nil,
		nil,
	)

	u, err := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

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
		userID:  u.ID,
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
		"ssh_host":    "git.example.com",
		"credentials": map[string]string{"token": "ghp_test123"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))
	if data["ssh_host"] != "git.example.com" {
		t.Fatalf("ssh_host = %v, want git.example.com", data["ssh_host"])
	}

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
