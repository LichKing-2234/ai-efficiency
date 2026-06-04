package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminsubscription"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type AdminUsersHandler struct {
	entClient        *ent.Client
	encryptionKey    string
	resolver         adminUsersProviderResolver
	subscriptionJobs *adminsubscription.Service
}

type adminUsersProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type adminRelayGroupLister interface {
	ListPlatformGroups(ctx context.Context) ([]relay.Group, error)
}

type adminRelaySubscriptionAssigner interface {
	AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error
}

type adminRelaySubscriptionExtender interface {
	ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error
}

type adminRelaySubscriptionRemover interface {
	RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error
}

const adminSubscriptionBatchMaxUsers = 500

type adminUserRow struct {
	ID                int       `json:"id"`
	Username          string    `json:"username"`
	Email             string    `json:"email"`
	Role              string    `json:"role"`
	AuthSource        string    `json:"auth_source"`
	RelayUserID       *int      `json:"relay_user_id,omitempty"`
	RelayAuthPassword string    `json:"relay_auth_password"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type adminUsersListRequest struct {
	Q        string
	Page     int
	PageSize int
}

type adminSubscriptionProviderRow struct {
	ID          int                         `json:"id"`
	Name        string                      `json:"name"`
	DisplayName string                      `json:"display_name"`
	Groups      []adminSubscriptionGroupRow `json:"groups"`
}

type adminSubscriptionGroupRow struct {
	GroupID          string `json:"group_id"`
	GroupName        string `json:"group_name"`
	Platform         string `json:"platform"`
	SubscriptionType string `json:"subscription_type"`
}

type adminAssignSubscriptionRequest struct {
	ProviderID   int    `json:"provider_id"`
	GroupID      string `json:"group_id"`
	ValidityDays int    `json:"validity_days"`
}

type adminManageSubscriptionsRequest struct {
	Scope        string                         `json:"scope"`
	UserIDs      []int                          `json:"user_ids"`
	Filters      adminManageSubscriptionsFilter `json:"filters"`
	Operation    string                         `json:"operation"`
	ProviderID   int                            `json:"provider_id"`
	GroupID      string                         `json:"group_id"`
	ValidityDays int                            `json:"validity_days"`
	Days         int                            `json:"days"`
}

type adminManageSubscriptionsFilter struct {
	Q string `json:"q"`
}

type adminManageSubscriptionsResponse struct {
	Status       string                              `json:"status"`
	Scope        string                              `json:"scope"`
	Operation    string                              `json:"operation"`
	ProviderID   int                                 `json:"provider_id"`
	GroupID      string                              `json:"group_id"`
	TotalCount   int                                 `json:"total_count"`
	SuccessCount int                                 `json:"success_count"`
	SkippedCount int                                 `json:"skipped_count"`
	FailedCount  int                                 `json:"failed_count"`
	Results      []adminManageSubscriptionsResultRow `json:"results"`
}

type adminManageSubscriptionsResultRow struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	RelayUserID *int   `json:"relay_user_id,omitempty"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
}

type adminManageSubscriptionTarget struct {
	User      *ent.User
	MissingID int
}

type adminSubscriptionJobResponse struct {
	ID               int                           `json:"id"`
	Status           string                        `json:"status"`
	Phase            string                        `json:"phase"`
	Scope            string                        `json:"scope"`
	Operation        string                        `json:"operation"`
	ProviderID       int                           `json:"provider_id"`
	GroupID          string                        `json:"group_id"`
	ValidityDays     int                           `json:"validity_days,omitempty"`
	Days             int                           `json:"days,omitempty"`
	FilterQuery      string                        `json:"filter_query,omitempty"`
	TargetUserIDs    []int                         `json:"target_user_ids,omitempty"`
	RequestedUserIDs []int                         `json:"requested_user_ids,omitempty"`
	TotalCount       int                           `json:"total_count"`
	ProcessedCount   int                           `json:"processed_count"`
	SuccessCount     int                           `json:"success_count"`
	SkippedCount     int                           `json:"skipped_count"`
	FailedCount      int                           `json:"failed_count"`
	Results          []adminsubscription.ResultRow `json:"results"`
	LastError        *string                       `json:"last_error,omitempty"`
	StartedAt        *time.Time                    `json:"started_at,omitempty"`
	CompletedAt      *time.Time                    `json:"completed_at,omitempty"`
	CreatedAt        time.Time                     `json:"created_at"`
	UpdatedAt        time.Time                     `json:"updated_at"`
}

