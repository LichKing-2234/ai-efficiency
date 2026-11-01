package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/accountquota"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AccountQuotaHandler is the thin HTTP layer of the admin quota sweep: it parses
// input, chooses an HTTP status and writes the platform envelope. Every decision
// lives in the accountquota service.
type AccountQuotaHandler struct {
	service accountQuotaService
}

// NewAccountQuotaHandler builds the admin quota sweep HTTP handler.
func NewAccountQuotaHandler(service accountQuotaService) *AccountQuotaHandler {
	return &AccountQuotaHandler{service: service}
}

type accountQuotaSweepRequest struct {
	ProviderID       int     `json:"provider_id"`
	Platform         string  `json:"platform"`
	AccountIDs       []int64 `json:"account_ids"`
	IncludeAutoReset bool    `json:"include_auto_reset"`
}

// List handles GET /api/v1/admin/account-quotas.
func (h *AccountQuotaHandler) List(c *gin.Context) {
	providerID, ok := accountQuotaProviderID(c)
	if !ok {
		return
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if platform == "" {
		platform = accountquota.PlatformOpenAI
	}
	page, err := h.service.List(c.Request.Context(), providerID, platform)
	if err != nil {
		writeAccountQuotaError(c, err)
		return
	}
	pkg.Success(c, page)
}

// StartSweep handles POST /api/v1/admin/account-quotas/sweeps.
func (h *AccountQuotaHandler) StartSweep(c *gin.Context) {
	var request accountQuotaSweepRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		pkg.ErrorWithDetails(c, http.StatusBadRequest, "invalid request body", gin.H{
			"error_code": accountquota.CodeInvalidRequest,
		})
		return
	}
	platform := strings.TrimSpace(request.Platform)
	if platform == "" {
		platform = accountquota.PlatformOpenAI
	}
	sweep, err := h.service.StartSweep(c.Request.Context(), accountquota.StartSweepRequest{
		ProviderID:       request.ProviderID,
		Platform:         platform,
		AccountIDs:       request.AccountIDs,
		IncludeAutoReset: request.IncludeAutoReset,
	})
	if err != nil {
		writeAccountQuotaError(c, err)
		return
	}
	pkg.Accepted(c, sweep)
}

// GetSweep handles GET /api/v1/admin/account-quotas/sweeps/:id.
func (h *AccountQuotaHandler) GetSweep(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.ErrorWithDetails(c, http.StatusBadRequest, "invalid sweep ID", gin.H{
			"error_code": accountquota.CodeInvalidRequest,
		})
		return
	}
	sweep, err := h.service.GetSweep(c.Request.Context(), id)
	if err != nil {
		writeAccountQuotaError(c, err)
		return
	}
	pkg.Success(c, sweep)
}

// LatestSweep handles GET /api/v1/admin/account-quotas/sweeps/latest.
func (h *AccountQuotaHandler) LatestSweep(c *gin.Context) {
	providerID, ok := accountQuotaProviderID(c)
	if !ok {
		return
	}
	sweep, err := h.service.LatestSweep(c.Request.Context(), providerID)
	if err != nil {
		writeAccountQuotaError(c, err)
		return
	}
	// A provider that has never been swept has no resource; the envelope omits
	// data and the page renders the empty state.
	if sweep == nil {
		pkg.Success(c, nil)
		return
	}
	pkg.Success(c, sweep)
}

func accountQuotaProviderID(c *gin.Context) (int, bool) {
	providerID, err := strconv.Atoi(strings.TrimSpace(c.Query("provider_id")))
	if err != nil || providerID <= 0 {
		pkg.ErrorWithDetails(c, http.StatusBadRequest, "provider_id is required", gin.H{
			"error_code": accountquota.CodeInvalidRequest,
		})
		return 0, false
	}
	return providerID, true
}

func writeAccountQuotaError(c *gin.Context, err error) {
	var classified *accountquota.Error
	if errors.As(err, &classified) {
		details := gin.H{"error_code": classified.Code}
		for key, value := range classified.Details {
			details[key] = value
		}
		pkg.ErrorWithDetails(c, classified.Status, classified.Message, details)
		return
	}
	pkg.ErrorWithDetails(c, http.StatusInternalServerError, "account quota request failed", gin.H{
		"error_code": accountquota.CodeInternalError,
	})
}
