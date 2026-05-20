package handler

import (
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// EfficiencyHandler handles efficiency metrics HTTP requests.
type EfficiencyHandler struct {
	entClient  *ent.Client
	aggregator *efficiency.Aggregator
}

// NewEfficiencyHandler creates a new efficiency handler.
func NewEfficiencyHandler(entClient *ent.Client, aggregator *efficiency.Aggregator) *EfficiencyHandler {
	return &EfficiencyHandler{entClient: entClient, aggregator: aggregator}
}

// Dashboard handles GET /api/v1/efficiency/dashboard
func (h *EfficiencyHandler) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()

	totalRepos, _ := h.entClient.RepoConfig.Query().Count(ctx)
	workspaceIDs, _ := h.entClient.ToolUsageEvent.Query().
		Unique(true).
		Select("workspace_id").
		Strings(ctx)
	trackedWorkflows := len(workspaceIDs)

	// Count AI PRs
	aiPRs, _ := h.entClient.PrRecord.Query().
		Where(prrecord.AiLabelEQ(prrecord.AiLabelAiViaSub2api)).
		Count(ctx)

	pkg.Success(c, gin.H{
		"total_repos":       totalRepos,
		"tracked_workflows": trackedWorkflows,
		"total_ai_prs":      aiPRs,
	})
}

// Aggregate handles POST /api/v1/efficiency/aggregate — triggers metric aggregation.
func (h *EfficiencyHandler) Aggregate(c *gin.Context) {
	if h.aggregator == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "aggregator not available")
		return
	}

	periodType := c.DefaultQuery("period", "daily")

	// Optional repo_id to aggregate a single repo
	if idStr := c.Query("repo_id"); idStr != "" {
		repoID, err := strconv.Atoi(idStr)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "invalid repo_id")
			return
		}
		if err := h.aggregator.AggregateForRepo(c.Request.Context(), repoID, periodType, efficiency.ComputePeriodStart(periodType)); err != nil {
			pkg.Error(c, http.StatusInternalServerError, "aggregation failed: "+err.Error())
			return
		}
		pkg.Success(c, gin.H{"status": "ok", "repo_id": repoID, "period": periodType})
		return
	}

	if err := h.aggregator.AggregateAll(c.Request.Context(), periodType); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "aggregation failed: "+err.Error())
		return
	}
	pkg.Success(c, gin.H{"status": "ok", "period": periodType})
}