func NewAdminUsersHandler(entClient *ent.Client, encryptionKey string, resolvers ...adminUsersProviderResolver) *AdminUsersHandler {
	var resolver adminUsersProviderResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &AdminUsersHandler{
		entClient:        entClient,
		encryptionKey:    strings.TrimSpace(encryptionKey),
		resolver:         resolver,
		subscriptionJobs: adminsubscription.NewService(entClient),
	}
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	req := parseAdminUsersListRequest(c)
	query := h.entClient.User.Query()
	if req.Q != "" {
		query = query.Where(adminUsersSearchPredicate(req.Q))
	}

	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(req.PageSize).
		Offset((req.Page - 1) * req.PageSize).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	items := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		relayPassword := ""
		if u.RelayAuthPassword != nil {
			relayPassword = strings.TrimSpace(*u.RelayAuthPassword)
		}
		items = append(items, adminUserRow{
			ID:                u.ID,
			Username:          u.Username,
			Email:             u.Email,
			Role:              string(u.Role),
			AuthSource:        string(u.AuthSource),
			RelayUserID:       u.RelayUserID,
			RelayAuthPassword: relayPassword,
			CreatedAt:         u.CreatedAt,
			UpdatedAt:         u.UpdatedAt,
		})
	}

	pkg.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

func (h *AdminUsersHandler) ListSubscriptionOptions(c *gin.Context) {
	if h.resolver == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider resolver is not configured")
		return
	}

	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		Order(ent.Desc(relayprovider.FieldIsPrimary), ent.Asc(relayprovider.FieldID)).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list relay providers: "+err.Error())
		return
	}

	rows := make([]adminSubscriptionProviderRow, 0, len(providers))
	for _, p := range providers {
		rp, err := h.resolver.Resolve(c.Request.Context(), p.ID)
		if err != nil {
			pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("resolve relay provider %d: %v", p.ID, err))
			return
		}
		lister, ok := rp.(adminRelayGroupLister)
		if !ok {
			rows = append(rows, adminSubscriptionProviderRow{
				ID:          p.ID,
				Name:        p.Name,
				DisplayName: p.DisplayName,
				Groups:      []adminSubscriptionGroupRow{},
			})
			continue
		}
		groups, err := lister.ListPlatformGroups(c.Request.Context())
		if err != nil {
			pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("list relay provider %d groups: %v", p.ID, err))
			return
		}
		rows = append(rows, adminSubscriptionProviderRow{
			ID:          p.ID,
			Name:        p.Name,
			DisplayName: p.DisplayName,
			Groups:      adminSubscriptionGroupsFromRelay(groups),
		})
	}

	pkg.Success(c, gin.H{"providers": rows})
}

func (h *AdminUsersHandler) StartSubscriptionJob(c *gin.Context) {
	if h.resolver == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider resolver is not configured")
		return
	}

	var req adminManageSubscriptionsRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Scope = strings.TrimSpace(req.Scope)
	req.Operation = strings.TrimSpace(req.Operation)
	if req.ProviderID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "provider_id is required")
		return
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(req.GroupID), 10, 64)
	if err != nil || groupID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}
	switch req.Operation {
	case "add":
		if req.ValidityDays <= 0 {
			pkg.Error(c, http.StatusBadRequest, "validity_days is required")
			return
		}
	case "extend":
		if req.Days <= 0 {
			pkg.Error(c, http.StatusBadRequest, "days is required")
			return
		}
	case "remove":
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, or remove")
		return
	}

	rp, ok := h.resolveAssignableSubscriptionRelay(c, req.ProviderID, groupID)
	if !ok {
		return
	}
	if _, ok := adminSubscriptionOperation(c, rp, req, groupID); !ok {
		return
	}

	job, err := h.subscriptionJobs.StartJob(c.Request.Context(), adminsubscription.StartJobRequest{
		Scope:        req.Scope,
		UserIDs:      req.UserIDs,
		FilterQuery:  req.Filters.Q,
		Operation:    req.Operation,
		ProviderID:   req.ProviderID,
		GroupID:      strconv.FormatInt(groupID, 10),
		ValidityDays: req.ValidityDays,
		Days:         req.Days,
	})
	if err != nil {
		pkg.Error(c, adminSubscriptionJobErrorStatus(err), err.Error())
		return
	}

	operator := adminRelaySubscriptionJobOperator{provider: rp}
	go func(jobID int) {
		_ = h.subscriptionJobs.RunJob(context.Background(), jobID, operator)
	}(job.ID)

	pkg.Success(c, adminSubscriptionJobResponseFromEnt(job))
}

