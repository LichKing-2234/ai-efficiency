package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/gin-gonic/gin"
)

type quotaResetService interface {
	Options(context.Context, int) (*quotareset.OptionsResponse, error)
	CreateRequest(context.Context, quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error)
	Cancel(context.Context, int, int) (*ent.QuotaResetRequest, error)
	Approve(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	Reject(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	RetryReset(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	ListMine(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error)
	ListApprovals(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error)
	ListAdmin(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error)
	ListApproverCandidates(context.Context, quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error)
	ListApproverConfigs(context.Context) (*quotareset.ApproverConfigListResponse, error)
	SaveApproverConfigs(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error)
	ListApprovalChains(context.Context) (*quotareset.ApprovalChainListResponse, error)
	SaveApprovalChains(context.Context, quotareset.SaveApprovalChainsInput) (*quotareset.ApprovalChainListResponse, error)
	ListApprovalChainOptions(context.Context) (*quotareset.ApprovalChainOptionsResponse, error)
	GetRequestSummary(context.Context, int, int, bool) (*quotareset.RequestSummary, error)
	GetNotificationSettings(context.Context) (*quotareset.NotificationSettings, error)
	UpdateNotificationSettings(context.Context, quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error)
	TestNotificationSettings(context.Context, int) (*quotareset.NotificationTestResult, error)
}

type QuotaResetHandler struct {
	service quotaResetService
}

func NewQuotaResetHandler(service quotaResetService) *QuotaResetHandler {
	return &QuotaResetHandler{service: service}
}

type quotaResetCreateRequest struct {
	GroupID string `json:"group_id"`
	Reason  string `json:"reason"`
}

type quotaResetDecisionRequest struct {
	RequestNodeID  int    `json:"request_node_id"`
	Reason         string `json:"reason"`
	DecisionReason string `json:"decision_reason"`
}

type quotaResetSaveApproverConfigsRequest struct {
	Items *[]quotareset.ApproverConfigInput `json:"items"`
	Mode  string                            `json:"mode"`
}

type quotaResetSaveApprovalChainsRequest struct {
	Items *[]quotareset.ApprovalChainInput `json:"items"`
}

type quotaResetNotificationSettingsRequest struct {
	Enabled      bool    `json:"enabled"`
	ChannelType  string  `json:"channel_type"`
	URL          *string `json:"url"`
	AuthType     string  `json:"auth_type"`
	CredentialID *int    `json:"credential_id"`
}

func (h *QuotaResetHandler) Options(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	resp, err := h.service.Options(c.Request.Context(), uc.UserID)
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) CreateRequest(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	var req quotaResetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.service.CreateRequest(c.Request.Context(), quotareset.CreateRequestInput{
		RequesterUserID: uc.UserID,
		GroupID:         req.GroupID,
		Reason:          req.Reason,
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	h.respondWithRequestSummary(c, created.ID, uc.UserID, false)
}

func (h *QuotaResetHandler) ListMine(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	resp, err := h.service.ListMine(c.Request.Context(), uc.UserID, parseQuotaResetListParams(c))
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) Cancel(c *gin.Context) {
	uc, requestID, ok := quotaResetActorAndRequestID(c)
	if !ok {
		return
	}
	resp, err := h.service.Cancel(c.Request.Context(), uc.UserID, requestID)
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	h.respondWithRequestSummary(c, resp.ID, uc.UserID, false)
}

func (h *QuotaResetHandler) ListApprovals(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	resp, err := h.service.ListApprovals(c.Request.Context(), uc.UserID, parseQuotaResetListParams(c))
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) Approve(c *gin.Context) {
	h.decide(c, false, true)
}

func (h *QuotaResetHandler) Reject(c *gin.Context) {
	h.decide(c, false, false)
}

func (h *QuotaResetHandler) RetryReset(c *gin.Context) {
	h.retryReset(c, false)
}

func (h *QuotaResetHandler) ListAdmin(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	resp, err := h.service.ListAdmin(c.Request.Context(), uc.UserID, parseQuotaResetListParams(c))
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) AdminApprove(c *gin.Context) {
	h.decide(c, true, true)
}

func (h *QuotaResetHandler) AdminReject(c *gin.Context) {
	h.decide(c, true, false)
}

func (h *QuotaResetHandler) AdminRetryReset(c *gin.Context) {
	h.retryReset(c, true)
}

func (h *QuotaResetHandler) ListApproverConfigs(c *gin.Context) {
	resp, err := h.service.ListApproverConfigs(c.Request.Context())
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) ListApproverCandidates(c *gin.Context) {
	resp, err := h.service.ListApproverCandidates(c.Request.Context(), quotareset.ApproverCandidateParams{
		SourceID: parseOptionalInt(c.Query("source_id")),
		Query:    strings.TrimSpace(c.Query("q")),
		Page:     parseOptionalInt(c.Query("page")),
		PageSize: parseOptionalInt(c.Query("page_size")),
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) ListApprovalChains(c *gin.Context) {
	resp, err := h.service.ListApprovalChains(c.Request.Context())
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) SaveApprovalChains(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	var req quotaResetSaveApprovalChainsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Items == nil {
		pkg.Error(c, http.StatusBadRequest, "items is required")
		return
	}
	resp, err := h.service.SaveApprovalChains(c.Request.Context(), quotareset.SaveApprovalChainsInput{
		ActorUserID: uc.UserID,
		Items:       *req.Items,
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) ListApprovalChainOptions(c *gin.Context) {
	resp, err := h.service.ListApprovalChainOptions(c.Request.Context())
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) SaveApproverConfigs(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	var req quotaResetSaveApproverConfigsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Items == nil {
		pkg.Error(c, http.StatusBadRequest, "items is required")
		return
	}
	resp, err := h.service.SaveApproverConfigs(c.Request.Context(), quotareset.SaveApproverConfigsInput{
		ActorUserID: uc.UserID,
		Mode:        strings.TrimSpace(req.Mode),
		Items:       *req.Items,
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) GetNotificationSettings(c *gin.Context) {
	resp, err := h.service.GetNotificationSettings(c.Request.Context())
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) UpdateNotificationSettings(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	var req quotaResetNotificationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.service.UpdateNotificationSettings(c.Request.Context(), quotareset.UpdateNotificationSettingsInput{
		ActorUserID:  uc.UserID,
		Enabled:      req.Enabled,
		ChannelType:  req.ChannelType,
		URL:          req.URL,
		AuthType:     req.AuthType,
		CredentialID: req.CredentialID,
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *QuotaResetHandler) TestNotificationSettings(c *gin.Context) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return
	}
	result, err := h.service.TestNotificationSettings(c.Request.Context(), uc.UserID)
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, result)
}

func (h *QuotaResetHandler) decide(c *gin.Context, admin bool, approve bool) {
	uc, requestID, ok := quotaResetActorAndRequestID(c)
	if !ok {
		return
	}
	var req quotaResetDecisionRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	input := quotareset.DecisionInput{
		ActorUserID:    uc.UserID,
		RequestID:      requestID,
		RequestNodeID:  req.RequestNodeID,
		DecisionReason: quotaResetDecisionReason(req),
		Admin:          admin,
	}
	var (
		resp *ent.QuotaResetRequest
		err  error
	)
	if approve {
		resp, err = h.service.Approve(c.Request.Context(), input)
	} else {
		resp, err = h.service.Reject(c.Request.Context(), input)
	}
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	h.respondWithRequestSummary(c, resp.ID, uc.UserID, admin)
}

func (h *QuotaResetHandler) retryReset(c *gin.Context, admin bool) {
	uc, requestID, ok := quotaResetActorAndRequestID(c)
	if !ok {
		return
	}
	resp, err := h.service.RetryReset(c.Request.Context(), quotareset.DecisionInput{
		ActorUserID: uc.UserID,
		RequestID:   requestID,
		Admin:       admin,
	})
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	h.respondWithRequestSummary(c, resp.ID, uc.UserID, admin)
}

func (h *QuotaResetHandler) respondWithRequestSummary(c *gin.Context, requestID, viewerUserID int, admin bool) {
	resp, err := h.service.GetRequestSummary(c.Request.Context(), requestID, viewerUserID, admin)
	if err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func quotaResetDecisionReason(req quotaResetDecisionRequest) string {
	if strings.TrimSpace(req.DecisionReason) != "" {
		return req.DecisionReason
	}
	return req.Reason
}

func quotaResetActor(c *gin.Context) (*auth.UserContext, bool) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return uc, true
}

func quotaResetActorAndRequestID(c *gin.Context) (*auth.UserContext, int, bool) {
	uc, ok := quotaResetActor(c)
	if !ok {
		return nil, 0, false
	}
	requestID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || requestID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid request id")
		return nil, 0, false
	}
	return uc, requestID, true
}

func parseQuotaResetListParams(c *gin.Context) quotareset.ListParams {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	return quotareset.ListParams{
		Page:     page,
		PageSize: pageSize,
		Status:   strings.TrimSpace(c.Query("status")),
		Scope:    strings.TrimSpace(c.Query("scope")),
	}
}

func writeQuotaResetError(c *gin.Context, err error) {
	var advanced *quotareset.WorkflowAdvancedError
	if errors.As(err, &advanced) {
		var latest *quotareset.RequestSummary
		if advanced != nil {
			latest = advanced.Latest
		}
		pkg.ErrorWithDetails(c, http.StatusConflict, quotareset.ErrWorkflowAdvanced.Error(), gin.H{
			"request": latest,
		})
		return
	}
	switch {
	case ent.IsNotFound(err):
		pkg.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, quotareset.ErrNoRelayMapping), errors.Is(err, quotareset.ErrNotApprover), errors.Is(err, quotareset.ErrSelfApprovalForbidden):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case errors.Is(err, quotareset.ErrReasonRequired), errors.Is(err, quotareset.ErrDecisionRequired), errors.Is(err, quotareset.ErrInactiveSubscription), errors.Is(err, quotareset.ErrInvalidStatus), errors.Is(err, quotareset.ErrInvalidNotification), errors.Is(err, quotareset.ErrInvalidApproverConfig):
		pkg.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, quotareset.ErrActiveRequestExists), errors.Is(err, quotareset.ErrApproverConfigReferenced):
		pkg.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, quotareset.ErrDirectoryUnavailable):
		pkg.Error(c, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, quotareset.ErrProviderUnsupported):
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}
