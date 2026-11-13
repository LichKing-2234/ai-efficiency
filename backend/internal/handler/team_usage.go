package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/kirocredits"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

type teamUsageService interface {
	Scope(context.Context, int) (*teamusage.ScopeResponse, error)
	Subjects(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error)
	SubjectDashboard(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error)
	Summary(context.Context, int, teamusage.OverviewParams) (*teamusage.SummaryResponse, error)
	Trend(context.Context, int, teamusage.OverviewParams) (*teamusage.TrendResponse, error)
	Members(context.Context, int, teamusage.MembersParams) (*teamusage.MembersResponse, error)
	Organization(context.Context, int, teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error)
	UpdateMultiplier(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error)
	ListAudit(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error)
	ListAdminAudit(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error)
}

type representativeScopeResolver interface {
	Resolve(context.Context, int) (*representativescope.Scope, error)
}

type TeamUsageHandler struct {
	service       teamUsageService
	scopeResolver representativeScopeResolver
	credits       *kirocredits.Service
}

func NewTeamUsageHandler(service teamUsageService) *TeamUsageHandler {
	return &TeamUsageHandler{service: service}
}

// NewTeamUsageHandlerWithCredits adds the read-only kiro2cc credits surface.
func NewTeamUsageHandlerWithCredits(service teamUsageService, scopeResolver representativeScopeResolver, credits *kirocredits.Service) *TeamUsageHandler {
	return &TeamUsageHandler{service: service, scopeResolver: scopeResolver, credits: credits}
}

type teamCreditsDepartment struct {
	DepartmentExternalID string  `json:"department_external_id"`
	Credits              float64 `json:"credits"`
	Requests             int64   `json:"requests"`
}

type teamCreditsResponse struct {
	kirocredits.TeamCredits
	Departments []teamCreditsDepartment `json:"departments"`
}

func (h *TeamUsageHandler) scopeCreditsRefs(scope *representativescope.Scope, actorID int) ([]kirocredits.UserRef, map[int]representativescope.Subject) {
	refs := make([]kirocredits.UserRef, 0, len(scope.Subjects))
	subjectsByUser := make(map[int]representativescope.Subject, len(scope.Subjects))
	for _, subject := range scope.Subjects {
		if subject.UserID <= 0 || subject.UserID == actorID || subject.RelayUserID == nil {
			continue
		}
		refs = append(refs, kirocredits.UserRef{UserID: subject.UserID, RelayUserID: int64(*subject.RelayUserID)})
		subjectsByUser[subject.UserID] = subject
	}
	return refs, subjectsByUser
}

func buildTeamCreditsResponse(scope *representativescope.Scope, batch kirocredits.TeamCredits, subjectsByUser map[int]representativescope.Subject) teamCreditsResponse {
	response := teamCreditsResponse{TeamCredits: batch, Departments: []teamCreditsDepartment{}}
	if scope == nil {
		return response
	}
	creditsByUser := make(map[int64]kirocredits.UserCredits, len(batch.Users))
	for _, user := range batch.Users {
		creditsByUser[user.RelayUserID] = user
	}
	direct := map[string]teamCreditsDepartment{}
	for _, subject := range subjectsByUser {
		if subject.RelayUserID == nil {
			continue
		}
		entry, ok := creditsByUser[int64(*subject.RelayUserID)]
		if !ok {
			continue
		}
		departmentID := strings.TrimSpace(subject.DepartmentExternalID)
		if departmentID == "" {
			continue
		}
		bucket := direct[departmentID]
		bucket.DepartmentExternalID = departmentID
		bucket.Credits += entry.Credits
		bucket.Requests += entry.Requests
		direct[departmentID] = bucket
	}
	for _, bucket := range direct {
		response.Departments = append(response.Departments, bucket)
	}
	// Represented roots carry the subtree total so the organization view can
	// render department badges that match the subtree token numbers.
	for _, root := range scope.Departments {
		subtree, ok := scope.RepresentedSubtreeIDs[root.ExternalID]
		if !ok {
			continue
		}
		total := teamCreditsDepartment{DepartmentExternalID: root.ExternalID}
		for _, subject := range subjectsByUser {
			if subject.RelayUserID == nil {
				continue
			}
			if _, ok := subtree[subject.DepartmentExternalID]; !ok {
				continue
			}
			entry, found := creditsByUser[int64(*subject.RelayUserID)]
			if !found {
				continue
			}
			total.Credits += entry.Credits
			total.Requests += entry.Requests
		}
		replaced := false
		for index := range response.Departments {
			if response.Departments[index].DepartmentExternalID == root.ExternalID {
				response.Departments[index] = total
				replaced = true
				break
			}
		}
		if !replaced {
			response.Departments = append(response.Departments, total)
		}
	}
	sort.Slice(response.Departments, func(i, j int) bool {
		return response.Departments[i].DepartmentExternalID < response.Departments[j].DepartmentExternalID
	})
	return response
}

