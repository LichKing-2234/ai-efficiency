package handler

import (
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AnalysisHandler handles scan-related HTTP requests.
type AnalysisHandler struct {
	analysisService analysisScanner
	optimizer       optimizerService
	repoService     repoSCMProvider
}

// NewAnalysisHandler creates a new analysis handler.
func NewAnalysisHandler(analysisService analysisScanner, optimizer optimizerService, repoService repoSCMProvider) *AnalysisHandler {
	return &AnalysisHandler{
		analysisService: analysisService,
		optimizer:       optimizer,
		repoService:     repoService,
	}
}

// TriggerScan handles POST /api/v1/repos/:id/scan
func (h *AnalysisHandler) TriggerScan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	result, err := h.analysisService.RunScan(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, result)
}

// ListScans handles GET /api/v1/repos/:id/scans
func (h *AnalysisHandler) ListScans(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	scans, err := h.analysisService.ListScans(c.Request.Context(), id, limit)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, scans)
}

// LatestScan handles GET /api/v1/repos/:id/scans/latest
func (h *AnalysisHandler) LatestScan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	scan, err := h.analysisService.GetLatestScan(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "no scan results found")
		return
	}

	pkg.Success(c, scan)
}

// Optimize handles POST /api/v1/repos/:id/optimize — creates an auto-optimization PR.
func (h *AnalysisHandler) Optimize(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if h.optimizer == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "optimizer not available")
		return
	}

	ctx := c.Request.Context()

	// Get latest scan
	scan, err := h.analysisService.GetLatestScan(ctx, id)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "no scan results found, run a scan first")
		return
	}

	// Get SCM provider for this repo
	scmProvider, rc, err := h.repoService.GetSCMProvider(ctx, id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	result, err := h.optimizer.CreateOptimizationPR(ctx, scmProvider, rc, scan)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "optimization failed: "+err.Error())
		return
	}

	if result == nil {
		pkg.Success(c, gin.H{"message": "no auto-fixable issues found"})
		return
	}

	pkg.Success(c, result)
}

// OptimizePreview handles POST /api/v1/repos/:id/optimize/preview — returns file diffs for review.
func (h *AnalysisHandler) OptimizePreview(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if h.optimizer == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "optimizer not available")
		return
	}

	ctx := c.Request.Context()

	scan, err := h.analysisService.GetLatestScan(ctx, id)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "no scan results found, run a scan first")
		return
	}

	scmProvider, rc, err := h.repoService.GetSCMProvider(ctx, id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	preview, err := h.optimizer.PreviewOptimization(ctx, scmProvider, rc, scan)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "preview failed: "+err.Error())
		return
	}

	if preview == nil {
		pkg.Success(c, gin.H{"files": []interface{}{}, "score": 0, "message": "no auto-fixable issues found"})
		return
	}

	pkg.Success(c, preview)
}

// OptimizeConfirm handles POST /api/v1/repos/:id/optimize/confirm — creates PR from reviewed files.
func (h *AnalysisHandler) OptimizeConfirm(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if h.optimizer == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "optimizer not available")
		return
	}

	var req struct {
		Files map[string]string `json:"files"`
		Score int               `json:"score"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := c.Request.Context()

	scmProvider, rc, err := h.repoService.GetSCMProvider(ctx, id)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get SCM provider: "+err.Error())
		return
	}

	result, err := h.optimizer.ConfirmOptimization(ctx, scmProvider, rc, req.Files, req.Score)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "optimization failed: "+err.Error())
		return
	}

	pkg.Success(c, result)
}
