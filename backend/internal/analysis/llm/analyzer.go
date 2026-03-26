package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ai-efficiency/backend/internal/analysis/rules"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// Analyzer performs LLM-assisted code analysis via relay provider.
type Analyzer struct {
	mu            sync.RWMutex
	cfg           config.LLMConfig
	relayProvider relay.Provider
	logger        *zap.Logger
}

// NewAnalyzer creates a new LLM analyzer.
func NewAnalyzer(cfg config.LLMConfig, relayProvider relay.Provider, logger *zap.Logger) *Analyzer {
	return &Analyzer{
		cfg:           cfg,
		relayProvider: relayProvider,
		logger:        logger,
	}
}

// Enabled returns true if the LLM analyzer has a relay provider configured.
func (a *Analyzer) Enabled() bool {
	return a.relayProvider != nil
}

// UpdateConfig hot-reloads the LLM configuration.
func (a *Analyzer) UpdateConfig(cfg config.LLMConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = cfg
	a.logger.Info("LLM config updated")
}

// GetConfig returns the current LLM configuration.
func (a *Analyzer) GetConfig() config.LLMConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

// ScanPromptOverride holds repo-level prompt overrides.
type ScanPromptOverride struct {
	SystemPrompt       string `json:"system_prompt,omitempty"`
	UserPromptTemplate string `json:"user_prompt_template,omitempty"`
}

// AnalysisResult holds the LLM analysis output.
type AnalysisResult struct {
	Dimensions  []rules.DimensionScore `json:"dimensions"`
	Suggestions []rules.Suggestion     `json:"suggestions"`
	TotalScore  float64                `json:"total_score"`
	TokensUsed  int                    `json:"tokens_used"`
}

// DefaultSystemPrompt is the hardcoded default system prompt for scan analysis.
const DefaultSystemPrompt = "You are a code quality analyzer. Respond ONLY with valid JSON."

// DefaultUserPromptTemplate is the hardcoded default user prompt template for scan analysis.
const DefaultUserPromptTemplate = `Analyze this repository for AI-friendliness and code quality.

{repo_context}

Score the following dimensions (each out of 10, total max 40):
1. code_readability - How readable and well-organized is the code?
2. module_coupling - How well-separated are the modules? (higher = better decoupling)
3. dependency_health - Are dependencies well-managed and up-to-date?
4. ai_collaboration - How well-suited is this repo for AI-assisted development?

Respond with ONLY this JSON structure:
{
  "dimensions": [
    {"name": "code_readability", "score": <0-10>, "details": "<brief explanation>"},
    {"name": "module_coupling", "score": <0-10>, "details": "<brief explanation>"},
    {"name": "dependency_health", "score": <0-10>, "details": "<brief explanation>"},
    {"name": "ai_collaboration", "score": <0-10>, "details": "<brief explanation>"}
  ],
  "suggestions": [
    {"category": "<dimension_name>", "message": "<actionable suggestion>", "priority": "high|medium|low"}
  ]
}`

// resolvePrompts merges prompts with three-tier fallback: repo override > global config > hardcoded default.
func (a *Analyzer) resolvePrompts(repoOverride *ScanPromptOverride) (systemPrompt, userTemplate string) {
	systemPrompt = DefaultSystemPrompt
	userTemplate = DefaultUserPromptTemplate

	cfg := a.GetConfig()
	if cfg.SystemPrompt != "" {
		systemPrompt = cfg.SystemPrompt
	}
	if cfg.UserPromptTemplate != "" {
		userTemplate = cfg.UserPromptTemplate
	}

	if repoOverride != nil {
		if repoOverride.SystemPrompt != "" {
			systemPrompt = repoOverride.SystemPrompt
		}
		if repoOverride.UserPromptTemplate != "" {
			userTemplate = repoOverride.UserPromptTemplate
		}
	}
	return
}

// Analyze runs LLM analysis on a repo directory.
// Returns dimensions scored out of 40 total (LLM weight).
func (a *Analyzer) Analyze(ctx context.Context, repoPath string, repoOverride *ScanPromptOverride) (*AnalysisResult, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("LLM analyzer not configured")
	}

	repoContext, err := CollectRepoContext(repoPath)
	if err != nil {
		return nil, fmt.Errorf("collect repo context: %w", err)
	}

	systemPrompt, userTemplate := a.resolvePrompts(repoOverride)
	userPrompt := strings.ReplaceAll(userTemplate, "{repo_context}", repoContext)

	resp, tokensUsed, err := a.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("call LLM: %w", err)
	}

	result, err := parseLLMResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}
	result.TokensUsed = tokensUsed

	return result, nil
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest holds the input for a chat request.
type ChatRequest struct {
	Message string        `json:"message"`
	History []ChatMessage `json:"history"`
}

// ChatResponse holds the output of a chat request.
type ChatResponse struct {
	Reply      string `json:"reply"`
	TokensUsed int    `json:"tokens_used"`
}

