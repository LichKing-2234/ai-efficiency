package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminsubscription"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
	"github.com/ai-efficiency/backend/internal/adminusers"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/directorytree"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AdminUsersHandler struct {
	entClient        *ent.Client
	users            *adminusers.Service
	encryptionKey    string
	resolver         adminUsersProviderResolver
	subscriptionJobs *adminsubscription.Service
	logger           *zap.Logger
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

type adminRelaySubscriptionQuotaResetter interface {
	ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error
}

const adminSubscriptionBatchMaxUsers = 500

const (
	directoryDepartmentRepresentativeIDsKey = "representative_external_ids"
	directoryMemberLeaderDepartmentIDsKey   = "leader_department_ids"
)

type adminUserRow struct {
	ID                int                     `json:"id"`
	Username          string                  `json:"username"`
	Email             string                  `json:"email"`
	Role              string                  `json:"role"`
	AuthSource        string                  `json:"auth_source"`
	RelayUserID       *int                    `json:"relay_user_id,omitempty"`
	RelayAuthPassword string                  `json:"relay_auth_password"`
	AccessStatus      string                  `json:"access_status"`
	TokenValidAfter   *time.Time              `json:"token_valid_after,omitempty"`
	RelayDisabledAt   *time.Time              `json:"relay_disabled_at,omitempty"`
	OffboardingStatus string                  `json:"offboarding_status,omitempty"`
	Department        *adminUserDepartmentRow `json:"department,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
}

type adminUsersListRequest struct {
	Q            string
	DepartmentID string
	AccessStatus string
	Page         int
	PageSize     int
}

type adminUserDepartmentRow struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	DisplayPath string `json:"display_path"`
}

type adminDirectoryDepartmentSummaryRow struct {
	ExternalID                 string  `json:"external_id"`
	ParentExternalID           *string `json:"parent_external_id,omitempty"`
	Name                       string  `json:"name"`
	Path                       string  `json:"path"`
	DisplayPath                string  `json:"display_path"`
	Depth                      int     `json:"depth"`
	ChildCount                 int     `json:"child_count"`
	MemberCount                int     `json:"member_count"`
	MatchedUserCount           int     `json:"matched_user_count"`
	SubtreeMemberCount         int     `json:"subtree_member_count"`
	SubtreeMatchedUserCount    int     `json:"subtree_matched_user_count"`
	RepresentativeCount        int     `json:"representative_count"`
	MatchedRepresentativeCount int     `json:"matched_representative_count"`
}

type adminDepartmentOptionRow struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	DisplayPath string `json:"display_path"`
}

type adminDepartmentChildRow struct {
	ExternalID                 string  `json:"external_id"`
	ParentExternalID           *string `json:"parent_external_id,omitempty"`
	Name                       string  `json:"name"`
	Path                       string  `json:"path"`
	DisplayPath                string  `json:"display_path"`
	Depth                      int     `json:"depth"`
	ChildCount                 int     `json:"child_count"`
	HasChildren                bool    `json:"has_children"`
	MemberCount                int     `json:"member_count"`
	MatchedUserCount           int     `json:"matched_user_count"`
	SubtreeMemberCount         int     `json:"subtree_member_count"`
	SubtreeMatchedUserCount    int     `json:"subtree_matched_user_count"`
	RepresentativeCount        int     `json:"representative_count"`
	MatchedRepresentativeCount int     `json:"matched_representative_count"`
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

type adminDisableAccessRequest struct {
	ConfirmEmail string `json:"confirm_email"`
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
	Q            string `json:"q"`
	DepartmentID string `json:"department_id"`
	AccessStatus string `json:"access_status"`
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
	userReader := adminusers.NewService(entClient)
	return &AdminUsersHandler{
		entClient:     entClient,
		users:         userReader,
		encryptionKey: strings.TrimSpace(encryptionKey),
		resolver:      resolver,
		subscriptionJobs: adminsubscription.NewService(entClient, adminsubscription.CurrentFilterTargetResolverFunc(
			func(ctx context.Context, filter adminsubscription.CurrentFilter, limit int) ([]*ent.User, error) {
				return userReader.Targets(ctx, adminusers.Filters{
					Query:        filter.Query,
					DepartmentID: filter.DepartmentID,
					AccessStatus: filter.AccessStatus,
				}, limit)
			},
		)),
		logger: zap.NewNop(),
	}
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	req := parseAdminUsersListRequest(c)
	page, err := h.users.List(c.Request.Context(), adminusers.ListRequest{
		Filters: adminusers.Filters{
			Query:        req.Q,
			DepartmentID: req.DepartmentID,
			AccessStatus: req.AccessStatus,
		},
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		if errors.Is(err, adminusers.ErrInvalidAccessStatus) {
			pkg.Error(c, http.StatusBadRequest, "access_status must be configured, disabled, or missing_credential")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]adminUserRow, 0, len(page.Users))
	for _, u := range page.Users {
		relayPassword := ""
		if u.RelayAuthPassword != nil {
			relayPassword = strings.TrimSpace(*u.RelayAuthPassword)
		}
		offboardingFact := page.OffboardingByUserID[u.ID]
		var department *adminUserDepartmentRow
		if value := page.DepartmentsByUserID[u.ID]; value != nil {
			department = &adminUserDepartmentRow{
				ExternalID:  value.ExternalID,
				Name:        value.Name,
				Path:        value.Path,
				DisplayPath: value.DisplayPath,
			}
		}
		items = append(items, adminUserRow{
			ID:                u.ID,
			Username:          u.Username,
			Email:             u.Email,
			Role:              string(u.Role),
			AuthSource:        string(u.AuthSource),
			RelayUserID:       u.RelayUserID,
			RelayAuthPassword: relayPassword,
			AccessStatus:      adminuseraccess.Derive(u, relayPassword, offboardingFact.Succeeded),
			TokenValidAfter:   u.TokenValidAfter,
			RelayDisabledAt:   u.RelayDisabledAt,
			OffboardingStatus: offboardingFact.LatestStatus,
			Department:        department,
			CreatedAt:         u.CreatedAt,
			UpdatedAt:         u.UpdatedAt,
		})
	}

	pkg.Success(c, gin.H{
		"items":     items,
		"total":     page.Total,
		"page":      page.Page,
		"page_size": page.PageSize,
	})
}

func (h *AdminUsersHandler) ListDepartmentOptions(c *gin.Context) {
	page, err := h.users.DepartmentOptions(c.Request.Context(), adminusers.DepartmentOptionRequest{
		Query:      strings.TrimSpace(c.Query("q")),
		SelectedID: strings.TrimSpace(c.Query("selected_id")),
		Page:       parseOptionalInt(c.Query("page")),
		PageSize:   parseOptionalInt(c.Query("page_size")),
	})
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]adminDepartmentOptionRow, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, adminDepartmentOptionRow{
			ExternalID:  item.ExternalID,
			Name:        item.Name,
			DisplayPath: item.DisplayPath,
		})
	}
	var selected *adminDepartmentOptionRow
	if page.Selected != nil {
		selected = &adminDepartmentOptionRow{
			ExternalID:  page.Selected.ExternalID,
			Name:        page.Selected.Name,
			DisplayPath: page.Selected.DisplayPath,
		}
	}
	pkg.Success(c, gin.H{
		"items":     items,
		"selected":  selected,
		"total":     page.Total,
		"page":      page.Page,
		"page_size": page.PageSize,
	})
}

func (h *AdminUsersHandler) ListDepartmentChildren(c *gin.Context) {
	page, err := h.users.DepartmentChildren(c.Request.Context(), adminusers.DepartmentChildrenRequest{
		ParentDepartmentID: strings.TrimSpace(c.Query("parent_department_id")),
		Page:               parseOptionalInt(c.Query("page")),
		PageSize:           parseOptionalInt(c.Query("page_size")),
	})
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]adminDepartmentChildRow, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, adminDepartmentChildRow{
			ExternalID:                 item.ExternalID,
			ParentExternalID:           item.ParentExternalID,
			Name:                       item.Name,
			Path:                       item.Path,
			DisplayPath:                item.DisplayPath,
			Depth:                      item.Depth,
			ChildCount:                 item.ChildCount,
			HasChildren:                item.HasChildren,
			MemberCount:                item.MemberCount,
			MatchedUserCount:           item.MatchedUserCount,
			SubtreeMemberCount:         item.SubtreeMemberCount,
			SubtreeMatchedUserCount:    item.SubtreeMatchedUserCount,
			RepresentativeCount:        item.RepresentativeCount,
			MatchedRepresentativeCount: item.MatchedRepresentativeCount,
		})
	}
	pkg.Success(c, gin.H{
		"items":                items,
		"parent_department_id": page.ParentDepartmentID,
		"total":                page.Total,
		"page":                 page.Page,
		"page_size":            page.PageSize,
	})
}

func (h *AdminUsersHandler) ListDepartments(c *gin.Context) {
	sourceID, ok, err := h.currentDirectorySourceID(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		pkg.Success(c, gin.H{"items": []adminDirectoryDepartmentSummaryRow{}})
		return
	}
	departments, err := h.entClient.DirectoryDepartment.Query().
		Where(directorydepartment.SourceIDEQ(sourceID)).
		Order(ent.Asc(directorydepartment.FieldName), ent.Asc(directorydepartment.FieldExternalID)).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list directory departments: "+err.Error())
		return
	}
	members, err := h.entClient.DirectoryMember.Query().
		Where(directorymember.SourceIDEQ(sourceID)).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list directory members: "+err.Error())
		return
	}
	memberDepartmentIDs, err := h.memberDepartmentIDsByMember(c.Request.Context(), sourceID, members)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	memberKeys := make(map[string]map[string]struct{}, len(departments))
	matchedKeys := make(map[string]map[string]struct{}, len(departments))
	membersByExternalID := make(map[string]*ent.DirectoryMember, len(members))
	for _, member := range members {
		externalID := strings.TrimSpace(member.ExternalID)
		if externalID != "" {
			membersByExternalID[externalID] = member
		}
		for _, departmentID := range adminDirectoryMemberDepartmentIDs(member, memberDepartmentIDs) {
			addAdminStringSetValue(memberKeys, departmentID, strconv.Itoa(member.ID))
			if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
				addAdminStringSetValue(matchedKeys, departmentID, strconv.Itoa(*member.MatchedUserID))
			}
		}
	}
	representativeIDs := representativeExternalIDsByDepartment(departments, members)
	tree := directorytree.New(departments)
	rows := make([]adminDirectoryDepartmentSummaryRow, 0, len(departments))
	for _, department := range tree.Ordered() {
		subtreeIDs := tree.SubtreeIDs(department.ExternalID)
		departmentRepresentativeIDs := representativeIDs[department.ExternalID]
		rows = append(rows, adminDirectoryDepartmentSummaryRow{
			ExternalID:                 department.ExternalID,
			ParentExternalID:           department.ParentExternalID,
			Name:                       department.Name,
			Path:                       department.Path,
			DisplayPath:                tree.DisplayPath(department.ExternalID),
			Depth:                      tree.Depth(department.ExternalID),
			ChildCount:                 tree.ChildCount(department.ExternalID),
			MemberCount:                len(memberKeys[department.ExternalID]),
			MatchedUserCount:           len(matchedKeys[department.ExternalID]),
			SubtreeMemberCount:         countAdminStringSetUnion(subtreeIDs, memberKeys),
			SubtreeMatchedUserCount:    countAdminStringSetUnion(subtreeIDs, matchedKeys),
			RepresentativeCount:        len(departmentRepresentativeIDs),
			MatchedRepresentativeCount: matchedRepresentativeCount(departmentRepresentativeIDs, membersByExternalID),
		})
	}
	pkg.Success(c, gin.H{"items": rows})
}

func representativeExternalIDsByDepartment(departments []*ent.DirectoryDepartment, members []*ent.DirectoryMember) map[string]map[string]struct{} {
	representatives := make(map[string]map[string]struct{}, len(departments))
	add := func(departmentID, representativeExternalID string) {
		departmentID = strings.TrimSpace(departmentID)
		representativeExternalID = strings.TrimSpace(representativeExternalID)
		if departmentID == "" || representativeExternalID == "" {
			return
		}
		if representatives[departmentID] == nil {
			representatives[departmentID] = map[string]struct{}{}
		}
		representatives[departmentID][representativeExternalID] = struct{}{}
	}

	for _, department := range departments {
		for _, representativeExternalID := range metadataStringValues(department.Metadata[directoryDepartmentRepresentativeIDsKey]) {
			add(department.ExternalID, representativeExternalID)
		}
	}
	for _, member := range members {
		for _, departmentID := range metadataStringValues(member.Metadata[directoryMemberLeaderDepartmentIDsKey]) {
			add(departmentID, member.ExternalID)
		}
	}
	return representatives
}

func matchedRepresentativeCount(representativeExternalIDs map[string]struct{}, membersByExternalID map[string]*ent.DirectoryMember) int {
	count := 0
	for representativeExternalID := range representativeExternalIDs {
		member := membersByExternalID[representativeExternalID]
		if member != nil && member.MatchedUserID != nil && *member.MatchedUserID > 0 {
			count++
		}
	}
	return count
}

func metadataStringValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := metadataScalarString(item); value != "" {
				values = append(values, value)
			}
		}
		return values
	case []string:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(item); value != "" {
				values = append(values, value)
			}
		}
		return values
	default:
		if value := metadataScalarString(typed); value != "" {
			return []string{value}
		}
		return nil
	}
}

func metadataScalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(typed), 'f', -1, 32))
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
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
	case "reset_quota":
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, remove, or reset_quota")
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
		DepartmentID: req.Filters.DepartmentID,
		AccessStatus: req.Filters.AccessStatus,
		Operation:    req.Operation,
		ProviderID:   req.ProviderID,
		GroupID:      strconv.FormatInt(groupID, 10),
		ValidityDays: req.ValidityDays,
		Days:         req.Days,
	})
	if err != nil {
		if errors.Is(err, adminusers.ErrInvalidAccessStatus) {
			pkg.Error(c, http.StatusBadRequest, "access_status must be configured, disabled, or missing_credential")
			return
		}
		pkg.Error(c, adminSubscriptionJobErrorStatus(err), err.Error())
		return
	}

	operator := adminRelaySubscriptionJobOperator{provider: rp}
	go func(jobID int) {
		if err := h.subscriptionJobs.RunJob(context.Background(), jobID, operator); err != nil && h.logger != nil {
			h.logger.Error(
				"admin subscription job failed",
				zap.Int("job_id", jobID),
				zap.Int("provider_id", req.ProviderID),
				zap.String("group_id", req.GroupID),
				zap.String("operation", req.Operation),
				zap.Error(err),
			)
		}
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
	case "reset_quota":
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, remove, or reset_quota")
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

func (h *AdminUsersHandler) DisableAccess(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	var req adminDisableAccessRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	confirmEmail := strings.ToLower(strings.TrimSpace(req.ConfirmEmail))
	if confirmEmail == "" {
		pkg.Error(c, http.StatusBadRequest, "confirm_email is required")
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
	if strings.ToLower(strings.TrimSpace(u.Email)) != confirmEmail {
		pkg.Error(c, http.StatusUnprocessableEntity, "confirm_email must match user email")
		return
	}
	if u.RelayUserID == nil || *u.RelayUserID <= 0 {
		pkg.Error(c, http.StatusUnprocessableEntity, "user is not linked to a relay user")
		return
	}
	if h.resolver == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider resolver is not configured")
		return
	}
	providerID, err := h.primaryRelayProviderID(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "get relay provider: "+err.Error())
		return
	}
	if providerID <= 0 {
		pkg.Error(c, http.StatusUnprocessableEntity, "no enabled relay provider")
		return
	}
	rp, err := h.resolver.Resolve(c.Request.Context(), providerID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("resolve relay provider %d: %v", providerID, err))
		return
	}
	disabler, ok := rp.(relay.UserDisabler)
	if !ok {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support user disable")
		return
	}
	if err := disabler.DisableUser(c.Request.Context(), int64(*u.RelayUserID)); err != nil {
		pkg.Error(c, http.StatusBadGateway, "disable relay user: "+err.Error())
		return
	}
	disabledAt := time.Now()
	if _, err := h.entClient.User.UpdateOneID(u.ID).SetRelayDisabledAt(disabledAt).Save(c.Request.Context()); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "mark relay user disabled: "+err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"status":            "disabled",
		"relay_user_id":     *u.RelayUserID,
		"relay_disabled_at": disabledAt,
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

func (h *AdminUsersHandler) primaryRelayProviderID(ctx context.Context) (int, error) {
	p, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true), relayprovider.IsPrimaryEQ(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if err == nil {
		return p.ID, nil
	}
	if !ent.IsNotFound(err) {
		return 0, err
	}
	p, err = h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return p.ID, nil
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
	case "reset_quota":
		resetter, ok := rp.(adminRelaySubscriptionQuotaResetter)
		if !ok {
			pkg.Error(c, http.StatusUnprocessableEntity, "relay provider does not support subscription quota reset")
			return nil, false
		}
		return func(ctx context.Context, relayUserID int64) error {
			return resetter.ResetSubscriptionQuotaForUser(ctx, relayUserID, groupID)
		}, true
	default:
		pkg.Error(c, http.StatusBadRequest, "operation must be add, extend, remove, or reset_quota")
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

func (o adminRelaySubscriptionJobOperator) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	resetter, ok := o.provider.(adminRelaySubscriptionQuotaResetter)
	if !ok {
		return fmt.Errorf("relay provider does not support subscription quota reset")
	}
	return resetter.ResetSubscriptionQuotaForUser(ctx, userID, groupID)
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
	var tooManyTargets *adminsubscription.TooManyTargetsError
	if errors.As(err, &tooManyTargets) {
		return http.StatusUnprocessableEntity
	}
	if errors.Is(err, adminusers.ErrInvalidAccessStatus) {
		return http.StatusBadRequest
	}
	var validationErr *adminsubscription.ValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
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
		users, err := h.users.Targets(c.Request.Context(), adminusers.Filters{
			Query:        req.Filters.Q,
			DepartmentID: req.Filters.DepartmentID,
			AccessStatus: req.Filters.AccessStatus,
		}, adminsubscription.MaxTargets+1)
		if err != nil {
			if errors.Is(err, adminusers.ErrInvalidAccessStatus) {
				pkg.Error(c, http.StatusBadRequest, "access_status must be configured, disabled, or missing_credential")
				return nil, false
			}
			pkg.Error(c, http.StatusInternalServerError, err.Error())
			return nil, false
		}
		if len(users) > adminsubscription.MaxTargets {
			pkg.Error(c, http.StatusUnprocessableEntity, fmt.Sprintf("subscription batch targets too many; maximum is %d users", adminsubscription.MaxTargets))
			return nil, false
		}
		targets := make([]adminManageSubscriptionTarget, 0, len(users))
		for _, u := range users {
			targets = append(targets, adminManageSubscriptionTarget{User: u})
		}
		return targets, true
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

func (h *AdminUsersHandler) currentDirectorySourceID(ctx context.Context) (int, bool, error) {
	return directorysync.CurrentSourceID(ctx, h.entClient)
}

func (h *AdminUsersHandler) memberDepartmentIDsByMember(ctx context.Context, sourceID int, members []*ent.DirectoryMember) (map[int][]string, error) {
	out := make(map[int][]string, len(members))
	if len(members) == 0 {
		return out, nil
	}
	memberIDs := make([]int, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID)
	}
	memberships, err := h.entClient.DirectoryMemberDepartment.Query().
		Where(
			directorymemberdepartment.SourceIDEQ(sourceID),
			directorymemberdepartment.DirectoryMemberIDIn(memberIDs...),
		).
		Order(ent.Asc(directorymemberdepartment.FieldDepartmentExternalID), ent.Asc(directorymemberdepartment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list directory member departments: %w", err)
	}
	for _, membership := range memberships {
		out[membership.DirectoryMemberID] = appendAdminUniqueStrings(out[membership.DirectoryMemberID], membership.DepartmentExternalID)
	}
	return out, nil
}

func adminDirectoryMemberDepartmentIDs(member *ent.DirectoryMember, indexed map[int][]string) []string {
	membershipIDs := indexed[member.ID]
	if len(membershipIDs) > 0 {
		out := make([]string, 0, len(membershipIDs))
		primaryDepartmentID := strings.TrimSpace(member.DepartmentExternalID)
		if primaryDepartmentID != "" && adminStringSliceContains(membershipIDs, primaryDepartmentID) {
			out = append(out, primaryDepartmentID)
		}
		return appendAdminUniqueStrings(out, membershipIDs...)
	}
	departmentID := strings.TrimSpace(member.DepartmentExternalID)
	if departmentID == "" {
		return nil
	}
	return []string{departmentID}
}

func addAdminStringSetValue(sets map[string]map[string]struct{}, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if sets[key] == nil {
		sets[key] = map[string]struct{}{}
	}
	sets[key][value] = struct{}{}
}

func countAdminStringSetUnion(keys []string, sets map[string]map[string]struct{}) int {
	union := map[string]struct{}{}
	for _, key := range keys {
		for value := range sets[key] {
			union[value] = struct{}{}
		}
	}
	return len(union)
}

func appendAdminUniqueStrings(current []string, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || adminStringSliceContains(current, value) {
			continue
		}
		current = append(current, value)
	}
	return current
}

func adminStringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
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
		Q:            strings.TrimSpace(c.Query("q")),
		DepartmentID: strings.TrimSpace(c.Query("department_id")),
		AccessStatus: strings.TrimSpace(c.Query("access_status")),
		Page:         page,
		PageSize:     pageSize,
	}
}
