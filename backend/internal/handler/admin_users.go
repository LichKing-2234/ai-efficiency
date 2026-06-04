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
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type AdminUsersHandler struct {
	entClient     *ent.Client
	encryptionKey string
	resolver      adminUsersProviderResolver
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

func NewAdminUsersHandler(entClient *ent.Client, encryptionKey string, resolvers ...adminUsersProviderResolver) *AdminUsersHandler {
	var resolver adminUsersProviderResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &AdminUsersHandler{
		entClient:     entClient,
		encryptionKey: strings.TrimSpace(encryptionKey),
		resolver:      resolver,
	}
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	req := parseAdminUsersListRequest(c)
	query := h.entClient.User.Query()
	if req.Q != "" {
		predicates := []predicate.User{
			entuser.UsernameContainsFold(req.Q),
			entuser.EmailContainsFold(req.Q),
		}
		if n, err := strconv.Atoi(req.Q); err == nil {
			predicates = append(predicates, entuser.IDEQ(n), entuser.RelayUserIDEQ(n))
		}
		query = query.Where(entuser.Or(predicates...))
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

	if _, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.IDEQ(req.ProviderID), relayprovider.EnabledEQ(true)).
		Only(c.Request.Context()); err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider is not enabled or not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get relay provider: "+err.Error())
		return
	}

	rp, err := h.resolver.Resolve(c.Request.Context(), req.ProviderID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("resolve relay provider %d: %v", req.ProviderID, err))
		return
	}
	lister, ok := rp.(adminRelayGroupLister)
	if !ok {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription group listing")
		return
	}
	groups, err := lister.ListPlatformGroups(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("list relay provider %d groups: %v", req.ProviderID, err))
		return
	}
	if !hasAssignableAdminSubscriptionGroup(groups, groupID) {
		pkg.Error(c, http.StatusUnprocessableEntity, "subscription group is not assignable")
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