type teamUsageProviderResolverFunc func(context.Context, int) (relay.Provider, error)

func (f teamUsageProviderResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

func newTeamUsageService(entClient *ent.Client, sqlDB *sql.DB, providerHandler *ProviderHandler, scopeCache *representativescope.Cache, snapshotCache *teamusage.SnapshotCache, originCache *teamusage.OriginCache, prewarmReader *teamusage.PrewarmReader, memberCursorSecret string) (*teamusage.Service, error) {
	resolver := teamUsageProviderResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return nil, teamusage.ErrProviderUnsupported
	})
	if providerHandler != nil {
		resolver = providerHandler.Resolve
	}
	return teamusage.NewService(
		entClient,
		representativescope.NewWithCache(entClient, scopeCache),
		resolver,
		teamusage.NewPostgresAdvisoryLocker(sqlDB),
		teamusage.ServiceOptions{SnapshotCache: snapshotCache, OriginCache: originCache, PrewarmReader: prewarmReader, CursorSecret: memberCursorSecret},
	)
}

func (h *TeamUsageHandler) Scope(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := h.service.Scope(c.Request.Context(), uc.UserID)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Subjects(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := h.service.Subjects(
		c.Request.Context(),
		uc.UserID,
		strings.TrimSpace(c.Query("q")),
		parseOptionalInt(c.DefaultQuery("page", "1")),
		parseOptionalInt(c.DefaultQuery("page_size", "20")),
	)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) SubjectDashboard(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID, ok := parseRequiredIntPathParam(c, "user_id")
	if !ok {
		return
	}
	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}

	resp, err := h.service.SubjectDashboard(c.Request.Context(), uc.UserID, targetUserID, params)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Summary(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dashboardParams, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	resp, err := h.service.Summary(c.Request.Context(), uc.UserID, teamusage.OverviewParams{
		StartDate: dashboardParams.StartDate, EndDate: dashboardParams.EndDate,
		Granularity: dashboardParams.Granularity, Timezone: dashboardParams.Timezone,
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	resp.RequestID = telemetry.RequestID(c.Request.Context())
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Trend(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dashboardParams, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	resp, err := h.service.Trend(c.Request.Context(), uc.UserID, teamusage.OverviewParams{
		StartDate: dashboardParams.StartDate, EndDate: dashboardParams.EndDate,
		Granularity: dashboardParams.Granularity, Timezone: dashboardParams.Timezone,
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	resp.RequestID = telemetry.RequestID(c.Request.Context())
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Members(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dashboardParams, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	limit, ok := parseOptionalIntQueryParam(c, "limit")
	if !ok {
		return
	}
	limitValue := 0
	if limit != nil {
		limitValue = *limit
	}
	resp, err := h.service.Members(c.Request.Context(), uc.UserID, teamusage.MembersParams{
		OverviewParams: teamusage.OverviewParams{
			StartDate: dashboardParams.StartDate, EndDate: dashboardParams.EndDate,
			Granularity: dashboardParams.Granularity, Timezone: dashboardParams.Timezone,
		},
		Cursor: strings.TrimSpace(c.Query("cursor")),
		Limit:  limitValue,
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	resp.RequestID = telemetry.RequestID(c.Request.Context())
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Organization(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dashboardParams, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	departmentLimit, ok := parseOptionalIntQueryParam(c, "department_limit")
	if !ok {
		return
	}
	memberLimit, ok := parseOptionalIntQueryParam(c, "member_limit")
	if !ok {
		return
	}
	departmentLimitValue := 0
	if departmentLimit != nil {
		departmentLimitValue = *departmentLimit
	}
	memberLimitValue := 0
	if memberLimit != nil {
		memberLimitValue = *memberLimit
	}
	resp, err := h.service.Organization(c.Request.Context(), uc.UserID, teamusage.OrganizationParams{
		OverviewParams: teamusage.OverviewParams{
			StartDate: dashboardParams.StartDate, EndDate: dashboardParams.EndDate,
			Granularity: dashboardParams.Granularity, Timezone: dashboardParams.Timezone,
		},
		ParentDepartmentExternalID: strings.TrimSpace(c.Query("parent_department_external_id")),
		DepartmentCursor:           strings.TrimSpace(c.Query("department_cursor")),
		DepartmentLimit:            departmentLimitValue,
		MemberCursor:               strings.TrimSpace(c.Query("member_cursor")),
		MemberLimit:                memberLimitValue,
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	resp.RequestID = telemetry.RequestID(c.Request.Context())
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) UpdateMultiplier(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID, ok := parseRequiredIntPathParam(c, "user_id")
	if !ok {
		return
	}
	groupID, ok := parseRequiredInt64PathParam(c, "group_id")
	if !ok {
		return
	}

	var req teamusage.UpdateMultiplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.UpdateMultiplier(c.Request.Context(), uc.UserID, targetUserID, groupID, req)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) Audit(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID, ok := parseOptionalIntQueryParam(c, "target_user_id")
	if !ok {
		return
	}

	resp, err := h.service.ListAudit(c.Request.Context(), uc.UserID, teamusage.AuditListParams{
		Page:         parseOptionalInt(c.DefaultQuery("page", "1")),
		PageSize:     parseOptionalInt(c.DefaultQuery("page_size", "20")),
		TargetUserID: targetUserID,
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func (h *TeamUsageHandler) AdminAudit(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	actorUserID, ok := parseOptionalIntQueryParam(c, "actor_user_id")
	if !ok {
		return
	}
	targetUserID, ok := parseOptionalIntQueryParam(c, "target_user_id")
	if !ok {
		return
	}

	resp, err := h.service.ListAdminAudit(c.Request.Context(), teamusage.AdminAuditListParams{
		Page:         parseOptionalInt(c.DefaultQuery("page", "1")),
		PageSize:     parseOptionalInt(c.DefaultQuery("page_size", "20")),
		ActorUserID:  actorUserID,
		TargetUserID: targetUserID,
		Status:       strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	pkg.Success(c, resp)
}

func parseRequiredIntPathParam(c *gin.Context, name string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(c.Param(name)))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, name+" must be an integer")
		return 0, false
	}
	return value, true
}

func parseRequiredInt64PathParam(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, name+" must be an integer")
		return 0, false
	}
	return value, true
}

func parseOptionalIntQueryParam(c *gin.Context, name string) (*int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, name+" must be an integer")
		return nil, false
	}
	return &value, true
}

func writeTeamUsageError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	if status, message, ok := teamUsageForbiddenErrorStatus(err); ok {
		pkg.Error(c, status, message)
		return
	}

	switch {
	case errors.Is(err, teamusage.ErrInvalidMemberCursor):
		pkg.Error(c, http.StatusBadRequest, teamusage.ErrInvalidMemberCursor.Error())
	case errors.Is(err, teamusage.ErrMemberSnapshotExpired):
		pkg.Error(c, http.StatusConflict, teamusage.ErrMemberSnapshotExpired.Error())
	case errors.Is(err, teamusage.ErrInvalidOverviewParams):
		pkg.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, teamusage.ErrNotRepresentative), errors.Is(err, teamusage.ErrSelfEditForbidden), errors.Is(err, teamusage.ErrNotUpperLevelRepresentative):
		pkg.Error(c, http.StatusForbidden, err.Error())
	case errors.Is(err, teamusage.ErrOutOfScope):
		pkg.Error(c, http.StatusNotFound, "target is not available")
	case errors.Is(err, teamusage.ErrNoRelayMapping), errors.Is(err, teamusage.ErrInactiveSubscription):
		pkg.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, teamusage.ErrPartialFailed):
		pkg.Error(c, http.StatusBadGateway, "rate multiplier update could not be verified")
	case errors.Is(err, teamusage.ErrPolicyDenied), errors.Is(err, teamusage.ErrInvalidMultiplier), errors.Is(err, teamusage.ErrInvalidMultiplierPrecision), errors.Is(err, teamusage.ErrMultiplierAboveMaximum):
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, teamusage.ErrProviderUnsupported):
		pkg.Error(c, http.StatusServiceUnavailable, err.Error())
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}

func teamUsageForbiddenErrorStatus(err error) (int, string, bool) {
	var forbidden *teamusage.ForbiddenError
	if !errors.As(err, &forbidden) || forbidden == nil {
		return 0, "", false
	}

	switch strings.TrimSpace(forbidden.Reason) {
	case teamusage.ErrNotRepresentative.Error(), teamusage.ErrSelfEditForbidden.Error(), teamusage.ErrNotUpperLevelRepresentative.Error():
		return http.StatusForbidden, forbidden.Error(), true
	case teamusage.ErrOutOfScope.Error():
		return http.StatusNotFound, "target is not available", true
	case teamusage.ErrNoRelayMapping.Error(), teamusage.ErrInactiveSubscription.Error():
		return http.StatusConflict, forbidden.Error(), true
	case teamusage.ErrPolicyDenied.Error(), teamusage.ErrInvalidMultiplier.Error(), teamusage.ErrInvalidMultiplierPrecision.Error(), teamusage.ErrMultiplierAboveMaximum.Error():
		return http.StatusUnprocessableEntity, forbidden.Error(), true
	case teamusage.ErrProviderUnsupported.Error():
		return http.StatusServiceUnavailable, forbidden.Error(), true
	default:
		return http.StatusForbidden, forbidden.Error(), true
	}
}

// Credits returns read-only kiro2cc credits for the current representative
// scope. Missing data is expressed in the payload; it never fails the page.
func (h *TeamUsageHandler) Credits(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h == nil || h.scopeResolver == nil || h.credits == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "team credits service is unavailable")
		return
	}
	scope, err := h.scopeResolver.Resolve(c.Request.Context(), uc.UserID)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	if scope == nil || !scope.IsRepresentative {
		pkg.Error(c, http.StatusForbidden, "representative scope is required")
		return
	}
	refs, subjectsByUser := h.scopeCreditsRefs(scope, uc.UserID)
	batch := h.credits.Batch(c.Request.Context(), refs, kirocredits.Params{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	})
	pkg.Success(c, buildTeamCreditsResponse(scope, batch, subjectsByUser))
}

// SubjectCredits returns credits for one member inside the representative scope.
func (h *TeamUsageHandler) SubjectCredits(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	targetUserID, ok := parseRequiredIntPathParam(c, "user_id")
	if !ok {
		return
	}
	if h == nil || h.scopeResolver == nil || h.credits == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "team credits service is unavailable")
		return
	}
	scope, err := h.scopeResolver.Resolve(c.Request.Context(), uc.UserID)
	if err != nil {
		writeTeamUsageError(c, err)
		return
	}
	if scope == nil || !scope.IsRepresentative {
		pkg.Error(c, http.StatusForbidden, "representative scope is required")
		return
	}
	var target *representativescope.Subject
	for index := range scope.Subjects {
		subject := scope.Subjects[index]
		if subject.UserID == targetUserID && subject.UserID != uc.UserID {
			target = &scope.Subjects[index]
			break
		}
	}
	if target == nil {
		pkg.Error(c, http.StatusForbidden, "member is outside the representative scope")
		return
	}
	if target.RelayUserID == nil || *target.RelayUserID <= 0 {
		pkg.Success(c, kirocredits.TeamCredits{
			AsOf:    time.Now().UTC(),
			Status:  kirocredits.StatusUnlinked,
			Message: "该成员未关联 AI 服务用户",
			Users:   []kirocredits.UserCredits{},
			ByModel: []kirocredits.ModelCredits{},
			ByDay:   []kirocredits.DayCredits{},
		})
		return
	}
	batch := h.credits.Batch(c.Request.Context(), []kirocredits.UserRef{{
		UserID:      target.UserID,
		RelayUserID: int64(*target.RelayUserID),
	}}, kirocredits.Params{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	})
	pkg.Success(c, batch)
}
