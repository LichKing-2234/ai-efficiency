package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/gin-gonic/gin"
)

// PRHandler handles PR record HTTP requests.
type PRHandler struct {
	entClient          *ent.Client
	repoService        repoSCMProvider
	syncService        prSyncer
	attributionService prAttributionSettler
	usageRefresher     prUsageRefresher
	usageFreshness     prUsageFreshnessEvaluator
	usagePageFreshness prUsagePageFreshnessEvaluator
}

// NewPRHandler creates a new PR handler.
func NewPRHandler(entClient *ent.Client, repoService repoSCMProvider, syncService prSyncer, attributionService prAttributionSettler, usageRefresher ...prUsageRefresher) *PRHandler {
	var attrSvc prAttributionSettler
	if attributionService != nil {
		attrSvc = attributionService
	}
	var usageSvc prUsageRefresher
	if len(usageRefresher) > 0 {
		usageSvc = usageRefresher[0]
	}
	var freshnessSvc prUsageFreshnessEvaluator
	if evaluator, ok := usageSvc.(prUsageFreshnessEvaluator); ok {
		freshnessSvc = evaluator
	}
	var pageFreshnessSvc prUsagePageFreshnessEvaluator
	if evaluator, ok := usageSvc.(prUsagePageFreshnessEvaluator); ok {
		pageFreshnessSvc = evaluator
	}
	return &PRHandler{
		entClient:          entClient,
		repoService:        repoService,
		syncService:        syncService,
		attributionService: attrSvc,
		usageRefresher:     usageSvc,
		usageFreshness:     freshnessSvc,
		usagePageFreshness: pageFreshnessSvc,
	}
}

type prResponse struct {
	*ent.PrRecord
	UsageStatus          string                    `json:"usage_status"`
	UsageStatusReason    string                    `json:"usage_status_reason"`
	UsageStatusCheckedAt *time.Time                `json:"usage_status_checked_at,omitempty"`
	CommitFreshness      []prusage.CommitFreshness `json:"commit_freshness,omitempty"`
}

type prListSummary struct {
	Total         int `json:"total"`
	WithUsage     int `json:"with_usage"`
	PendingUpload int `json:"pending_upload"`
	NoCheckpoint  int `json:"no_checkpoint"`
	RefreshFailed int `json:"refresh_failed"`
}

func serializePRSyncJob(job *ent.PRSyncJob) gin.H {
	if job == nil {
		return nil
	}
	return gin.H{
		"id":                  job.ID,
		"repo_config_id":      job.RepoConfigID,
		"status":              string(job.Status),
		"phase":               string(job.Phase),
		"current_page":        job.CurrentPage,
		"page_size":           job.PageSize,
		"fetched_prs":         job.FetchedPrs,
		"total_prs":           job.TotalPrs,
		"processed_prs":       job.ProcessedPrs,
		"created_prs":         job.CreatedPrs,
		"changed_prs":         job.ChangedPrs,
		"unchanged_prs":       job.UnchangedPrs,
		"usage_total_prs":     job.UsageTotalPrs,
		"usage_refreshed_prs": job.UsageRefreshedPrs,
		"usage_skipped_prs":   job.UsageSkippedPrs,
		"usage_failed_prs":    job.UsageFailedPrs,
		"last_error":          job.LastError,
	}
}

func buildPRResponseFromFreshness(pr *ent.PrRecord, freshness *prusage.PRFreshness, includeCommits bool) any {
	resp := &prResponse{
		PrRecord:          pr,
		UsageStatus:       string(prusage.UsageStatusUnknown),
		UsageStatusReason: "Usage freshness has not been evaluated.",
	}
	if freshness != nil {
		resp.UsageStatus = string(freshness.Status)
		resp.UsageStatusReason = freshness.Reason
		resp.UsageStatusCheckedAt = &freshness.CheckedAt
		if includeCommits {
			resp.CommitFreshness = freshness.Commits
		}
	}
	return resp
}

