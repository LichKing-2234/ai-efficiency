package handler

import (
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/efficiencymetric"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
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
	activeSessions, _ := h.entClient.Session.Query().
		Where(session.StatusEQ(session.StatusActive)).
		Count(ctx)

	// Compute average AI score across all repos
	repos, _ := h.entClient.RepoConfig.Query().All(ctx)
	var totalScore int
	for _, r := range repos {
		totalScore += r.AiScore
	}
	avgScore := 0
	if len(repos) > 0 {
		avgScore = totalScore / len(repos)
	}

	// Count AI PRs
	aiPRs, _ := h.entClient.PrRecord.Query().
		Where(prrecord.AiLabelEQ(prrecord.AiLabelAiViaSub2api)).
		Count(ctx)

	pkg.Success(c, gin.H{
		"total_repos":     totalRepos,
		"active_sessions": activeSessions,
		"avg_ai_score":    avgScore,
		"total_ai_prs":    aiPRs,
	})
}

// RepoMetrics handles GET /api/v1/efficiency/repos/:id/metrics
func (h *EfficiencyHandler) RepoMetrics(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	periodType := c.DefaultQuery("period", "daily")

	metrics, err := h.entClient.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(repoID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodType(periodType)),
		).
		Order(ent.Desc(efficiencymetric.FieldPeriodStart)).
		Limit(30).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get metrics")
		return
	}

	pkg.Success(c, metrics)
}

// Trend handles GET /api/v1/efficiency/repos/:id/trend
func (h *EfficiencyHandler) Trend(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	periodType := c.DefaultQuery("period", "weekly")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "12"))

	metrics, err := h.entClient.EfficiencyMetric.Query().
		Where(
			efficiencymetric.HasRepoConfigWith(repoconfig.IDEQ(repoID)),
			efficiencymetric.PeriodTypeEQ(efficiencymetric.PeriodType(periodType)),
		).
		Order(ent.Asc(efficiencymetric.FieldPeriodStart)).
		Limit(limit).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get trend")
		return
	}

	pkg.Success(c, metrics)
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