func (h *AdminUsersHandler) GetSubscriptionJob(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	job, err := h.subscriptionJobs.GetJob(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "subscription job not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get subscription job: "+err.Error())
		return
	}
	pkg.Success(c, adminSubscriptionJobResponseFromEnt(job))
}

func (h *AdminUsersHandler) GetLatestSubscriptionJob(c *gin.Context) {
	job, err := h.subscriptionJobs.GetLatestJob(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Success(c, nil)
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get latest subscription job: "+err.Error())
		return
	}
	pkg.Success(c, adminSubscriptionJobResponseFromEnt(job))
}

func (h *AdminUsersHandler) ManageSubscriptions(c *gin.Context) {
	if h.resolver == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider resolver is not configured")
		return
	}

	var req adminManageSubscriptionsRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Scope = strings.TrimSpace(req.Scope)
	req.Operation = strings.TrimSpace(req.Operation)
	if req.ProviderID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "provider_id is required")
		return
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(req.GroupID), 10, 64)
	if err != nil || groupID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}
	switch req.Operation {
	case "add":
		if req.ValidityDays <= 0 {
			pkg.Error(c, http.StatusBadRequest, "validity_days is required")
			return
		}
	case "extend":
		if req.Days <= 0 {
			pkg.Error(c, http.StatusBadRequest, "days is required")
			return
		}
	case "remove":
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, or remove")
		return
	}

	targets, ok := h.subscriptionTargetsForScope(c, req)
	if !ok {
		return
	}
	rp, ok := h.resolveAssignableSubscriptionRelay(c, req.ProviderID, groupID)
	if !ok {
		return
	}
	applyOperation, ok := adminSubscriptionOperation(c, rp, req, groupID)
	if !ok {
		return
	}

	resp := adminManageSubscriptionsResponse{
		Status:     "completed",
		Scope:      req.Scope,
		Operation:  req.Operation,
		ProviderID: req.ProviderID,
		GroupID:    strconv.FormatInt(groupID, 10),
		TotalCount: len(targets),
		Results:    make([]adminManageSubscriptionsResultRow, 0, len(targets)),
	}
	for _, target := range targets {
		if target.User == nil {
			resp.FailedCount++
			resp.Results = append(resp.Results, adminManageSubscriptionsResultRow{
				UserID:  target.MissingID,
				Status:  "failed",
				Message: "user not found",
			})
			continue
		}

		u := target.User
		row := adminManageSubscriptionsResultRow{
			UserID:      u.ID,
			Username:    u.Username,
			Email:       u.Email,
			RelayUserID: u.RelayUserID,
		}
		if u.RelayUserID == nil || *u.RelayUserID <= 0 {
			row.Status = "skipped"
			row.Message = "user is not linked to a relay user"
			resp.SkippedCount++
			resp.Results = append(resp.Results, row)
			continue
		}

		if err := applyOperation(c.Request.Context(), int64(*u.RelayUserID)); err != nil {
			row.Status = "failed"
			row.Message = err.Error()
			resp.FailedCount++
		} else {
			row.Status = "success"
			resp.SuccessCount++
		}
		resp.Results = append(resp.Results, row)
	}

	pkg.Success(c, resp)
}

func (h *AdminUsersHandler) AssignSubscription(c *gin.Context) {
	if h.resolver == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider resolver is not configured")
		return
	}

	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	var req adminAssignSubscriptionRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ProviderID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "provider_id is required")
		return
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(req.GroupID), 10, 64)
	if err != nil || groupID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}
	if req.ValidityDays <= 0 {
		pkg.Error(c, http.StatusBadRequest, "validity_days is required")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "user not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}
	if u.RelayUserID == nil || *u.RelayUserID <= 0 {
		pkg.Error(c, http.StatusUnprocessableEntity, "user is not linked to a relay user")
		return
	}

	rp, ok := h.resolveAssignableSubscriptionRelay(c, req.ProviderID, groupID)
	if !ok {
		return
	}

	assigner, ok := rp.(adminRelaySubscriptionAssigner)
	if !ok {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription assignment")
		return
	}
	if err := assigner.AssignSubscriptionForUser(c.Request.Context(), int64(*u.RelayUserID), groupID, req.ValidityDays); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"status":        "assigned",
		"provider_id":   req.ProviderID,
		"group_id":      strconv.FormatInt(groupID, 10),
		"relay_user_id": *u.RelayUserID,
	})
}

func (h *AdminUsersHandler) RevealRelayPassword(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "user not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay auth password is not stored")
		return
	}
	if h.encryptionKey == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil || strings.TrimSpace(password) == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	pkg.Success(c, gin.H{"password": password})
}

