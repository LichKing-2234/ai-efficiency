package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// testRelayProvider creates a relay.Provider backed by the given httptest.Server.
func testRelayProvider(server *httptest.Server) relay.Provider {
	return relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "test-model", zap.NewNop())
}

// dummyRelayProvider creates a relay.Provider backed by a simple 200-OK server.
func dummyRelayProvider() (relay.Provider, *httptest.Server) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	return testRelayProvider(server), server
}

// ---------------------------------------------------------------------------
// Enabled / Config tests
// ---------------------------------------------------------------------------

func TestAnalyzerEnabledNilProvider(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	if a.Enabled() {
		t.Error("should be disabled with nil relay provider")
	}
}

func TestAnalyzerEnabledWithProvider(t *testing.T) {
	rp, srv := dummyRelayProvider()
	defer srv.Close()
	a := NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())
	if !a.Enabled() {
		t.Error("should be enabled with non-nil relay provider")
	}
}

func TestUpdateConfig(t *testing.T) {
	// Enabled() depends on relay provider, not config fields.
	// With nil provider, Enabled() is always false regardless of config.
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	if a.Enabled() {
		t.Error("should start disabled with nil provider")
	}

	a.UpdateConfig(config.LLMConfig{
	})

	// Still disabled because relay provider is nil
	if a.Enabled() {
		t.Error("should still be disabled — UpdateConfig does not set relay provider")
	}

	cfg := a.GetConfig()
	if cfg.SystemPrompt != "" {
		t.Errorf("system_prompt = %q, want empty", cfg.SystemPrompt)
	}
}

func TestGetConfig(t *testing.T) {
	cfg := config.LLMConfig{
		MaxTokensPerScan:   50000,
		MaxScansPerRepoDay: 5,
	}
	a := NewAnalyzer(cfg, nil, zap.NewNop())
	got := a.GetConfig()
	if got.MaxTokensPerScan != 50000 {
		t.Errorf("MaxTokensPerScan = %d, want 50000", got.MaxTokensPerScan)
	}
	if got.MaxScansPerRepoDay != 5 {
		t.Errorf("MaxScansPerRepoDay = %d, want 5", got.MaxScansPerRepoDay)
	}
}

// ---------------------------------------------------------------------------
// Analyze tests
// ---------------------------------------------------------------------------

func TestAnalyzeNotConfigured(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	_, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %q, want 'not configured'", err.Error())
	}
}

func TestAnalyzeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	_, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want to contain '500'", err.Error())
	}
}

func TestAnalyzeInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"not valid json at all"}}],"usage":{"total_tokens":10}}`))
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	_, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON in LLM response")
	}
}

func TestAnalyzeNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []interface{}{},
			"usage":   map[string]int{"total_tokens": 0},
		})
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	_, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestAnalyzeScoreCapping(t *testing.T) {
	// Score > 10 should be capped to 10
	mockResp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"content": `{"dimensions":[{"name":"test","score":15,"details":"over max"}],"suggestions":[]}`,
				},
			},
		},
		"usage": map[string]int{"total_tokens": 100},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	result, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Dimensions[0].Score != 10 {
		t.Errorf("score = %v, want 10 (capped)", result.Dimensions[0].Score)
	}
	if result.TotalScore != 10 {
		t.Errorf("total = %v, want 10", result.TotalScore)
	}
}

func TestAnalyzeWithRepoOverride(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"dimensions":[],"suggestions":[]}`}},
			},
			"usage": map[string]int{"total_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	a := NewAnalyzer(config.LLMConfig{
	}, testRelayProvider(server), zap.NewNop())

	override := &ScanPromptOverride{
		SystemPrompt: "Custom system prompt",
	}
	_, err := a.Analyze(context.Background(), t.TempDir(), override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the custom system prompt was used
	messages, ok := capturedBody["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatal("expected messages in request body")
	}
	firstMsg, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected first message to be a map")
	}
	if firstMsg["content"] != "Custom system prompt" {
		t.Errorf("system prompt = %q, want 'Custom system prompt'", firstMsg["content"])
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestChatNotConfigured(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	_, err := a.Chat(context.Background(), "system", ChatRequest{Message: "hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %q, want 'not configured'", err.Error())
	}
}

func TestChatSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "Hello back!"}},
			},
			"usage": map[string]int{"total_tokens": 25},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	chatResp, err := a.Chat(context.Background(), "You are helpful", ChatRequest{
		Message: "Hi",
		History: []ChatMessage{{Role: "user", Content: "Previous message"}},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if chatResp.Reply != "Hello back!" {
		t.Errorf("reply = %q, want 'Hello back!'", chatResp.Reply)
	}
	if chatResp.TokensUsed != 25 {
		t.Errorf("tokens = %d, want 25", chatResp.TokensUsed)
	}
}

func TestChatAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	_, err := a.Chat(context.Background(), "system", ChatRequest{Message: "hi"})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want to contain '429'", err.Error())
	}
}

func TestChatNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []interface{}{},
			"usage":   map[string]int{"total_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	resp, err := a.Chat(context.Background(), "system", ChatRequest{Message: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "" {
		t.Errorf("reply = %q, want empty for no choices", resp.Reply)
	}
}

// ---------------------------------------------------------------------------
// ChatWithTools tests
// ---------------------------------------------------------------------------

func TestChatWithToolsNotConfigured(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	_, err := a.ChatWithTools(context.Background(), "system", ChatRequest{Message: "hi"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatWithToolsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "I'll call the tool",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]string{
									"name":      "get_weather",
									"arguments": `{"city":"Tokyo"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]int{"total_tokens": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	tools := []ToolDef{
		{
			Type: "function",
			Function: ToolFuncDef{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
	}

	resp, err := a.ChatWithTools(context.Background(), "system", ChatRequest{Message: "weather?"}, tools)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Reply != "I'll call the tool" {
		t.Errorf("reply = %q", resp.Reply)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool_calls count = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool name = %q, want 'get_weather'", resp.ToolCalls[0].Function.Name)
	}
	if resp.TokensUsed != 50 {
		t.Errorf("tokens = %d, want 50", resp.TokensUsed)
	}
}

func TestChatWithToolsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	_, err := a.ChatWithTools(context.Background(), "system", ChatRequest{Message: "hi"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatWithToolsNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []interface{}{},
			"usage":   map[string]int{"total_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	resp, err := a.ChatWithTools(context.Background(), "system", ChatRequest{Message: "hi"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "" {
		t.Errorf("reply = %q, want empty for no choices", resp.Reply)
	}
}

// ---------------------------------------------------------------------------
// resolvePrompts tests
// ---------------------------------------------------------------------------

func TestResolvePromptsDefaults(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	sys, user := a.resolvePrompts(nil)
	if sys != DefaultSystemPrompt {
		t.Errorf("system = %q, want default", sys)
	}
	if user != DefaultUserPromptTemplate {
		t.Errorf("user = %q, want default", user)
	}
}

func TestResolvePromptsGlobalOverride(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{
		SystemPrompt:       "global system",
		UserPromptTemplate: "global user",
	}, nil, zap.NewNop())

	sys, user := a.resolvePrompts(nil)
	if sys != "global system" {
		t.Errorf("system = %q, want 'global system'", sys)
	}
	if user != "global user" {
		t.Errorf("user = %q, want 'global user'", user)
	}
}

func TestResolvePromptsRepoOverride(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{
		SystemPrompt:       "global system",
		UserPromptTemplate: "global user",
	}, nil, zap.NewNop())

	override := &ScanPromptOverride{
		SystemPrompt:       "repo system",
		UserPromptTemplate: "repo user",
	}
	sys, user := a.resolvePrompts(override)
	if sys != "repo system" {
		t.Errorf("system = %q, want 'repo system'", sys)
	}
	if user != "repo user" {
		t.Errorf("user = %q, want 'repo user'", user)
	}
}

func TestResolvePromptsPartialRepoOverride(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{
		SystemPrompt:       "global system",
		UserPromptTemplate: "global user",
	}, nil, zap.NewNop())

	// Only override system prompt at repo level
	override := &ScanPromptOverride{
		SystemPrompt: "repo system only",
	}
	sys, user := a.resolvePrompts(override)
	if sys != "repo system only" {
		t.Errorf("system = %q, want 'repo system only'", sys)
	}
	if user != "global user" {
		t.Errorf("user = %q, want 'global user' (not overridden)", user)
	}
}

// ---------------------------------------------------------------------------
// CollectRepoContext tests
// ---------------------------------------------------------------------------

func TestCollectRepoContextEmpty(t *testing.T) {
	dir := t.TempDir()
	ctx, err := CollectRepoContext(dir)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(ctx, "Repository Structure") {
		t.Error("should contain structure header")
	}
}

func TestCollectRepoContextWithFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hello\nWorld"), 0o644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21"), 0o644)
	os.Mkdir(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main"), 0o644)

	ctx, err := CollectRepoContext(dir)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(ctx, "README.md") {
		t.Error("should list README.md")
	}
	if !strings.Contains(ctx, "go.mod") {
		t.Error("should list go.mod")
	}
	if !strings.Contains(ctx, "src (dir)") {
		t.Error("should list src as dir")
	}
	if !strings.Contains(ctx, "# Hello") {
		t.Error("should include README content")
	}
	if !strings.Contains(ctx, "module test") {
		t.Error("should include go.mod content")
	}
}

func TestCollectRepoContextSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log"), 0o644)

	ctx, err := CollectRepoContext(dir)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if strings.Contains(ctx, ".git (dir)") {
		t.Error("should skip .git directory")
	}
	if !strings.Contains(ctx, ".gitignore") {
		t.Error("should include .gitignore")
	}
}

func TestCollectRepoContextTruncatesLargeFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a README larger than 2000 chars
	large := strings.Repeat("x", 3000)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(large), 0o644)

	ctx, err := CollectRepoContext(dir)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(ctx, "truncated") {
		t.Error("should indicate truncation for large files")
	}
}

func TestCollectRepoContextInvalidPath(t *testing.T) {
	_, err := CollectRepoContext("/nonexistent/path/xyz")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

// ---------------------------------------------------------------------------
// parseLLMResponse tests
// ---------------------------------------------------------------------------

func TestParseLLMResponseValidJSON(t *testing.T) {
	resp := `{"dimensions":[{"name":"test","score":8,"details":"good"}],"suggestions":[{"category":"test","message":"do this","priority":"low"}]}`
	result, err := parseLLMResponse(resp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Dimensions) != 1 {
		t.Errorf("dims = %d, want 1", len(result.Dimensions))
	}
	if result.Dimensions[0].MaxScore != 10 {
		t.Errorf("max_score = %v, want 10", result.Dimensions[0].MaxScore)
	}
	if len(result.Suggestions) != 1 {
		t.Errorf("suggestions = %d, want 1", len(result.Suggestions))
	}
}

func TestParseLLMResponseInvalidJSON(t *testing.T) {
	_, err := parseLLMResponse("this is not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseLLMResponseEmptyDimensions(t *testing.T) {
	resp := `{"dimensions":[],"suggestions":[]}`
	result, err := parseLLMResponse(resp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.TotalScore != 0 {
		t.Errorf("total = %v, want 0", result.TotalScore)
	}
}

func TestParseLLMResponseWrappedInText(t *testing.T) {
	resp := `Here is the analysis:
{"dimensions":[{"name":"a","score":5,"details":"ok"}],"suggestions":[]}
That's my analysis.`
	result, err := parseLLMResponse(resp)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Dimensions) != 1 {
		t.Errorf("dims = %d, want 1", len(result.Dimensions))
	}
}

// ---------------------------------------------------------------------------
// Thread safety tests
// ---------------------------------------------------------------------------

func TestConcurrentUpdateConfigAndAnalyze(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"dimensions":[],"suggestions":[]}`}},
			},
			"usage": map[string]int{"total_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := testRelayProvider(server)
	a := NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())

	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Concurrent Analyze calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Analyze(context.Background(), dir, nil)
			if err != nil {
				errs <- err
			}
		}()
	}

	// Concurrent UpdateConfig calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a.UpdateConfig(config.LLMConfig{
			})
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

func TestConcurrentEnabledAndUpdateConfig(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			a.Enabled()
		}()
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				a.UpdateConfig(config.LLMConfig{
				})
			} else {
				a.UpdateConfig(config.LLMConfig{})
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentGetConfigAndUpdateConfig(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cfg := a.GetConfig()
			_ = cfg.SystemPrompt // just access it to detect races
		}()
		go func(i int) {
			defer wg.Done()
		}(i)
	}
	wg.Wait()
}