// Chat sends a conversational message to the LLM with repo context.
func (a *Analyzer) Chat(ctx context.Context, systemPrompt string, req ChatRequest) (*ChatResponse, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("LLM not configured")
	}

	var messages []relay.ChatMessage
	messages = append(messages, relay.ChatMessage{Role: "system", Content: systemPrompt})
	for _, m := range req.History {
		messages = append(messages, relay.ChatMessage{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, relay.ChatMessage{Role: "user", Content: req.Message})

	resp, err := a.relayProvider.ChatCompletion(ctx, relay.ChatCompletionRequest{
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	return &ChatResponse{
		Reply:      resp.Content,
		TokensUsed: resp.TokensUsed,
	}, nil
}

// ToolCall represents an OpenAI-compatible tool call from the LLM.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolDef represents an OpenAI-compatible tool definition.
type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

// ToolFuncDef is the function definition inside a tool.
type ToolFuncDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ChatWithToolsResponse extends ChatResponse with tool calls.
type ChatWithToolsResponse struct {
	Reply      string     `json:"reply"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}

// ChatWithTools sends a chat request with tool definitions to the LLM.
func (a *Analyzer) ChatWithTools(ctx context.Context, systemPrompt string, req ChatRequest, tools []ToolDef) (*ChatWithToolsResponse, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("LLM not configured")
	}

	var messages []relay.ChatMessage
	messages = append(messages, relay.ChatMessage{Role: "system", Content: systemPrompt})
	for _, m := range req.History {
		messages = append(messages, relay.ChatMessage{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, relay.ChatMessage{Role: "user", Content: req.Message})

	// Convert local ToolDef to relay.ToolDef
	var relayTools []relay.ToolDef
	for _, t := range tools {
		params, _ := json.Marshal(t.Function.Parameters)
		relayTools = append(relayTools, relay.ToolDef{
			Type: t.Type,
			Function: relay.ToolFuncDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  params,
			},
		})
	}

	resp, err := a.relayProvider.ChatCompletionWithTools(ctx, relay.ChatCompletionRequest{
		Messages: messages,
	}, relayTools)
	if err != nil {
		return nil, fmt.Errorf("chat completion with tools: %w", err)
	}

	// Convert relay.ToolCall to local ToolCall
	var toolCalls []ToolCall
	for _, tc := range resp.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return &ChatWithToolsResponse{
		Reply:      resp.Content,
		ToolCalls:  toolCalls,
		TokensUsed: resp.TokensUsed,
	}, nil
}

func (a *Analyzer) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, int, error) {
	resp, err := a.relayProvider.ChatCompletion(ctx, relay.ChatCompletionRequest{
		Messages: []relay.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return "", 0, fmt.Errorf("LLM call: %w", err)
	}
	return resp.Content, resp.TokensUsed, nil
}

// CollectRepoContext gathers file tree and key files from a repo directory (exported for chat handler).
func CollectRepoContext(repoPath string) (string, error) {
	var sb strings.Builder

	// List top-level files and dirs
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return "", err
	}

	sb.WriteString("## Repository Structure (top-level)\n")
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".git") && e.Name() != ".gitignore" {
			continue
		}
		kind := "file"
		if e.IsDir() {
			kind = "dir"
		}
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", e.Name(), kind))
	}

	// Read key files (truncated)
	keyFiles := []string{"README.md", "go.mod", "package.json", "pyproject.toml", "Cargo.toml", "pom.xml"}
	for _, f := range keyFiles {
		path := filepath.Join(repoPath, f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > 2000 {
			content = content[:2000] + "\n... (truncated)"
		}
		sb.WriteString(fmt.Sprintf("\n## %s\n```\n%s\n```\n", f, content))
	}

	return sb.String(), nil
}

func parseLLMResponse(resp string) (*AnalysisResult, error) {
	// Try to extract JSON from the response (LLM might wrap it in markdown)
	resp = strings.TrimSpace(resp)
	if idx := strings.Index(resp, "{"); idx >= 0 {
		if end := strings.LastIndex(resp, "}"); end >= idx {
			resp = resp[idx : end+1]
		}
	}

	var parsed struct {
		Dimensions []struct {
			Name    string  `json:"name"`
			Score   float64 `json:"score"`
			Details string  `json:"details"`
		} `json:"dimensions"`
		Suggestions []struct {
			Category string `json:"category"`
			Message  string `json:"message"`
			Priority string `json:"priority"`
		} `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (response: %.200s)", err, resp)
	}

	result := &AnalysisResult{}
	for _, d := range parsed.Dimensions {
		score := d.Score
		if score > 10 {
			score = 10
		}
		result.Dimensions = append(result.Dimensions, rules.DimensionScore{
			Name:     d.Name,
			Score:    score,
			MaxScore: 10,
			Details:  d.Details,
		})
		result.TotalScore += score
	}

	for _, s := range parsed.Suggestions {
		result.Suggestions = append(result.Suggestions, rules.Suggestion{
			Category: s.Category,
			Message:  s.Message,
			Priority: s.Priority,
		})
	}

	return result, nil
}
