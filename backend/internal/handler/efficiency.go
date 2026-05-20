package handler

import (
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// EfficiencyHandler handles efficiency metrics HTTP requests.
type EfficiencyHandler struct {
	entClient *ent.Client
}

// NewEfficiencyHandler creates a new efficiency handler.
func NewEfficiencyHandler(entClient *ent.Client) *EfficiencyHandler {
	return &EfficiencyHandler{entClient: entClient}
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
