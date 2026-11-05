package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/teamaccount"
)

// TeamAccountHandler exposes department team accounts to their managers and to
// administrators.
type TeamAccountHandler struct {
	service *teamaccount.Service
}

// NewTeamAccountHandler builds the handler. A nil service disables the routes.
func NewTeamAccountHandler(service *teamaccount.Service) *TeamAccountHandler {
	if service == nil {
		return nil
	}
	return &TeamAccountHandler{service: service}
}

func (h *TeamAccountHandler) actor(c *gin.Context) (userID int, isAdmin bool, ok bool) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "authentication required")
		return 0, false, false
	}
	return uc.UserID, uc.Role == "admin", true
}

func (h *TeamAccountHandler) respondError(c *gin.Context, err error) {
	var validation *teamaccount.ValidationError
	var conflict *teamaccount.ConflictError
	var permission *teamaccount.PermissionError
	var missing *teamaccount.NotFoundError
	switch {
	case errors.As(err, &validation):
		pkg.Error(c, http.StatusUnprocessableEntity, validation.Message)
	case errors.As(err, &conflict):
		pkg.Error(c, http.StatusConflict, conflict.Message)
	case errors.As(err, &permission):
		pkg.Error(c, http.StatusForbidden, permission.Message)
	case errors.As(err, &missing):
		pkg.Error(c, http.StatusNotFound, missing.Message)
	default:
		pkg.Error(c, http.StatusBadGateway, err.Error())
	}
}

func accountIDParam(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid team account id")
		return 0, false
	}
	return id, true
}

// List handles GET /api/v1/user/team-accounts.
func (h *TeamAccountHandler) List(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	accounts, err := h.service.List(c.Request.Context(), userID, isAdmin)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": accounts})
}

// Create handles POST /api/v1/user/team-accounts.
func (h *TeamAccountHandler) Create(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	var req struct {
		Name                 string  `json:"name"`
		DepartmentExternalID string  `json:"department_external_id"`
		AdditionalGroupIDs   []int64 `json:"additional_group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	account, err := h.service.Create(c.Request.Context(), teamaccount.CreateInput{
		Name:                 req.Name,
		DepartmentExternalID: req.DepartmentExternalID,
		ActorUserID:          userID,
		IsAdmin:              isAdmin,
		AdditionalGroupIDs:   req.AdditionalGroupIDs,
	})
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, account)
}

// Get handles GET /api/v1/user/team-accounts/:id.
func (h *TeamAccountHandler) Get(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	account, err := h.service.Get(c.Request.Context(), userID, isAdmin, id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, account)
}

// CreateKey handles POST /api/v1/user/team-accounts/:id/keys.
func (h *TeamAccountHandler) CreateKey(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req struct {
		GroupID int64  `json:"relay_group_id"`
		Name    string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	key, secret, err := h.service.CreateKey(c.Request.Context(), userID, isAdmin, id, req.GroupID, req.Name)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"key": key, "secret": secret})
}

// RevokeKey handles POST /api/v1/user/team-accounts/:id/keys/:key_id/revoke.
func (h *TeamAccountHandler) RevokeKey(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	keyID, err := strconv.Atoi(strings.TrimSpace(c.Param("key_id")))
	if err != nil || keyID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid key id")
		return
	}
	if err := h.service.RevokeKey(c.Request.Context(), userID, isAdmin, id, keyID); err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"revoked": true})
}

// RevealCredentials handles POST /api/v1/user/team-accounts/:id/credentials.
func (h *TeamAccountHandler) RevealCredentials(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	credentials, err := h.service.Credentials(c.Request.Context(), userID, isAdmin, id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, credentials)
}

// AddManager handles POST /api/v1/user/team-accounts/:id/managers.
func (h *TeamAccountHandler) AddManager(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req struct {
		UserID int `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.AddManager(c.Request.Context(), userID, isAdmin, id, req.UserID); err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"added": true})
}

// RemoveManager handles DELETE /api/v1/user/team-accounts/:id/managers/:user_id.
func (h *TeamAccountHandler) RemoveManager(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	target, err := strconv.Atoi(strings.TrimSpace(c.Param("user_id")))
	if err != nil || target <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.service.RemoveManager(c.Request.Context(), userID, isAdmin, id, target); err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"removed": true})
}

// Transfer handles POST /api/v1/user/team-accounts/:id/transfer.
func (h *TeamAccountHandler) Transfer(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req struct {
		NewOwnerUserID int `json:"new_owner_user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.Transfer(c.Request.Context(), userID, isAdmin, id, req.NewOwnerUserID); err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"transferred": true})
}

// Usage handles GET /api/v1/user/team-accounts/:id/usage.
func (h *TeamAccountHandler) Usage(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	from, to := usageWindow(c)
	usage, err := h.service.AggregateUsage(c.Request.Context(), userID, isAdmin, id, from, to)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, usage)
}

func usageWindow(c *gin.Context) (time.Time, time.Time) {
	to := time.Now()
	from := to.AddDate(0, 0, -7)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			from = parsed
		} else if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			from = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			to = parsed
		} else if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			to = parsed.Add(24 * time.Hour)
		}
	}
	return from, to
}

// Events handles GET /api/v1/user/team-accounts/:id/events.
func (h *TeamAccountHandler) Events(c *gin.Context) {
	userID, isAdmin, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	events, err := h.service.Events(c.Request.Context(), userID, isAdmin, id, limit)
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"items": events})
}

// AdminCreate handles POST /api/v1/admin/team-accounts.
func (h *TeamAccountHandler) AdminCreate(c *gin.Context) {
	userID, _, ok := h.actor(c)
	if !ok {
		return
	}
	var req struct {
		Name                 string  `json:"name"`
		DepartmentExternalID string  `json:"department_external_id"`
		OwnerUserID          int     `json:"owner_user_id"`
		AdditionalGroupIDs   []int64 `json:"additional_group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	account, err := h.service.Create(c.Request.Context(), teamaccount.CreateInput{
		Name:                 req.Name,
		DepartmentExternalID: req.DepartmentExternalID,
		OwnerUserID:          req.OwnerUserID,
		ActorUserID:          userID,
		IsAdmin:              true,
		AdditionalGroupIDs:   req.AdditionalGroupIDs,
	})
	if err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, account)
}

// AdminSetStatus handles POST /api/v1/admin/team-accounts/:id/status.
func (h *TeamAccountHandler) AdminSetStatus(c *gin.Context) {
	userID, _, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.service.SetStatus(c.Request.Context(), userID, id, req.Status); err != nil {
		h.respondError(c, err)
		return
	}
	pkg.Success(c, gin.H{"status": req.Status})
}
