package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type DirectoryAdminService interface {
	ListSources(ctx context.Context) ([]*ent.DirectorySource, error)
	CreateSource(ctx context.Context, input directorysync.SourceInput) (*ent.DirectorySource, error)
	UpdateSource(ctx context.Context, id int, input directorysync.SourceInput) (*ent.DirectorySource, error)
	DeleteSource(ctx context.Context, id int) error
	ValidateSource(ctx context.Context, sourceID int) ([]directorysync.ValidationIssue, error)
	RunSource(ctx context.Context, sourceID int, mode, trigger string) (*ent.DirectorySyncRun, error)
	ExecuteRun(ctx context.Context, runID int) (*ent.DirectorySyncRun, error)
	GetRun(ctx context.Context, runID int) (*ent.DirectorySyncRun, error)
	ListRuns(ctx context.Context, sourceID int) ([]*ent.DirectorySyncRun, error)
	ListDepartments(ctx context.Context, sourceID int, q string) ([]*ent.DirectoryDepartment, error)
	ListMembers(ctx context.Context, sourceID int, q string) ([]*ent.DirectoryMember, error)
	ListOffboardingCandidates(ctx context.Context, sourceID int, q string) ([]directorysync.OffboardingCandidate, error)
	DisableRelayUserForCandidate(ctx context.Context, req directorysync.DisableCandidateRequest) (*ent.DirectoryOffboardingAction, error)
}

type DirectoryHandler struct {
	service DirectoryAdminService
}

func NewDirectoryHandler(service DirectoryAdminService) *DirectoryHandler {
	return &DirectoryHandler{service: service}
}

type directorySourceRequest struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	Scope            string `json:"scope"`
	Enabled          bool   `json:"enabled"`
	DSL              string `json:"dsl"`
	ScheduleEnabled  bool   `json:"schedule_enabled"`
	ScheduleInterval string `json:"schedule_interval"`
	ScheduleTimezone string `json:"schedule_timezone"`
}

type directoryRunRequest struct {
	Mode string `json:"mode"`
}

type disableRelayUserRequest struct {
	SourceID     int    `json:"source_id"`
	ConfirmEmail string `json:"confirm_email"`
	Reason       string `json:"reason"`
}

func (h *DirectoryHandler) ListSources(c *gin.Context) {
	items, err := h.service.ListSources(c.Request.Context())
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *DirectoryHandler) CreateSource(c *gin.Context) {
	var req directorySourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	source, err := h.service.CreateSource(c.Request.Context(), req.toInput())
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Created(c, source)
}

func (h *DirectoryHandler) GetSource(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	sources, err := h.service.ListSources(c.Request.Context())
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	for _, source := range sources {
		if source.ID == id {
			pkg.Success(c, source)
			return
		}
	}
	pkg.Error(c, http.StatusNotFound, "directory source not found")
}

func (h *DirectoryHandler) UpdateSource(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	var req directorySourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	source, err := h.service.UpdateSource(c.Request.Context(), id, req.toInput())
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, source)
}

func (h *DirectoryHandler) DeleteSource(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteSource(c.Request.Context(), id); err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"deleted": true})
}

func (h *DirectoryHandler) ValidateSource(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	issues, err := h.service.ValidateSource(c.Request.Context(), id)
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"valid": len(issues) == 0, "issues": issues})
}

func (h *DirectoryHandler) PreviewSource(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	run, err := h.service.RunSource(c.Request.Context(), id, "preview", "manual")
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	h.executeRunAsync(run.ID)
	pkg.Created(c, run)
}

func (h *DirectoryHandler) StartRun(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	var req directoryRunRequest
	if c.Request.Body != nil {
		_ = c.ShouldBindJSON(&req)
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "apply"
	}
	run, err := h.service.RunSource(c.Request.Context(), id, mode, "manual")
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	h.executeRunAsync(run.ID)
	pkg.Created(c, run)
}

func (h *DirectoryHandler) executeRunAsync(runID int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, _ = h.service.ExecuteRun(ctx, runID)
	}()
}

func (h *DirectoryHandler) ListRuns(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	runs, err := h.service.ListRuns(c.Request.Context(), id)
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": runs})
}

