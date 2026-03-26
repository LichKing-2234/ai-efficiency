package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/aiscanresult"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ChatHandler handles repo chat endpoints.
type ChatHandler struct {
	entClient   *ent.Client
	llmAnalyzer *llm.Analyzer
	dataDir     string
	logger      *zap.Logger

	// Rate limiting: per-user counters
	mu       sync.Mutex
	counters map[int]*rateBucket
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(entClient *ent.Client, llmAnalyzer *llm.Analyzer, dataDir string, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		entClient:   entClient,
		llmAnalyzer: llmAnalyzer,
		dataDir:     dataDir,
		logger:      logger,
		counters:    make(map[int]*rateBucket),
	}
}

type chatHandlerRequest struct {
	Message      string            `json:"message"`
	History      []chatHistoryItem `json:"history"`
	PreviewFiles []previewFileItem `json:"preview_files,omitempty"`
}

type chatHistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type previewFileItem struct {
	Path       string `json:"path"`
	NewContent string `json:"new_content"`
}

// Chat handles POST /api/v1/repos/:id/chat
func (h *ChatHandler) Chat(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid id"})
		return
	}

	var req chatHandlerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}

	// Validate message
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}
	if len(req.Message) > 4000 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
		return
	}

	// Validate history
	if len(req.History) > 20 {
		req.History = req.History[len(req.History)-20:]
	}
	for _, h := range req.History {
		if h.Role != "user" && h.Role != "assistant" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
			return
		}
		if len(h.Content) > 4000 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request"})
			return
		}
	}

	// Check LLM configured
	if !h.llmAnalyzer.Enabled() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "LLM not configured"})
		return
	}

	// Rate limiting
	uc := auth.GetUserContext(c)
	userID := 0
	if uc != nil {
		userID = uc.UserID
	}
	if !h.checkRate(userID) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "message": "rate limit exceeded"})
		return
	}

	// Load repo
	rc, err := h.entClient.RepoConfig.Get(c.Request.Context(), repoID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "repo not found"})
		return
	}

	// Load latest scan result
	latestScan, _ := h.entClient.AiScanResult.Query().
		Where(aiscanresult.HasRepoConfigWith(repoconfig.IDEQ(repoID))).
		Order(ent.Desc(aiscanresult.FieldCreatedAt)).
		First(c.Request.Context())

	// Build system prompt
	systemPrompt := h.buildChatSystemPrompt(rc, latestScan)

	// Convert history
	history := make([]llm.ChatMessage, len(req.History))
	for i, h := range req.History {
		history[i] = llm.ChatMessage{Role: h.Role, Content: h.Content}
	}

	// Call LLM — with tools if preview files are present
	chatReq := llm.ChatRequest{
		Message: req.Message,
		History: history,
	}

	if len(req.PreviewFiles) > 0 {
		// Preview mode: inject file context into system prompt and add tools
		var fileCtx strings.Builder
		fileCtx.WriteString("\n\n## Optimization Preview Files\nThe user is reviewing the following files before submitting a PR. ")
		fileCtx.WriteString("You can use the provided tools to modify the file list. You MUST call ALL relevant tools in a single response — for example, if the user asks to remove one file, add a new file, and update another, return all three tool calls together. Do NOT split them across multiple responses.\n")
		fileCtx.WriteString("If the user asks to restore or re-add a previously removed file, look up its content from the conversation history and use add_file to add it back.\n\n")
		for _, f := range req.PreviewFiles {
			fileCtx.WriteString(fmt.Sprintf("### %s\n```\n%s\n```\n\n", f.Path, f.NewContent))
		}
		previewSystemPrompt := systemPrompt + fileCtx.String()

		tools := []llm.ToolDef{
			{
				Type: "function",
				Function: llm.ToolFuncDef{
					Name:        "remove_file",
					Description: "Remove a file from the optimization PR. Use this when the user wants to exclude a file.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path to remove, e.g. '.editorconfig'",
							},
						},
						"required": []string{"path"},
					},
				},
			},
			{
				Type: "function",
				Function: llm.ToolFuncDef{
					Name:        "add_file",
					Description: "Add a new file to the optimization PR. Use this when the user wants to create a new file that doesn't exist yet in the PR.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path for the new file, e.g. '.cursorrules'",
							},
							"content": map[string]interface{}{
								"type":        "string",
								"description": "The full content of the new file",
							},
						},
						"required": []string{"path", "content"},
					},
				},
			},
			{
				Type: "function",
				Function: llm.ToolFuncDef{
					Name:        "update_file_content",
					Description: "Update the content of an existing file in the optimization PR. Use this when the user wants to modify a file that is already in the PR.",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path to update (must already exist in the PR)",
							},
							"content": map[string]interface{}{
								"type":        "string",
								"description": "The new full content for the file",
							},
						},
						"required": []string{"path", "content"},
					},
				},
			},
		}

		resp, err := h.llmAnalyzer.ChatWithTools(c.Request.Context(), previewSystemPrompt, chatReq, tools)
		if err != nil {
			h.logger.Error("chat LLM call failed", zap.Error(err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "LLM service unavailable"})
			return
		}

		result := gin.H{
			"reply":       resp.Reply,
			"tokens_used": resp.TokensUsed,
		}
		if len(resp.ToolCalls) > 0 {
			result["tool_calls"] = resp.ToolCalls
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": result})
		return
	}

	resp, err := h.llmAnalyzer.Chat(c.Request.Context(), systemPrompt, chatReq)
	if err != nil {
		h.logger.Error("chat LLM call failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "LLM service unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"reply":       resp.Reply,
			"tokens_used": resp.TokensUsed,
		},
	})
}

func (h *ChatHandler) checkRate(userID int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	bucket, ok := h.counters[userID]
	if !ok || now.After(bucket.resetAt) {
		h.counters[userID] = &rateBucket{count: 1, resetAt: now.Add(time.Hour)}
		return true
	}
	if bucket.count >= 60 {
		return false
	}
	bucket.count++
	return true
}

func (h *ChatHandler) buildChatSystemPrompt(rc *ent.RepoConfig, scan *ent.AiScanResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are an AI code analysis assistant for the repository \"%s\".\n\n", rc.FullName))

	if scan != nil {
		sb.WriteString(fmt.Sprintf("## Latest Scan Results\nScore: %d/100\n", scan.Score))
		if dims, err := json.Marshal(scan.Dimensions); err == nil {
			sb.WriteString(fmt.Sprintf("Dimensions: %s\n", string(dims)))
		}
		if sugs, err := json.Marshal(scan.Suggestions); err == nil {
			sb.WriteString(fmt.Sprintf("Suggestions: %s\n", string(sugs)))
		}
		sb.WriteString("\n")
	}

	// Try to load file tree from clone cache
	repoPath := fmt.Sprintf("%s/repos/%d", h.dataDir, rc.ID)
	repoContext, err := llm.CollectRepoContext(repoPath)
	if err != nil {
		sb.WriteString("Note: Repository code has not been cloned yet. Answering based on scan results only.\n\n")
	} else {
		sb.WriteString(repoContext)
		sb.WriteString("\n")
	}

	sb.WriteString("Answer questions about this repository based on the scan results and code context. Be helpful and specific.")
	return sb.String()
}

