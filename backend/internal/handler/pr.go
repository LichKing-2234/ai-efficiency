package handler

import (
	"net/http"
	"strconv"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// PRHandler handles PR record HTTP requests.
type PRHandler struct {
	entClient   *ent.Client
	repoService repoSCMProvider
	syncService prSyncer
}

// NewPRHandler creates a new PR handler.
func NewPRHandler(entClient *ent.Client, repoService repoSCMProvider, syncService prSyncer) *PRHandler {
	return &PRHandler{
		entClient:   entClient,
		repoService: repoService,
		syncService: syncService,
	}
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

	pkg.Success(c, gin.H{
		"items": prs,
		"total": total,
	})
}

// Get handles GET /api/v1/prs/:id
func (h *PRHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	pr, err := h.entClient.PrRecord.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "PR not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get PR")
		return
	}

	pkg.Success(c, pr)
}

// SyncPRs handles POST /api/v1/repos/:id/sync-prs
func (h *PRHandler) SyncPRs(c *gin.Context) {
	repoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	scmProvider, rc, err := h.repoService.GetSCMProvider(c.Request.Context(), repoID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	result, err := h.syncService.Sync(c.Request.Context(), scmProvider, rc)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "sync failed: "+err.Error())
		return
	}

	pkg.Success(c, result)
}
