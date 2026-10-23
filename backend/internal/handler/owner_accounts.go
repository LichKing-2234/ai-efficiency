package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/owneraccount"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// OwnerAccountsHandler is the thin HTTP layer of the owner-account console. Every
// decision lives in the owneraccount service; these methods only parse input,
// choose an HTTP status and write the platform envelope.
type OwnerAccountsHandler struct {
	service *owneraccount.Service
}

// NewOwnerAccountsHandler builds the owner-account HTTP handler.
func NewOwnerAccountsHandler(service *owneraccount.Service) *OwnerAccountsHandler {
	return &OwnerAccountsHandler{service: service}
}

type ownerAccountResetRequest struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason"`
}

type ownerAccountInvitationRequest struct {
	Email     string `json:"email"`
	ProgramID string `json:"program_id"`
	Consent   bool   `json:"consent"`
	Reason    string `json:"reason"`
}

type ownerAccountUsageRequest struct {
	AccountIDs []int64 `json:"account_ids"`
	Force      bool    `json:"force"`
}

// Scope handles GET /api/v1/user/owner-accounts/scope.
func (h *OwnerAccountsHandler) Scope(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	response, err := h.service.Scope(c.Request.Context(), actor)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// List handles GET /api/v1/user/owner-accounts.
func (h *OwnerAccountsHandler) List(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	page, pageSize, ok := ownerAccountPagination(c)
	if !ok {
		return
	}
	response, err := h.service.List(c.Request.Context(), actor, page, pageSize)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// Usage handles POST /api/v1/user/owner-accounts/usage.
func (h *OwnerAccountsHandler) Usage(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	var request ownerAccountUsageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOwnerAccountRequestError(c)
		return
	}
	response, err := h.service.Usage(c.Request.Context(), actor, request.AccountIDs, request.Force)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// RefreshUsage handles POST /api/v1/user/owner-accounts/:id/usage/refresh.
func (h *OwnerAccountsHandler) RefreshUsage(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	response, err := h.service.RefreshUsage(c.Request.Context(), actor, accountID)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// RefreshQuota handles POST /api/v1/user/owner-accounts/:id/quota/refresh.
func (h *OwnerAccountsHandler) RefreshQuota(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	response, err := h.service.RefreshQuota(c.Request.Context(), actor, accountID)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// RefreshReferral handles POST /api/v1/user/owner-accounts/:id/referral/refresh.
func (h *OwnerAccountsHandler) RefreshReferral(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	response, err := h.service.RefreshReferral(c.Request.Context(), actor, accountID)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// ResetCredit handles POST /api/v1/user/owner-accounts/:id/reset-credit.
func (h *OwnerAccountsHandler) ResetCredit(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	var request ownerAccountResetRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOwnerAccountRequestError(c)
		return
	}
	accepted, err := h.service.ResetCredit(c.Request.Context(), owneraccount.ResetCreditInput{
		ActorUserID: actor,
		AccountID:   accountID,
		Confirm:     request.Confirm,
		Reason:      request.Reason,
	})
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Accepted(c, accepted)
}

// Invite handles POST /api/v1/user/owner-accounts/:id/invitations.
func (h *OwnerAccountsHandler) Invite(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	var request ownerAccountInvitationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeOwnerAccountRequestError(c)
		return
	}
	accepted, err := h.service.Invite(c.Request.Context(), owneraccount.InvitationInput{
		ActorUserID: actor,
		AccountID:   accountID,
		Email:       request.Email,
		ProgramID:   request.ProgramID,
		Consent:     request.Consent,
		Reason:      request.Reason,
	})
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Accepted(c, accepted)
}

// Operation handles GET /api/v1/user/owner-accounts/operations/:operation_id.
func (h *OwnerAccountsHandler) Operation(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	operationID, ok := ownerAccountParamID(c, "operation_id", owneraccount.CodeOperationNotFound, "operation not found")
	if !ok {
		return
	}
	response, err := h.service.Operation(c.Request.Context(), actor, int(operationID))
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// ListOperations handles GET /api/v1/user/owner-accounts/:id/operations.
func (h *OwnerAccountsHandler) ListOperations(c *gin.Context) {
	actor, ok := ownerAccountActor(c)
	if !ok {
		return
	}
	accountID, ok := ownerAccountParamID(c, "id", owneraccount.CodeAccountNotFound, "account not found")
	if !ok {
		return
	}
	page, pageSize, ok := ownerAccountPagination(c)
	if !ok {
		return
	}
	response, err := h.service.ListOperations(c.Request.Context(), actor, accountID, page, pageSize)
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// AdminListOperations handles the read-only support surface
// GET /api/v1/admin/owner-operations. There is deliberately no admin mutation.
func (h *OwnerAccountsHandler) AdminListOperations(c *gin.Context) {
	page, pageSize, ok := ownerAccountPagination(c)
	if !ok {
		return
	}
	accountID := int64(0)
	if raw := c.Query("account_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			pkg.ErrorWithDetails(c, http.StatusBadRequest, "invalid filter: account_id", gin.H{
				"error_code": owneraccount.CodeInvalidFilter,
			})
			return
		}
		accountID = parsed
	}
	response, err := h.service.AdminListOperations(c.Request.Context(), owneraccount.AdminListParams{
		Page:      page,
		PageSize:  pageSize,
		Status:    c.Query("status"),
		Action:    c.Query("action"),
		AccountID: accountID,
	})
	if err != nil {
		writeOwnerAccountError(c, err)
		return
	}
	pkg.Success(c, response)
}

// ownerAccountActor reads the authenticated local user id. Authorization is
// already enforced by the route group.
func ownerAccountActor(c *gin.Context) (int, bool) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return 0, false
	}
	return uc.UserID, true
}