func (h *AdminUsersHandler) resolveAssignableSubscriptionRelay(c *gin.Context, providerID int, groupID int64) (relay.Provider, bool) {
	if _, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.IDEQ(providerID), relayprovider.EnabledEQ(true)).
		Only(c.Request.Context()); err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider is not enabled or not found")
			return nil, false
		}
		pkg.Error(c, http.StatusInternalServerError, "get relay provider: "+err.Error())
		return nil, false
	}

	rp, err := h.resolver.Resolve(c.Request.Context(), providerID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("resolve relay provider %d: %v", providerID, err))
		return nil, false
	}
	lister, ok := rp.(adminRelayGroupLister)
	if !ok {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription group listing")
		return nil, false
	}
	groups, err := lister.ListPlatformGroups(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("list relay provider %d groups: %v", providerID, err))
		return nil, false
	}
	if !hasAssignableAdminSubscriptionGroup(groups, groupID) {
		pkg.Error(c, http.StatusUnprocessableEntity, "subscription group is not assignable")
		return nil, false
	}
	return rp, true
}

func adminSubscriptionOperation(c *gin.Context, rp relay.Provider, req adminManageSubscriptionsRequest, groupID int64) (func(context.Context, int64) error, bool) {
	switch req.Operation {
	case "add":
		assigner, ok := rp.(adminRelaySubscriptionAssigner)
		if !ok {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription assignment")
			return nil, false
		}
		return func(ctx context.Context, relayUserID int64) error {
			return assigner.AssignSubscriptionForUser(ctx, relayUserID, groupID, req.ValidityDays)
		}, true
	case "extend":
		extender, ok := rp.(adminRelaySubscriptionExtender)
		if !ok {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription extension")
			return nil, false
		}
		return func(ctx context.Context, relayUserID int64) error {
			return extender.ExtendSubscriptionForUser(ctx, relayUserID, groupID, req.Days)
		}, true
	case "remove":
		remover, ok := rp.(adminRelaySubscriptionRemover)
		if !ok {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription removal")
			return nil, false
		}
		return func(ctx context.Context, relayUserID int64) error {
			return remover.RemoveSubscriptionForUser(ctx, relayUserID, groupID)
		}, true
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, or remove")
		return nil, false
	}
}

type adminRelaySubscriptionJobOperator struct {
	provider relay.Provider
}

func (o adminRelaySubscriptionJobOperator) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	assigner, ok := o.provider.(adminRelaySubscriptionAssigner)
	if !ok {
		return fmt.Errorf("relay provider does not support subscription assignment")
	}
	return assigner.AssignSubscriptionForUser(ctx, userID, groupID, validityDays)
}

func (o adminRelaySubscriptionJobOperator) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	extender, ok := o.provider.(adminRelaySubscriptionExtender)
	if !ok {
		return fmt.Errorf("relay provider does not support subscription extension")
	}
	return extender.ExtendSubscriptionForUser(ctx, userID, groupID, days)
}

func (o adminRelaySubscriptionJobOperator) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	remover, ok := o.provider.(adminRelaySubscriptionRemover)
	if !ok {
		return fmt.Errorf("relay provider does not support subscription removal")
	}
	return remover.RemoveSubscriptionForUser(ctx, userID, groupID)
}

func adminSubscriptionJobResponseFromEnt(job *ent.AdminSubscriptionJob) adminSubscriptionJobResponse {
	if job == nil {
		return adminSubscriptionJobResponse{}
	}
	return adminSubscriptionJobResponse{
		ID:               job.ID,
		Status:           string(job.Status),
		Phase:            string(job.Phase),
		Scope:            string(job.Scope),
		Operation:        string(job.Operation),
		ProviderID:       job.ProviderID,
		GroupID:          job.GroupID,
		ValidityDays:     job.ValidityDays,
		Days:             job.Days,
		FilterQuery:      job.FilterQuery,
		TargetUserIDs:    job.TargetUserIds,
		RequestedUserIDs: job.RequestedUserIds,
		TotalCount:       job.TotalCount,
		ProcessedCount:   job.ProcessedCount,
		SuccessCount:     job.SuccessCount,
		SkippedCount:     job.SkippedCount,
		FailedCount:      job.FailedCount,
		Results:          adminsubscription.ResultsFromJob(job),
		LastError:        job.LastError,
		StartedAt:        job.StartedAt,
		CompletedAt:      job.CompletedAt,
		CreatedAt:        job.CreatedAt,
		UpdatedAt:        job.UpdatedAt,
	}
}

