package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SettingsHandler handles system settings endpoints.
type SettingsHandler struct {
	configPath   string
	relayCfg     config.RelayConfig
	relayRuntime relayRuntimeUpdater
	llmAnalyzer  *llm.Analyzer
	logger       *zap.Logger
}

type relayRuntimeUpdater interface {
	SetAdminAPIKey(apiKey string)
	SetModel(model string)
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(configPath string, relayCfg config.RelayConfig, llmAnalyzer *llm.Analyzer, logger *zap.Logger, relayRuntimes ...relayRuntimeUpdater) *SettingsHandler {
	h := &SettingsHandler{
		configPath:  configPath,
		relayCfg:    relayCfg,
		llmAnalyzer: llmAnalyzer,
		logger:      logger,
	}
	if len(relayRuntimes) > 0 {
		h.relayRuntime = relayRuntimes[0]
	}
	return h
}

type llmConfigResponse struct {
	RelayURL           string `json:"relay_url"`
	RelayAPIKey        string `json:"relay_api_key"`       // masked
	RelayAdminAPIKey   string `json:"relay_admin_api_key"` // masked
	Model              string `json:"model"`               // from relay config, admin-editable via this settings surface
	MaxTokensPerScan   int    `json:"max_tokens_per_scan"`
	Enabled            bool   `json:"enabled"`
	SystemPrompt       string `json:"system_prompt"`
	UserPromptTemplate string `json:"user_prompt_template"`
}

type llmConfigRequest struct {
	RelayAdminAPIKey   string `json:"relay_admin_api_key"`
	Model              string `json:"model"`
	MaxTokensPerScan   int    `json:"max_tokens_per_scan"`
	SystemPrompt       string `json:"system_prompt"`
	UserPromptTemplate string `json:"user_prompt_template"`
}

type llmConnectionTestRequest struct {
	Prompt string `json:"prompt"`
}

const (
	llmConnectionTestPrompt    = "Hi"
	llmConnectionTestMaxTokens = 64
)

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
			RelayAdminAPIKey:   maskAPIKey(h.currentRelayAdminAPIKey()),
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
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = h.relayCfg.Model
	}

	newCfg := config.LLMConfig{
		MaxTokensPerScan:   req.MaxTokensPerScan,
		SystemPrompt:       req.SystemPrompt,
		UserPromptTemplate: req.UserPromptTemplate,
	}

	relayAdminAPIKey := strings.TrimSpace(req.RelayAdminAPIKey)
	switch {
	case relayAdminAPIKey == "":
		relayAdminAPIKey = h.currentRelayAdminAPIKey()
	case strings.Contains(relayAdminAPIKey, "****"):
		relayAdminAPIKey = h.currentRelayAdminAPIKey()
	}

	// Persist to config.yaml
	if err := h.persistLLMConfig(newCfg); err != nil {
		h.logger.Error("failed to persist LLM config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to save config"})
		return
	}
	if err := h.persistRelayConfig(relayAdminAPIKey, model); err != nil {
		h.logger.Error("failed to persist relay config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to save config"})
		return
	}

	// Hot-reload analyzer
	h.llmAnalyzer.UpdateConfig(newCfg)
	h.relayCfg.AdminAPIKey = relayAdminAPIKey
	h.relayCfg.Model = model
	if h.relayRuntime != nil {
		h.relayRuntime.SetAdminAPIKey(relayAdminAPIKey)
		h.relayRuntime.SetModel(model)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "LLM configuration updated",
		"data": llmConfigResponse{
			RelayURL:           h.relayCfg.URL,
			RelayAPIKey:        maskAPIKey(h.relayCfg.APIKey),
			RelayAdminAPIKey:   maskAPIKey(h.currentRelayAdminAPIKey()),
			Model:              model,
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

	prompt := llmConnectionTestPrompt
	if c.Request.Body != nil {
		reqBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
			return
		}
		if len(bytes.TrimSpace(reqBody)) > 0 {
			var testReq llmConnectionTestRequest
			if err := json.Unmarshal(reqBody, &testReq); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
				return
			}
			if v := strings.TrimSpace(testReq.Prompt); v != "" {
				prompt = v
			}
		}
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
	type chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	body := chatReq{
		Model:     h.relayCfg.Model,
		Messages:  []chatMsg{{Role: "user", Content: prompt}},
		MaxTokens: llmConnectionTestMaxTokens,
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
		var result chatResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": "failed to decode relay response: " + err.Error()}})
			return
		}

		responsePreview := ""
		if len(result.Choices) > 0 {
			responsePreview = strings.TrimSpace(result.Choices[0].Message.Content)
		}

		data := gin.H{
			"success": true,
			"message": "Connection successful",
		}
		if responsePreview != "" {
			data["response"] = responsePreview
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
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

func (h *SettingsHandler) persistRelayConfig(apiKey string, model string) error {
	relaySection := map[string]interface{}{
		"api_key":       h.relayCfg.APIKey,
		"admin_api_key": apiKey,
		"model":         model,
		"url":           h.relayCfg.URL,
	}
	if v := strings.TrimSpace(h.relayCfg.Provider); v != "" {
		relaySection["provider"] = v
	}
	if v := strings.TrimSpace(h.relayCfg.DefaultGroupID); v != "" {
		relaySection["default_group_id"] = v
	}
	return updateYAMLSection(h.configPath, []string{"relay"}, relaySection)
}

func (h *SettingsHandler) currentRelayAdminAPIKey() string {
	if v := strings.TrimSpace(h.relayCfg.AdminAPIKey); v != "" {
		return v
	}
	return h.relayCfg.APIKey
}