// ownerAccountPagination parses optional page/page_size. Invalid integers are a
// 400; range and allowed page sizes are enforced by the service.
func ownerAccountPagination(c *gin.Context) (int, int, bool) {
	page := 0
	pageSize := 0
	if raw := c.Query("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeOwnerAccountInvalidPagination(c)
			return 0, 0, false
		}
		page = parsed
	}
	if raw := c.Query("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeOwnerAccountInvalidPagination(c)
			return 0, 0, false
		}
		pageSize = parsed
	}
	return page, pageSize, true
}

// ownerAccountParamID parses a positive numeric path parameter. A malformed id is
// indistinguishable from a missing resource, so it returns the same not-found
// shape the service uses.
func ownerAccountParamID(c *gin.Context, name, notFoundCode, notFoundMessage string) (int64, bool) {
	parsed, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || parsed <= 0 {
		pkg.ErrorWithDetails(c, http.StatusNotFound, notFoundMessage, gin.H{
			"error_code": notFoundCode,
		})
		return 0, false
	}
	return parsed, true
}

func writeOwnerAccountInvalidPagination(c *gin.Context) {
	pkg.ErrorWithDetails(c, http.StatusBadRequest, "page and page_size are invalid", gin.H{
		"error_code": owneraccount.CodeInvalidPagination,
	})
}

func writeOwnerAccountRequestError(c *gin.Context) {
	pkg.ErrorWithDetails(c, http.StatusBadRequest, "invalid request body", gin.H{
		"error_code": owneraccount.CodeInvalidRequest,
	})
}

// writeOwnerAccountError maps a classified service error onto the platform
// envelope. Upstream error text is never forwarded; an unexpected internal error
// becomes a generic message.
func writeOwnerAccountError(c *gin.Context, err error) {
	var classified *owneraccount.Error
	if errors.As(err, &classified) {
		details := gin.H{"error_code": classified.Code}
		for key, value := range classified.Details {
			details[key] = value
		}
		pkg.ErrorWithDetails(c, classified.Status, classified.Message, details)
		return
	}
	pkg.ErrorWithDetails(c, http.StatusInternalServerError, "owner account request failed", gin.H{
		"error_code": owneraccount.CodeInternalError,
	})
}
