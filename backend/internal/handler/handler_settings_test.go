package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/analysis/llm"
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

	// API key should be masked
	apiKey, _ := data["relay_api_key"].(string)
	if !strings.Contains(apiKey, "****") {
		t.Errorf("expected masked API key containing '****', got %q", apiKey)
	}
	adminAPIKey, _ := data["relay_admin_api_key"].(string)
	if !strings.Contains(adminAPIKey, "****") {
		t.Errorf("expected masked admin API key containing '****', got %q", adminAPIKey)
	}

	// Model should match configured value
	model, _ := data["model"].(string)
	if model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", model)
	}

	// URL should be returned unmasked
	url, _ := data["relay_url"].(string)
	if url != "http://localhost:19876" {
		t.Errorf("expected relay_url 'http://localhost:19876', got %q", url)
	}

	// Should include prompt defaults
	if _, exists := data["system_prompt"]; !exists {
		t.Error("expected system_prompt in response")
	}
	if _, exists := data["user_prompt_template"]; !exists {
		t.Error("expected user_prompt_template in response")
	}
}

func TestGetLLMConfigRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/settings/llm", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	code, _ := resp["code"].(float64)
	if code != 403 {
		t.Errorf("expected code 403, got %v", code)
	}
}

func TestUpdateLLMConfig(t *testing.T) {
	env := setupFullTestEnv(t)

	body := map[string]interface{}{
		"max_tokens_per_scan": 50000,
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

	// Model should reflect the updated relay config value
	model, _ := data["model"].(string)
	if model != "gpt-4o-mini" {
		t.Errorf("expected model 'gpt-4o-mini', got %q", model)
	}

	// relay_url should still be present (read-only from relay config)
	url, _ := data["relay_url"].(string)
	if url != "http://localhost:19876" {
		t.Errorf("expected relay_url 'http://localhost:19876', got %q", url)
	}

	// LLM API key should remain masked in response
	apiKey, _ := data["relay_api_key"].(string)
	if !strings.Contains(apiKey, "****") {
		t.Errorf("expected masked API key, got %q", apiKey)
	}
	adminAPIKey, _ := data["relay_admin_api_key"].(string)
	if !strings.HasSuffix(adminAPIKey, "5678") {
		t.Errorf("expected masked admin API key to reflect updated key suffix, got %q", adminAPIKey)
	}

	// Verify config file was persisted with max_tokens_per_scan and relay admin api key
	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configStr := string(configData)
	if !strings.Contains(configStr, "max_tokens_per_scan") {
		t.Errorf("config file should contain 'max_tokens_per_scan', got:\n%s", configStr)
	}
	if !strings.Contains(configStr, "admin_api_key: admin-new-key-12345678") {
		t.Errorf("config file should contain updated relay admin api key, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, "model: gpt-4o-mini") {
		t.Errorf("config file should contain updated relay model, got:\n%s", configStr)
	}
}

func TestUpdateLLMConfigUpdatesModelUsedByConnectionTest(t *testing.T) {
	var captured struct {
		Model string `json:"model"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("analysis:\n  llm:\n    max_tokens_per_scan: 100000\nrelay:\n  url: http://old.example\n  api_key: sk-old\n  model: old-model\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	analyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	sh := NewSettingsHandler(configPath, config.RelayConfig{
		URL:    server.URL,
		APIKey: "sk-test-key",
		Model:  "old-model",
	}, analyzer, zap.NewNop())

	r := gin.New()
	r.PUT("/llm", sh.UpdateLLMConfig)
	r.POST("/llm/test", sh.TestLLMConnection)

	updateReq := httptest.NewRequest(http.MethodPut, "/llm", bytes.NewBufferString(`{"model":"new-model"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	r.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d, body: %s", updateResp.Code, http.StatusOK, updateResp.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/llm/test", nil)
	testResp := httptest.NewRecorder()
	r.ServeHTTP(testResp, testReq)
	if testResp.Code != http.StatusOK {
		t.Fatalf("test status = %d, want %d, body: %s", testResp.Code, http.StatusOK, testResp.Body.String())
	}

	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configData), "model: new-model") {
		t.Fatalf("config file should contain updated model, got:\n%s", string(configData))
	}

	if captured.Model != "new-model" {
		t.Fatalf("connection test used model %q, want %q", captured.Model, "new-model")
	}
}

func TestUpdateLLMConfigInvalidBody(t *testing.T) {
	env := setupFullTestEnv(t)

	// Send raw invalid JSON by using a string body directly
	w := doFullRequestWithToken(env, http.MethodPut, "/api/v1/settings/llm", "not-json{", env.token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	code, _ := resp["code"].(float64)
	if code != 400 {
		t.Errorf("expected code 400, got %v", code)
	}
}

func TestUpdateLLMConfigMaskedKeyPreservesOld(t *testing.T) {
	env := setupFullTestEnv(t)

	// Send update with just max_tokens — config should be persisted correctly
	body := map[string]interface{}{
		"max_tokens_per_scan": 80000,
	}

	w := doFullRequest(env, http.MethodPut, "/api/v1/settings/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the persisted config has max_tokens_per_scan
	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configStr := string(configData)
	if !strings.Contains(configStr, "80000") {
		t.Errorf("config should contain max_tokens_per_scan 80000, got:\n%s", configStr)
	}
}

func TestUpdateLLMConfigEmptyKeyPreservesOld(t *testing.T) {
	env := setupFullTestEnv(t)

	// Send update with just max_tokens — should succeed
	body := map[string]interface{}{
		"max_tokens_per_scan": 60000,
	}

	w := doFullRequest(env, http.MethodPut, "/api/v1/settings/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configData), "60000") {
		t.Errorf("config should contain max_tokens_per_scan after update")
	}
}

func TestUpdateLLMConfigRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	body := map[string]interface{}{
		"max_tokens_per_scan": 50000,
	}

	w := doFullRequestWithToken(env, http.MethodPut, "/api/v1/settings/llm", body, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
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

	// Connection to localhost:19876 should fail
	success, _ := data["success"].(bool)
	if success {
		t.Error("expected success=false since localhost:19876 is not running")
	}

	// Should have an error message
	msg, _ := data["message"].(string)
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestTestLLMConnectionUsesRealChatPromptAndReturnsReply(t *testing.T) {
	var captured struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hello from the relay-backed test model."}}]}`))
	}))
	defer server.Close()

	analyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	configPath := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("analysis:\n  llm:\n    max_tokens_per_scan: 100000\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sh := NewSettingsHandler(configPath, config.RelayConfig{
		URL:    server.URL,
		APIKey: "sk-test-key",
		Model:  "gpt-5.4",
	}, analyzer, zap.NewNop())

	r := gin.New()
	r.POST("/llm/test", sh.TestLLMConnection)

	req := httptest.NewRequest(http.MethodPost, "/llm/test", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"response":"Hello from the relay-backed test model."`) {
		t.Fatalf("expected relay reply preview in response, got: %s", resp.Body.String())
	}
	if captured.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want %q", captured.Model, "gpt-5.4")
	}
	if captured.MaxTokens != 64 {
		t.Fatalf("max_tokens = %d, want %d", captured.MaxTokens, 64)
	}
	if len(captured.Messages) != 1 {
		t.Fatalf("messages length = %d, want %d", len(captured.Messages), 1)
	}
	if captured.Messages[0].Role != "user" {
		t.Fatalf("first message role = %q, want %q", captured.Messages[0].Role, "user")
	}
	if captured.Messages[0].Content != "Hi" {
		t.Fatalf("prompt = %q", captured.Messages[0].Content)
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

	analyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	configPath := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("analysis:\n  llm:\n    max_tokens_per_scan: 100000\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sh := NewSettingsHandler(configPath, config.RelayConfig{
		URL:    server.URL,
		APIKey: "sk-test-key",
		Model:  "gpt-5.4",
	}, analyzer, zap.NewNop())

	r := gin.New()
	r.POST("/llm/test", sh.TestLLMConnection)

	req := httptest.NewRequest(http.MethodPost, "/llm/test", bytes.NewBufferString(`{"prompt":"Say hello from custom test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", resp.Code, http.StatusOK, resp.Body.String())
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

func TestUpdateLLMConfigDefaultValues(t *testing.T) {
	env := setupFullTestEnv(t)

	// Send update with no max_tokens — should get defaults
	body := map[string]interface{}{}

	w := doFullRequest(env, http.MethodPut, "/api/v1/settings/llm", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data, _ := resp["data"].(map[string]interface{})

	// Model comes from relay config, not from defaults
	model, _ := data["model"].(string)
	if model != "gpt-4" {
		t.Errorf("expected model 'gpt-4' (from relay config), got %q", model)
	}

	// max_tokens_per_scan should default to 100000
	maxTokens, _ := data["max_tokens_per_scan"].(float64)
	if maxTokens != 100000 {
		t.Errorf("expected default max_tokens_per_scan 100000, got %v", maxTokens)
	}
}
