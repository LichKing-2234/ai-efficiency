package handler

import (
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/gin-gonic/gin"
)

// RepoHandler handles repo configuration HTTP requests.
type RepoHandler struct {
	repoService *repo.Service
}

// NewRepoHandler creates a new repo handler.
func NewRepoHandler(repoService *repo.Service) *RepoHandler {
	return &RepoHandler{repoService: repoService}
}

// List handles GET /api/v1/repos
func (h *RepoHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	scmProviderID, _ := strconv.Atoi(c.Query("scm_provider_id"))
	status := c.Query("status")
	groupID := c.Query("group_id")

	opts := repo.ListOpts{
		Page:          page,
		PageSize:      pageSize,
		SCMProviderID: scmProviderID,
		Status:        status,
		GroupID:       groupID,
	}

	repos, total, err := h.repoService.List(c.Request.Context(), opts)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list repos")
		return
	}

	pkg.Paged(c, total, page, pageSize, repos)
}

// Create handles POST /api/v1/repos
func (h *RepoHandler) Create(c *gin.Context) {
	var req repo.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	r, err := h.repoService.Create(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Created(c, r)
}

// CreateDirect handles POST /api/v1/repos/direct (skips SCM validation)
func (h *RepoHandler) CreateDirect(c *gin.Context) {
	var req repo.CreateDirectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	r, err := h.repoService.CreateDirect(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Created(c, r)
}

// Get handles GET /api/v1/repos/:id
func (h *RepoHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	r, err := h.repoService.Get(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusNotFound, "repo not found")
		return
	}

	pkg.Success(c, r)
}

// Update handles PUT /api/v1/repos/:id
func (h *RepoHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req repo.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	r, err := h.repoService.Update(c.Request.Context(), id, req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, r)
}

// Delete handles DELETE /api/v1/repos/:id
func (h *RepoHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.repoService.Delete(c.Request.Context(), id); err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, gin.H{"deleted": true})
}

// TriggerScan handles POST /api/v1/repos/:id/scan
func (h *RepoHandler) TriggerScan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.repoService.TriggerScan(c.Request.Context(), id); err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	pkg.Success(c, gin.H{"message": "scan triggered"})
}
