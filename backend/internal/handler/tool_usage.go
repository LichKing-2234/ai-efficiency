package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
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
	RepoConfigID      int            `json:"repo_config_id"`
	Tool              string         `json:"tool" binding:"required"`
	WorkspaceID       string         `json:"workspace_id" binding:"required"`
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

type createToolUsageEventsBatchRequest struct {
	Events []createToolUsageEventRequest `json:"events" binding:"required,min=1,dive"`
}

const maxToolUsageBatchSize = 100

func (h *ToolUsageHandler) Create(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createToolUsageEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.CreateUsageEvent(c.Request.Context(), uc.UserID, toolusage.CreateUsageEventRequest{
		RepoConfigID:      req.RepoConfigID,
		Tool:              req.Tool,
		WorkspaceID:       req.WorkspaceID,
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
		if errors.Is(err, toolusage.ErrUsageEventForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"dedupe_key": req.DedupeKey})
}

func (h *ToolUsageHandler) CreateBatch(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createToolUsageEventsBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Events) == 0 {
		pkg.Error(c, http.StatusBadRequest, "events are required")
		return
	}
	if len(req.Events) > maxToolUsageBatchSize {
		pkg.Error(c, http.StatusBadRequest, "events exceeds max batch size")
		return
	}

	items := make([]toolusage.CreateUsageEventRequest, 0, len(req.Events))
	for _, item := range req.Events {
		items = append(items, toolusage.CreateUsageEventRequest{
			RepoConfigID:      item.RepoConfigID,
			Tool:              item.Tool,
			WorkspaceID:       item.WorkspaceID,
			ToolSessionID:     item.ToolSessionID,
			ToolEventID:       item.ToolEventID,
			DedupeKey:         item.DedupeKey,
			UsageUnit:         item.UsageUnit,
			RequestCount:      item.RequestCount,
			InputTokens:       item.InputTokens,
			OutputTokens:      item.OutputTokens,
			CachedInputTokens: item.CachedInputTokens,
			ReasoningTokens:   item.ReasoningTokens,
			CreditUsage:       item.CreditUsage,
			ContextUsagePct:   item.ContextUsagePct,
			ObservedStartAt:   item.ObservedStartAt,
			ObservedEndAt:     item.ObservedEndAt,
			RawSourcePath:     item.RawSourcePath,
			RawSourceLocator:  item.RawSourceLocator,
			RawPayload:        item.RawPayload,
		})
	}

	result, err := h.service.CreateUsageEvents(c.Request.Context(), uc.UserID, items)
	if err != nil {
		if errors.Is(err, toolusage.ErrUsageEventForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{
		"accepted":   result.Accepted,
		"created":    result.Created,
		"duplicates": result.Duplicates,
	})
}
