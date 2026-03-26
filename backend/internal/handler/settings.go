package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SettingsHandler handles system settings endpoints.
type SettingsHandler struct {
	configPath  string
	relayCfg    config.RelayConfig
	llmAnalyzer *llm.Analyzer
	logger      *zap.Logger
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(configPath string, relayCfg config.RelayConfig, llmAnalyzer *llm.Analyzer, logger *zap.Logger) *SettingsHandler {
	return &SettingsHandler{
		configPath:  configPath,
		relayCfg:    relayCfg,
		llmAnalyzer: llmAnalyzer,
		logger:      logger,
	}
}

type llmConfigResponse struct {
	RelayURL           string `json:"relay_url"`
	RelayAPIKey        string `json:"relay_api_key"` // masked
	Model              string `json:"model"`         // from relay config, read-only
	MaxTokensPerScan   int    `json:"max_tokens_per_scan"`
	Enabled            bool   `json:"enabled"`
	SystemPrompt       string `json:"system_prompt"`
	UserPromptTemplate string `json:"user_prompt_template"`
}

type llmConfigRequest struct {
	MaxTokensPerScan   int    `json:"max_tokens_per_scan"`
	SystemPrompt       string `json:"system_prompt"`
	UserPromptTemplate string `json:"user_prompt_template"`
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:3] + "****" + key[len(key)-4:]
}

// GetLLMConfig returns the current LLM configuration.
func (h *SettingsHandler) GetLLMConfig(c *gin.Context) {
	cfg := h.llmAnalyzer.GetConfig()
	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = llm.DefaultSystemPrompt
	}
	userPromptTemplate := cfg.UserPromptTemplate
	if userPromptTemplate == "" {
		userPromptTemplate = llm.DefaultUserPromptTemplate
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": llmConfigResponse{
			RelayURL:           h.relayCfg.URL,
			RelayAPIKey:        maskAPIKey(h.relayCfg.APIKey),
			Model:              h.relayCfg.Model,
			MaxTokensPerScan:   cfg.MaxTokensPerScan,
			Enabled:            h.llmAnalyzer.Enabled(),
			SystemPrompt:       systemPrompt,
			UserPromptTemplate: userPromptTemplate,
		},
	})
}

// UpdateLLMConfig updates the LLM configuration and persists to config.yaml.
func (h *SettingsHandler) UpdateLLMConfig(c *gin.Context) {
	var req llmConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}

	if req.MaxTokensPerScan == 0 {
		req.MaxTokensPerScan = 100000
	}

	newCfg := config.LLMConfig{
		MaxTokensPerScan:   req.MaxTokensPerScan,
		SystemPrompt:       req.SystemPrompt,
		UserPromptTemplate: req.UserPromptTemplate,
	}

	// Persist to config.yaml
	if err := h.persistLLMConfig(newCfg); err != nil {
		h.logger.Error("failed to persist LLM config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to save config"})
		return
	}

	// Hot-reload analyzer
	h.llmAnalyzer.UpdateConfig(newCfg)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "LLM configuration updated",
		"data": llmConfigResponse{
			RelayURL:           h.relayCfg.URL,
			RelayAPIKey:        maskAPIKey(h.relayCfg.APIKey),
			Model:              h.relayCfg.Model,
			MaxTokensPerScan:   newCfg.MaxTokensPerScan,
			Enabled:            h.llmAnalyzer.Enabled(),
			SystemPrompt:       newCfg.SystemPrompt,
			UserPromptTemplate: newCfg.UserPromptTemplate,
		},
	})
}

// TestLLMConnection tests the LLM connection with a simple request.
func (h *SettingsHandler) TestLLMConnection(c *gin.Context) {
	if h.relayCfg.URL == "" || h.relayCfg.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": "Relay not configured"}})
		return
	}

	// Send a minimal chat completion request
	client := &http.Client{}
	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatReq struct {
		Model     string    `json:"model"`
		Messages  []chatMsg `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
	}
	body := chatReq{
		Model:     h.relayCfg.Model,
		Messages:  []chatMsg{{Role: "user", Content: "ping"}},
		MaxTokens: 5,
	}
	bodyBytes, _ := json.Marshal(body)
	url := strings.TrimRight(h.relayCfg.URL, "/") + "/v1/chat/completions"

	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": err.Error()}})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.relayCfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": err.Error()}})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": true, "message": "Connection successful"}})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": "API returned " + resp.Status}})
	}
}

func (h *SettingsHandler) persistLLMConfig(llmCfg config.LLMConfig) error {
	return updateYAMLSection(h.configPath, []string{"analysis", "llm"}, map[string]interface{}{
		"max_tokens_per_scan":        llmCfg.MaxTokensPerScan,
		"max_scans_per_repo_per_day": 3,
		"system_prompt":              llmCfg.SystemPrompt,
		"user_prompt_template":       llmCfg.UserPromptTemplate,
	})
}