func (h *PRHandler) buildPRResponse(ctx context.Context, pr *ent.PrRecord, includeCommits bool) any {
	if h.usageFreshness == nil || pr == nil {
		return buildPRResponseFromFreshness(pr, nil, includeCommits)
	}
	freshness, err := h.usageFreshness.EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		freshness = nil
	}
	return buildPRResponseFromFreshness(pr, freshness, includeCommits)
}

func (h *PRHandler) buildPRListSummary(ctx context.Context, query *ent.PrRecordQuery, total int) (prListSummary, error) {
	summary := prListSummary{Total: total}
	withUsage, err := query.Clone().
		Where(prrecord.Or(
			prrecord.UsageInputTokensGT(0),
			prrecord.UsageOutputTokensGT(0),
			prrecord.UsageCachedInputTokensGT(0),
			prrecord.UsageReasoningTokensGT(0),
			prrecord.UsageCreditUsageGT(0),
			prrecord.UsageRequestCountGT(0),
		)).
		Count(ctx)
	if err != nil {
		return summary, err
	}
	summary.WithUsage = withUsage

	noCheckpoint, err := query.Clone().
		Where(
			prrecord.UsageRefreshedAtIsNil(),
			prrecord.UsageInputTokensEQ(0),
			prrecord.UsageOutputTokensEQ(0),
			prrecord.UsageCachedInputTokensEQ(0),
			prrecord.UsageReasoningTokensEQ(0),
			prrecord.UsageCreditUsageEQ(0),
			prrecord.UsageRequestCountEQ(0),
		).
		Count(ctx)
	if err != nil {
		return summary, err
	}
	summary.NoCheckpoint = noCheckpoint
	return summary, nil
}

// ListByRepo handles GET /api/v1/repos/:id/prs
func (h *PRHandler) ListByRepo(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	query := h.entClient.PrRecord.Query().
		Where(prrecord.HasRepoConfigWith(repoconfig.IDEQ(repoID)))

	if status != "" {
		query.Where(prrecord.StatusEQ(prrecord.Status(status)))
	}

	// Default: open PRs + PRs from last 3 months
	// ?months=N to change range, ?months=0 for all
	months := 3
	if m := c.Query("months"); m != "" {
		if v, err := strconv.Atoi(m); err == nil && v >= 0 {
			months = v
		}
	}
	if months > 0 {
		since := time.Now().AddDate(0, -months, 0)
		query.Where(
			prrecord.Or(
				prrecord.StatusEQ(prrecord.StatusOpen),
				prrecord.CreatedAtGTE(since),
			),
		)
	}

	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to count PRs")
		return
	}

	summary, err := h.buildPRListSummary(c.Request.Context(), query.Clone(), total)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to summarize PRs")
		return
	}

	prs, err := query.
		Order(func(s *sql.Selector) {
			s.OrderExpr(sql.ExprP("CASE status WHEN 'open' THEN 0 WHEN 'merged' THEN 1 ELSE 2 END"))
			s.OrderBy(sql.Desc(prrecord.FieldCreatedAt))
		}).
		Offset(offset).
		Limit(limit).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list PRs")
		return
	}

	var freshnessByPR map[int]*prusage.PRFreshness
	if h.usagePageFreshness != nil {
		evaluated, err := h.usagePageFreshness.EvaluatePRFreshnessPage(c.Request.Context(), repoID, prs)
		if err == nil {
			freshnessByPR = evaluated
		}
	}

	items := make([]any, 0, len(prs))
	for _, pr := range prs {
		items = append(items, buildPRResponseFromFreshness(pr, freshnessByPR[pr.ID], false))
	}

	pkg.Success(c, gin.H{
		"items":   items,
		"summary": summary,
		"total":   total,
	})
}