func (h *DirectoryHandler) GetRun(c *gin.Context) {
	id, ok := directoryIDParam(c, "id")
	if !ok {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, run)
}

func (h *DirectoryHandler) ListDepartments(c *gin.Context) {
	sourceID, ok := directoryQueryID(c, "source_id")
	if !ok {
		return
	}
	items, err := h.service.ListDepartments(c.Request.Context(), sourceID, c.Query("q"))
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *DirectoryHandler) ListMembers(c *gin.Context) {
	sourceID, ok := directoryQueryID(c, "source_id")
	if !ok {
		return
	}
	items, err := h.service.ListMembers(c.Request.Context(), sourceID, c.Query("q"))
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *DirectoryHandler) ListOffboardingCandidates(c *gin.Context) {
	sourceID, ok := directoryQueryID(c, "source_id")
	if !ok {
		return
	}
	items, err := h.service.ListOffboardingCandidates(c.Request.Context(), sourceID, c.Query("q"))
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *DirectoryHandler) DisableRelayUser(c *gin.Context) {
	userID, ok := directoryIDParam(c, "user_id")
	if !ok {
		return
	}
	var req disableRelayUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request")
		return
	}
	if req.SourceID <= 0 || strings.TrimSpace(req.ConfirmEmail) == "" {
		pkg.Error(c, http.StatusBadRequest, "source_id and confirm_email are required")
		return
	}
	performedBy := 0
	if uc := auth.GetUserContext(c); uc != nil {
		performedBy = uc.UserID
	}
	action, err := h.service.DisableRelayUserForCandidate(c.Request.Context(), directorysync.DisableCandidateRequest{
		SourceID:          req.SourceID,
		UserID:            userID,
		ConfirmEmail:      req.ConfirmEmail,
		Reason:            req.Reason,
		PerformedByUserID: performedBy,
	})
	if err != nil {
		writeDirectoryError(c, err)
		return
	}
	pkg.Success(c, action)
}

func (r directorySourceRequest) toInput() directorysync.SourceInput {
	return directorysync.SourceInput{
		Name:             r.Name,
		Description:      r.Description,
		Scope:            r.Scope,
		Enabled:          r.Enabled,
		DSL:              r.DSL,
		ScheduleEnabled:  r.ScheduleEnabled,
		ScheduleInterval: r.ScheduleInterval,
		ScheduleTimezone: r.ScheduleTimezone,
	}
}

func directoryIDParam(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

func directoryQueryID(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Query(name))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, name+" is required")
		return 0, false
	}
	return id, true
}

func writeDirectoryError(c *gin.Context, err error) {
	var validation *directorysync.ValidationError
	var conflict *directorysync.ConflictError
	var upstream *directorysync.UpstreamError
	switch {
	case errors.As(err, &validation):
		pkg.Error(c, http.StatusUnprocessableEntity, validation.Error())
	case errors.As(err, &conflict):
		pkg.Error(c, http.StatusConflict, conflict.Error())
	case errors.As(err, &upstream):
		pkg.Error(c, http.StatusBadGateway, upstream.Error())
	case ent.IsNotFound(err):
		pkg.Error(c, http.StatusNotFound, "not found")
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}

func RegisterDirectoryRoutes(group *gin.RouterGroup, handler *DirectoryHandler) {
	group.GET("/sources", handler.ListSources)
	group.POST("/sources", handler.CreateSource)
	group.GET("/sources/:id", handler.GetSource)
	group.PUT("/sources/:id", handler.UpdateSource)
	group.DELETE("/sources/:id", handler.DeleteSource)
	group.POST("/sources/:id/validate", handler.ValidateSource)
	group.POST("/sources/:id/preview", handler.PreviewSource)
	group.POST("/sources/:id/runs", handler.StartRun)
	group.GET("/sources/:id/runs", handler.ListRuns)
	group.GET("/runs/:id", handler.GetRun)
	group.GET("/departments", handler.ListDepartments)
	group.GET("/members", handler.ListMembers)
	group.GET("/offboarding-candidates", handler.ListOffboardingCandidates)
	group.POST("/offboarding-candidates/:user_id/disable-relay-user", handler.DisableRelayUser)
}
