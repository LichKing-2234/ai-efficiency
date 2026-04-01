package handler

import (
	"net/http"
	"os"
	"strings"
	"testing"
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
		"relay_api_key":       "admin-new-key-12345678",
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

	// Model comes from relay config (read-only)
	model, _ := data["model"].(string)
	if model != "gpt-4" {
		t.Errorf("expected model 'gpt-4' (from relay config), got %q", model)
	}

	// relay_url should still be present (read-only from relay config)
	url, _ := data["relay_url"].(string)
	if url != "http://localhost:19876" {
		t.Errorf("expected relay_url 'http://localhost:19876', got %q", url)
	}

	// API key should be masked in response
	apiKey, _ := data["relay_api_key"].(string)
	if !strings.Contains(apiKey, "****") {
		t.Errorf("expected masked API key, got %q", apiKey)
	}
	if !strings.HasSuffix(apiKey, "5678") {
		t.Errorf("expected masked API key to reflect updated key suffix, got %q", apiKey)
	}

	// Verify config file was persisted with max_tokens_per_scan and relay api key
	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	configStr := string(configData)
	if !strings.Contains(configStr, "max_tokens_per_scan") {
		t.Errorf("config file should contain 'max_tokens_per_scan', got:\n%s", configStr)
	}
	if !strings.Contains(configStr, "api_key: admin-new-key-12345678") {
		t.Errorf("config file should contain updated relay api key, got:\n%s", configStr)
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
