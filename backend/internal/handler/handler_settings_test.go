package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestGetLLMConfig(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodGet, "/api/v1/settings/llm", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", resp["data"])
	}

	if url, _ := data["relay_url"].(string); url != "http://localhost:19876" {
		t.Fatalf("relay_url = %q", url)
	}
	if model, _ := data["model"].(string); model != "gpt-4" {
		t.Fatalf("model = %q", model)
	}
	if _, ok := data["relay_api_key"]; ok {
		t.Fatalf("relay_api_key should not be exposed in LLM config response")
	}
	if adminAPIKey, _ := data["relay_admin_api_key"].(string); !strings.Contains(adminAPIKey, "****") {
		t.Fatalf("relay_admin_api_key should be masked, got %q", adminAPIKey)
	}
	if enabled, _ := data["enabled"].(bool); !enabled {
		t.Fatal("enabled should be true")
	}
}

func TestGetLLMConfigRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/settings/llm", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateLLMConfig(t *testing.T) {
	env := setupFullTestEnv(t)

	body := map[string]interface{}{
		"relay_admin_api_key": "admin-new-key-12345678",
		"model":               "gpt-4o-mini",
	}

	w := doFullRequest(env, http.MethodPut, "/api/v1/settings/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", resp["data"])
	}
	if model, _ := data["model"].(string); model != "gpt-4o-mini" {
		t.Fatalf("model = %q", model)
	}
	if adminAPIKey, _ := data["relay_admin_api_key"].(string); !strings.HasSuffix(adminAPIKey, "5678") {
		t.Fatalf("expected masked admin key suffix, got %q", adminAPIKey)
	}

	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configStr := string(configData)
	if !strings.Contains(configStr, "admin_api_key: admin-new-key-12345678") {
		t.Fatalf("updated admin_api_key missing from config:\n%s", configStr)
	}
	if !strings.Contains(configStr, "model: gpt-4o-mini") {
		t.Fatalf("updated model missing from config:\n%s", configStr)
	}
}

func TestUpdateLLMConfigRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	body := map[string]interface{}{"model": "gpt-4o-mini"}
	w := doFullRequestWithToken(env, http.MethodPut, "/api/v1/settings/llm", body, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateLLMConfigUpdatesModelUsedByConnectionTest(t *testing.T) {
	var captured struct {
		Model         string `json:"model"`
		Authorization string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		captured.Authorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer server.Close()

	configPath := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("relay:\n  url: http://old.example\n  admin_api_key: admin-old\n  model: old-model\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sh := NewSettingsHandler(configPath, config.RelayConfig{
		URL:         server.URL,
		AdminAPIKey: "admin-test-key",
		Model:       "old-model",
	}, zap.NewNop())

	r := gin.New()
	r.PUT("/llm", sh.UpdateLLMConfig)
	r.POST("/llm/test", sh.TestLLMConnection)

	updateReq := httptest.NewRequest(http.MethodPut, "/llm", bytes.NewBufferString(`{"model":"new-model"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	r.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status = %d, body: %s", updateResp.Code, updateResp.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/llm/test", nil)
	testResp := httptest.NewRecorder()
	r.ServeHTTP(testResp, testReq)
	if testResp.Code != http.StatusOK {
		t.Fatalf("test status = %d, body: %s", testResp.Code, testResp.Body.String())
	}

	if captured.Model != "new-model" {
		t.Fatalf("connection test used model %q", captured.Model)
	}
	if captured.Authorization != "Bearer admin-test-key" {
		t.Fatalf("connection test authorization = %q, want admin key", captured.Authorization)
	}
}

func TestTestLLMConnection(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/settings/llm/test", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", resp["data"])
	}
	if success, _ := data["success"].(bool); success {
		t.Fatal("expected connection test to fail against localhost:19876")
	}
	if msg, _ := data["message"].(string); msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestTestLLMConnectionUsesCustomPromptFromRequest(t *testing.T) {
	var captured struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Custom prompt worked."}}]}`))
	}))
	defer server.Close()

	configPath := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sh := NewSettingsHandler(configPath, config.RelayConfig{
		URL:         server.URL,
		AdminAPIKey: "admin-test-key",
		Model:       "gpt-5.4",
	}, zap.NewNop())

	r := gin.New()
	r.POST("/llm/test", sh.TestLLMConnection)

	req := httptest.NewRequest(http.MethodPost, "/llm/test", bytes.NewBufferString(`{"prompt":"Say hello from custom test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", resp.Code, resp.Body.String())
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Content != "Say hello from custom test" {
		t.Fatalf("captured messages = %+v", captured.Messages)
	}
}

func TestTestLLMConnectionRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/settings/llm/test", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