func adminSubscriptionJobErrorStatus(err error) int {
	message := err.Error()
	if strings.Contains(message, "maximum is") {
		return http.StatusUnprocessableEntity
	}
	return http.StatusBadRequest
}

func (h *AdminUsersHandler) subscriptionTargetsForScope(c *gin.Context, req adminManageSubscriptionsRequest) ([]adminManageSubscriptionTarget, bool) {
	query := h.entClient.User.Query()
	switch req.Scope {
	case "selected":
		ids := uniquePositiveInts(req.UserIDs)
		if len(ids) == 0 {
			pkg.Error(c, http.StatusBadRequest, "user_ids is required for selected scope")
			return nil, false
		}
		if len(ids) > adminSubscriptionBatchMaxUsers {
			pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("subscription batch targets too many; maximum is %d users", adminSubscriptionBatchMaxUsers))
			return nil, false
		}
		users, err := query.Where(entuser.IDIn(ids...)).All(c.Request.Context())
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
			return nil, false
		}
		usersByID := make(map[int]*ent.User, len(users))
		for _, u := range users {
			usersByID[u.ID] = u
		}
		targets := make([]adminManageSubscriptionTarget, 0, len(ids))
		for _, id := range ids {
			if u := usersByID[id]; u != nil {
				targets = append(targets, adminManageSubscriptionTarget{User: u})
				continue
			}
			targets = append(targets, adminManageSubscriptionTarget{MissingID: id})
		}
		return targets, true
	case "current_filter":
		q := strings.TrimSpace(req.Filters.Q)
		if q != "" {
			query = query.Where(adminUsersSearchPredicate(q))
		}
	case "all_mapped":
		query = query.Where(entuser.RelayUserIDNotNil(), entuser.RelayUserIDGT(0))
	default:
		pkg.Error(c, http.StatusBadRequest, "scope must be selected, current_filter, or all_mapped")
		return nil, false
	}

	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(adminSubscriptionBatchMaxUsers + 1).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return nil, false
	}
	if len(users) > adminSubscriptionBatchMaxUsers {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("subscription batch targets too many; maximum is %d users", adminSubscriptionBatchMaxUsers))
		return nil, false
	}
	targets := make([]adminManageSubscriptionTarget, 0, len(users))
	for _, u := range users {
		targets = append(targets, adminManageSubscriptionTarget{User: u})
	}
	return targets, true
}

func adminSubscriptionGroupsFromRelay(groups []relay.Group) []adminSubscriptionGroupRow {
	rows := make([]adminSubscriptionGroupRow, 0, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || strings.TrimSpace(group.Platform) == "" {
			continue
		}
		subscriptionType := strings.TrimSpace(group.SubscriptionType)
		if subscriptionType != "" && !strings.EqualFold(subscriptionType, "subscription") {
			continue
		}
		groupID := strconv.FormatInt(group.ID, 10)
		groupName := strings.TrimSpace(group.Name)
		if groupName == "" {
			groupName = groupID
		}
		rows = append(rows, adminSubscriptionGroupRow{
			GroupID:          groupID,
			GroupName:        groupName,
			Platform:         strings.TrimSpace(group.Platform),
			SubscriptionType: firstNonEmptyAdminSubscriptionType(subscriptionType),
		})
	}
	return rows
}

func hasAssignableAdminSubscriptionGroup(groups []relay.Group, groupID int64) bool {
	target := strconv.FormatInt(groupID, 10)
	for _, group := range adminSubscriptionGroupsFromRelay(groups) {
		if group.GroupID == target {
			return true
		}
	}
	return false
}

func firstNonEmptyAdminSubscriptionType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "subscription"
	}
	return strings.TrimSpace(value)
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	ids := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	return ids
}

func adminUsersSearchPredicate(q string) predicate.User {
	predicates := []predicate.User{
		entuser.UsernameContainsFold(q),
		entuser.EmailContainsFold(q),
	}
	if n, err := strconv.Atoi(q); err == nil {
		predicates = append(predicates, entuser.IDEQ(n), entuser.RelayUserIDEQ(n))
	}
	return entuser.Or(predicates...)
}

func parseAdminUsersListRequest(c *gin.Context) adminUsersListRequest {
	page := parseOptionalInt(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize := parseOptionalInt(c.DefaultQuery("page_size", "20"))
	switch {
	case pageSize <= 0:
		pageSize = 20
	case pageSize > 100:
		pageSize = 100
	}
	return adminUsersListRequest{
		Q:        strings.TrimSpace(c.Query("q")),
		Page:     page,
		PageSize: pageSize,
	}
}
