package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SettingsHandler handles system settings endpoints.
type SettingsHandler struct {
	configPath   string
	relayCfg     config.RelayConfig
	relayRuntime relayRuntimeUpdater
	logger       *zap.Logger
}

type relayRuntimeUpdater interface {
	SetAdminAPIKey(apiKey string)
	SetModel(model string)
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(configPath string, relayCfg config.RelayConfig, logger *zap.Logger, relayRuntimes ...relayRuntimeUpdater) *SettingsHandler {
	h := &SettingsHandler{
		configPath: configPath,
		relayCfg:   relayCfg,
		logger:     logger,
	}
	if len(relayRuntimes) > 0 {
		h.relayRuntime = relayRuntimes[0]
	}
	return h
}

type llmConfigResponse struct {
	RelayURL         string `json:"relay_url"`
	RelayAdminAPIKey string `json:"relay_admin_api_key"` // masked
	Model            string `json:"model"`
	Enabled          bool   `json:"enabled"`
}

type llmConfigRequest struct {
	RelayAdminAPIKey string `json:"relay_admin_api_key"`
	Model            string `json:"model"`
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

// GetLLMConfig returns the current relay-backed LLM configuration.
func (h *SettingsHandler) GetLLMConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": llmConfigResponse{
			RelayURL:         h.relayCfg.URL,
			RelayAdminAPIKey: maskAPIKey(h.currentRelayAdminAPIKey()),
			Model:            h.relayCfg.Model,
			Enabled:          h.relayConfigured(),
		},
	})
}

// UpdateLLMConfig updates the relay-side admin configuration and persists it to config.yaml.
func (h *SettingsHandler) UpdateLLMConfig(c *gin.Context) {
	var req llmConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = h.relayCfg.Model
	}

	relayAdminAPIKey := strings.TrimSpace(req.RelayAdminAPIKey)
	switch {
	case relayAdminAPIKey == "":
		relayAdminAPIKey = h.currentRelayAdminAPIKey()
	case strings.Contains(relayAdminAPIKey, "****"):
		relayAdminAPIKey = h.currentRelayAdminAPIKey()
	}

	if err := h.persistRelayConfig(relayAdminAPIKey, model); err != nil {
		h.logger.Error("failed to persist relay config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to save config"})
		return
	}

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
			RelayURL:         h.relayCfg.URL,
			RelayAdminAPIKey: maskAPIKey(h.currentRelayAdminAPIKey()),
			Model:            model,
			Enabled:          h.relayConfigured(),
		},
	})
}

// TestLLMConnection tests the LLM connection with a simple request.
func (h *SettingsHandler) TestLLMConnection(c *gin.Context) {
	inferenceAPIKey := h.currentRelayAdminAPIKey()
	if strings.TrimSpace(h.relayCfg.URL) == "" || inferenceAPIKey == "" {
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
	req.Header.Set("Authorization", "Bearer "+inferenceAPIKey)

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
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"success": false, "message": "API returned " + resp.Status}})
}

func (h *SettingsHandler) persistRelayConfig(apiKey string, model string) error {
	relaySection := map[string]interface{}{
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
	return strings.TrimSpace(h.relayCfg.AdminAPIKey)
}

func (h *SettingsHandler) relayConfigured() bool {
	return strings.TrimSpace(h.relayCfg.URL) != "" && h.currentRelayAdminAPIKey() != ""
}