// Get handles GET /api/v1/prs/:id
func (h *PRHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	pr, err := h.entClient.PrRecord.Query().
		Where(prrecord.IDEQ(id)).
		WithPrCommitUsageSnapshots().
		WithLastAttributionRun().
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get PR")
		return
	}

	pkg.Success(c, h.buildPRResponse(c.Request.Context(), pr, true))
}

// RefreshUsage handles POST /api/v1/prs/:id/refresh-usage.
func (h *PRHandler) RefreshUsage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.usageRefresher == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr usage refresher is not configured")
		return
	}

	pr, err := h.entClient.PrRecord.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to load PR")
		return
	}

	rc, err := pr.QueryRepoConfig().Only(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load PR repo")
		return
	}

	scmProvider, _, err := h.repoService.GetSCMProvider(c.Request.Context(), rc.ID)
	if err != nil {
		if handleRepoBindingError(c, err) {
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	if _, err := h.usageRefresher.RefreshPR(c.Request.Context(), scmProvider, pr); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to refresh PR usage: "+err.Error())
		return
	}

	loaded, err := h.entClient.PrRecord.Query().
		Where(prrecord.IDEQ(id)).
		WithPrCommitUsageSnapshots().
		WithLastAttributionRun().
		Only(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get PR")
		return
	}

	pkg.Success(c, h.buildPRResponse(c.Request.Context(), loaded, true))
}

// SyncPRs handles POST /api/v1/repos/:id/sync-prs
func (h *PRHandler) SyncPRs(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.syncService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr sync service is not configured")
		return
	}

	scmProvider, rc, err := h.repoService.GetSCMProvider(c.Request.Context(), repoID)
	if err != nil {
		if handleRepoBindingError(c, err) {
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	job, reused, err := h.syncService.StartSyncJob(c.Request.Context(), scmProvider, rc)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "sync failed: "+err.Error())
		return
	}
	if !reused {
		go func(jobID int, provider scm.SCMProvider, repoConfig *ent.RepoConfig) {
			_, _ = h.syncService.RunSyncJob(context.Background(), jobID, provider, repoConfig)
		}(job.ID, scmProvider, rc)
	}

	pkg.Success(c, gin.H{
		"job_id": job.ID,
		"status": string(job.Status),
		"phase":  string(job.Phase),
		"reused": reused,
	})
}

// GetLatestSyncJobForRepo handles GET /api/v1/repos/:id/pr-sync-job/latest.
func (h *PRHandler) GetLatestSyncJobForRepo(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.syncService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr sync service is not configured")
		return
	}
	job, err := h.syncService.GetLatestSyncJobForRepo(c.Request.Context(), repoID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, serializePRSyncJob(job))
}

// GetSyncJob handles GET /api/v1/pr-sync-jobs/:id.
func (h *PRHandler) GetSyncJob(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if h.syncService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "pr sync service is not configured")
		return
	}
	job, err := h.syncService.GetSyncJob(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR sync job not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, serializePRSyncJob(job))
}

type settlePRRequest struct {
	TriggeredBy string `json:"triggered_by"`
}

// Settle handles POST /api/v1/prs/:id/settle.
func (h *PRHandler) Settle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req settlePRRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			pkg.Error(c, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if h.attributionService == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "attribution service is not configured")
		return
	}

	pr, err := h.entClient.PrRecord.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to load PR")
		return
	}

	rc, err := pr.QueryRepoConfig().Only(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load PR repo")
		return
	}

	scmProvider, _, err := h.repoService.GetSCMProvider(c.Request.Context(), rc.ID)
	if err != nil {
		if handleRepoBindingError(c, err) {
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	triggeredBy := strings.TrimSpace(req.TriggeredBy)
	if triggeredBy == "" {
		if uc := auth.GetUserContext(c); uc != nil {
			triggeredBy = uc.Username
		}
	}
	if triggeredBy == "" {
		triggeredBy = "system"
	}

	result, err := h.attributionService.Settle(c.Request.Context(), scmProvider, pr, triggeredBy)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to settle PR: "+err.Error())
		return
	}

	pkg.Success(c, result)
}
