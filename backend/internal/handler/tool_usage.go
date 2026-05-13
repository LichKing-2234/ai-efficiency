package handler

import (
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/toolusage"
	"github.com/gin-gonic/gin"
)

type ToolUsageHandler struct {
	service *toolusage.Service
}

func NewToolUsageHandler(service *toolusage.Service) *ToolUsageHandler {
	return &ToolUsageHandler{service: service}
}

type createToolUsageEventRequest struct {
	Tool              string         `json:"tool" binding:"required"`
	WorkspaceID       string         `json:"workspace_id" binding:"required"`
	RepoConfigID      int            `json:"repo_config_id" binding:"required"`
	UserID            int            `json:"user_id" binding:"required"`
	ToolSessionID     string         `json:"tool_session_id" binding:"required"`
	ToolEventID       string         `json:"tool_event_id"`
	DedupeKey         string         `json:"dedupe_key" binding:"required"`
	UsageUnit         string         `json:"usage_unit" binding:"required"`
	RequestCount      int            `json:"request_count"`
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	CreditUsage       float64        `json:"credit_usage"`
	ContextUsagePct   float64        `json:"context_usage_pct"`
	ObservedStartAt   time.Time      `json:"observed_start_at" binding:"required"`
	ObservedEndAt     time.Time      `json:"observed_end_at" binding:"required"`
	RawSourcePath     string         `json:"raw_source_path"`
	RawSourceLocator  string         `json:"raw_source_locator"`
	RawPayload        map[string]any `json:"raw_payload"`
}

type bindToolUsageEventsRequest struct {
	WorkspaceID        string    `json:"workspace_id" binding:"required"`
	CommitCheckpointID int       `json:"commit_checkpoint_id" binding:"required"`
	CommitCapturedAt   time.Time `json:"commit_captured_at" binding:"required"`
	PreviousCapturedAt time.Time `json:"previous_captured_at" binding:"required"`
}

func (h *ToolUsageHandler) Create(c *gin.Context) {
	var req createToolUsageEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.CreateUsageEvent(c.Request.Context(), toolusage.CreateUsageEventRequest{
		Tool:              req.Tool,
		WorkspaceID:       req.WorkspaceID,
		RepoConfigID:      req.RepoConfigID,
		UserID:            req.UserID,
		ToolSessionID:     req.ToolSessionID,
		ToolEventID:       req.ToolEventID,
		DedupeKey:         req.DedupeKey,
		UsageUnit:         req.UsageUnit,
		RequestCount:      req.RequestCount,
		InputTokens:       req.InputTokens,
		OutputTokens:      req.OutputTokens,
		CachedInputTokens: req.CachedInputTokens,
		ReasoningTokens:   req.ReasoningTokens,
		CreditUsage:       req.CreditUsage,
		ContextUsagePct:   req.ContextUsagePct,
		ObservedStartAt:   req.ObservedStartAt,
		ObservedEndAt:     req.ObservedEndAt,
		RawSourcePath:     req.RawSourcePath,
		RawSourceLocator:  req.RawSourceLocator,
		RawPayload:        req.RawPayload,
	}); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"dedupe_key": req.DedupeKey})
}

func (h *ToolUsageHandler) Bind(c *gin.Context) {
	var req bindToolUsageEventsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	bound, err := h.service.BindUsageEventsToCheckpoint(c.Request.Context(), toolusage.BindUsageEventsRequest{
		WorkspaceID:        req.WorkspaceID,
		CommitCheckpointID: req.CommitCheckpointID,
		CommitCapturedAt:   req.CommitCapturedAt,
		PreviousCapturedAt: req.PreviousCapturedAt,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Success(c, gin.H{"bound_count": bound})
}
