package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

func TestAnalyzerNotEnabled(t *testing.T) {
	a := NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	if a.Enabled() {
		t.Error("expected analyzer to be disabled with nil relay provider")
	}

	_, err := a.Analyze(context.Background(), "/tmp", nil)
	if err == nil {
		t.Error("expected error when analyzer is not configured")
	}
}

func TestAnalyzerEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	a := NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())
	if !a.Enabled() {
		t.Error("expected analyzer to be enabled")
	}
}

func TestAnalyzerCallLLM(t *testing.T) {
	mockResp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"content": `{
						"dimensions": [
							{"name": "code_readability", "score": 8, "details": "Well structured"},
							{"name": "module_coupling", "score": 7, "details": "Good separation"},
							{"name": "dependency_health", "score": 6, "details": "Some outdated deps"},
							{"name": "ai_collaboration", "score": 9, "details": "Excellent AI config"}
						],
						"suggestions": [
							{"category": "dependency_health", "message": "Update outdated deps", "priority": "medium"}
						]
					}`,
				},
			},
		},
		"usage": map[string]int{
			"total_tokens": 1500,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	a := NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())

	result, err := a.Analyze(context.Background(), t.TempDir(), nil)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}

	if len(result.Dimensions) != 4 {
		t.Errorf("dimensions count = %d, want 4", len(result.Dimensions))
	}
	if result.TotalScore != 30 {
		t.Errorf("total score = %v, want 30", result.TotalScore)
	}
	if result.TokensUsed != 1500 {
		t.Errorf("tokens used = %d, want 1500", result.TokensUsed)
	}
	if len(result.Suggestions) != 1 {
		t.Errorf("suggestions count = %d, want 1", len(result.Suggestions))
	}
}

func TestParseLLMResponseWithMarkdown(t *testing.T) {
	resp := "```json\n" + `{
		"dimensions": [
			{"name": "code_readability", "score": 5, "details": "OK"}
		],
		"suggestions": []
	}` + "\n```"

	result, err := parseLLMResponse(resp)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(result.Dimensions) != 1 {
		t.Errorf("dimensions count = %d, want 1", len(result.Dimensions))
	}
	if result.TotalScore != 5 {
		t.Errorf("total score = %v, want 5", result.TotalScore)
	}
}

func TestAnalyzerBudgetControl(t *testing.T) {
	cfg := config.LLMConfig{
		MaxTokensPerScan:   100000,
		MaxScansPerRepoDay: 3,
	}
	a := NewAnalyzer(cfg, nil, zap.NewNop())
	if a.cfg.MaxTokensPerScan != 100000 {
		t.Errorf("max_tokens_per_scan = %d, want 100000", a.cfg.MaxTokensPerScan)
	}
	if a.cfg.MaxScansPerRepoDay != 3 {
		t.Errorf("max_scans_per_repo_per_day = %d, want 3", a.cfg.MaxScansPerRepoDay)
	}
}
