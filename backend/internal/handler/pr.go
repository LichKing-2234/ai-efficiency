package handler

import (
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
	"github.com/gin-gonic/gin"
)

// PRHandler handles PR record HTTP requests.
type PRHandler struct {
	entClient          *ent.Client
	repoService        repoSCMProvider
	syncService        prSyncer
	attributionService prAttributionSettler
}

// NewPRHandler creates a new PR handler.
func NewPRHandler(entClient *ent.Client, repoService repoSCMProvider, syncService prSyncer, attributionService ...prAttributionSettler) *PRHandler {
	var attrSvc prAttributionSettler
	if len(attributionService) > 0 {
		attrSvc = attributionService[0]
	}
	return &PRHandler{
		entClient:          entClient,
		repoService:        repoService,
		syncService:        syncService,
		attributionService: attrSvc,
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
