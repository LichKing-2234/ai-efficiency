package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	GetNotificationSettings(context.Context) (*quotareset.NotificationSettings, error)
	UpdateNotificationSettings(context.Context, quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error)
	TestNotificationSettings(context.Context, int) error
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
	Reason         string `json:"reason"`
	DecisionReason string `json:"decision_reason"`
}

type quotaResetSaveApproverConfigsRequest struct {
	Items []quotareset.ApproverConfigInput `json:"items"`
	Mode  string                           `json:"mode"`
}

type quotaResetNotificationSettingsRequest struct {
	Enabled      bool   `json:"enabled"`
	URL          string `json:"url"`
	AuthType     string `json:"auth_type"`
	CredentialID *int   `json:"credential_id"`
}

type quotaResetRequestResponse struct {
	ID                      int        `json:"id"`
	RequesterUserID         int        `json:"requester_user_id"`
	ProviderID              int        `json:"provider_id"`
	GroupID                 string     `json:"group_id"`
	GroupName               string     `json:"group_name"`
	GroupPlatform           string     `json:"group_platform"`
	Reason                  string     `json:"reason"`
	Status                  string     `json:"status"`
	ResolvedApproverUserIDs []int      `json:"resolved_approver_user_ids"`
	ApprovedByUserID        *int       `json:"approved_by_user_id,omitempty"`
	RejectedByUserID        *int       `json:"rejected_by_user_id,omitempty"`
	DecisionReason          string     `json:"decision_reason,omitempty"`
	ResetError              string     `json:"reset_error,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	DecidedAt               *time.Time `json:"decided_at,omitempty"`
	ResetStartedAt          *time.Time `json:"reset_started_at,omitempty"`
	ResetCompletedAt        *time.Time `json:"reset_completed_at,omitempty"`
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
	pkg.Success(c, quotaResetEntResponse(created))
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
	pkg.Success(c, quotaResetEntResponse(resp))
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
	sourceID := parseOptionalInt(c.Query("source_id"))
	if sourceID <= 0 {
		writeQuotaResetError(c, fmt.Errorf("%w: source_id is required", quotareset.ErrInvalidApproverConfig))
		return
	}
	resp, err := h.service.ListApproverCandidates(c.Request.Context(), quotareset.ApproverCandidateParams{
		SourceID: sourceID,
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
	resp, err := h.service.SaveApproverConfigs(c.Request.Context(), quotareset.SaveApproverConfigsInput{
		ActorUserID: uc.UserID,
		Mode:        strings.TrimSpace(req.Mode),
		Items:       req.Items,
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
	if err := h.service.TestNotificationSettings(c.Request.Context(), uc.UserID); err != nil {
		writeQuotaResetError(c, err)
		return
	}
	pkg.Success(c, gin.H{"message": "notification test sent"})
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
	pkg.Success(c, quotaResetEntResponse(resp))
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
	pkg.Success(c, quotaResetEntResponse(resp))
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
	}
}

func quotaResetEntResponse(req *ent.QuotaResetRequest) quotaResetRequestResponse {
	if req == nil {
		return quotaResetRequestResponse{}
	}
	return quotaResetRequestResponse{
		ID:                      req.ID,
		RequesterUserID:         req.RequesterUserID,
		ProviderID:              req.ProviderID,
		GroupID:                 req.GroupID,
		GroupName:               req.GroupName,
		GroupPlatform:           req.GroupPlatform,
		Reason:                  req.Reason,
		Status:                  req.Status.String(),
		ResolvedApproverUserIDs: req.ResolvedApproverUserIds,
		ApprovedByUserID:        req.ApprovedByUserID,
		RejectedByUserID:        req.RejectedByUserID,
		DecisionReason:          req.DecisionReason,
		ResetError:              req.ResetError,
		CreatedAt:               req.CreatedAt,
		UpdatedAt:               req.UpdatedAt,
		DecidedAt:               req.DecidedAt,
		ResetStartedAt:          req.ResetStartedAt,
		ResetCompletedAt:        req.ResetCompletedAt,
	}
}

func writeQuotaResetError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, quotareset.ErrNoRelayMapping), errors.Is(err, quotareset.ErrNotApprover), errors.Is(err, quotareset.ErrSelfApprovalForbidden):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case errors.Is(err, quotareset.ErrReasonRequired), errors.Is(err, quotareset.ErrDecisionRequired), errors.Is(err, quotareset.ErrInactiveSubscription), errors.Is(err, quotareset.ErrInvalidStatus), errors.Is(err, quotareset.ErrInvalidNotification), errors.Is(err, quotareset.ErrInvalidApproverConfig):
		pkg.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, quotareset.ErrActiveRequestExists), errors.Is(err, quotareset.ErrApproverConfigReferenced):
		pkg.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, quotareset.ErrProviderUnsupported), errors.Is(err, quotareset.ErrDirectoryUnavailable):
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}
