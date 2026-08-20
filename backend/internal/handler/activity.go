package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type activityV2Service interface {
	V2Overview(context.Context, int, activity.V2Query) (*activity.V2Overview, error)
	V2Repositories(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2RepositoryRow], error)
	V2PullRequests(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2PullRequestRow], error)
	V2TeamMemberAvailability(context.Context, int, activity.V2TeamMemberAvailabilityQuery) (*activity.V2TeamMemberAvailability, error)
}

type ActivityHandler struct {
	service activityV2Service
}

func NewActivityHandler(service activityV2Service) *ActivityHandler {
	return &ActivityHandler{service: service}
}

func RegisterActivityRoutes(group *gin.RouterGroup, handler *ActivityHandler) {
	group.GET("/v2/overview", handler.V2Overview)
	group.GET("/v2/repositories", handler.V2Repositories)
	group.GET("/v2/pull-requests", handler.V2PullRequests)
	group.GET("/v2/teams/:team_id/member-availability", handler.V2TeamMemberAvailability)
}

func (h *ActivityHandler) V2Overview(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	result, err := h.service.V2Overview(c.Request.Context(), actor, query)
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) V2Repositories(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	result, err := h.service.V2Repositories(c.Request.Context(), actor, activity.V2PageQuery{
		V2Query: query, Search: strings.TrimSpace(c.Query("search")),
		Sort: strings.TrimSpace(c.DefaultQuery("sort", "tokens")), Cursor: strings.TrimSpace(c.Query("cursor")),
	})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) V2PullRequests(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	result, err := h.service.V2PullRequests(c.Request.Context(), actor, activity.V2PageQuery{
		V2Query: query, Search: strings.TrimSpace(c.Query("search")),
		Sort: strings.TrimSpace(c.DefaultQuery("sort", "tokens")), Cursor: strings.TrimSpace(c.Query("cursor")),
	})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) V2TeamMemberAvailability(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	userIDs, ok := parseActivityUserIDs(c.Query("user_ids"))
	if !ok {
		pkg.Error(c, http.StatusBadRequest, "invalid user_ids")
		return
	}
	result, err := h.service.V2TeamMemberAvailability(c.Request.Context(), actor, activity.V2TeamMemberAvailabilityQuery{
		TeamID: strings.TrimSpace(c.Param("team_id")), FromDate: strings.TrimSpace(c.Query("from")),
		ToDate: strings.TrimSpace(c.Query("to")), Timezone: strings.TrimSpace(c.Query("timezone")), UserIDs: userIDs,
	})
	writeActivityResult(c, result, err)
}

func parseV2ActivityQuery(c *gin.Context) (int, activity.V2Query, bool) {
	actor, ok := activityActor(c)
	if !ok {
		return 0, activity.V2Query{}, false
	}
	query := activity.V2Query{
		Scope:  activity.V2ScopeKind(strings.TrimSpace(c.DefaultQuery("scope", "personal"))),
		TeamID: strings.TrimSpace(c.Query("team_id")), FromDate: strings.TrimSpace(c.Query("from")),
		ToDate: strings.TrimSpace(c.Query("to")), Timezone: strings.TrimSpace(c.Query("timezone")),
	}
	for name, target := range map[string]*int{"subject_user_id": &query.SubjectID, "repo_id": &query.RepoID, "pr_record_id": &query.PRRecordID} {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			pkg.Error(c, http.StatusBadRequest, "invalid "+name)
			return 0, activity.V2Query{}, false
		}
		*target = value
	}
	if query.FromDate == "" || query.ToDate == "" || query.Timezone == "" {
		pkg.Error(c, http.StatusBadRequest, "from, to, and timezone are required")
		return 0, activity.V2Query{}, false
	}
	return actor, query, true
}

func parseActivityUserIDs(raw string) ([]int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []int{}, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 100 {
		return nil, false
	}
	seen, result := map[int]struct{}{}, make([]int, 0, len(parts))
	for _, part := range parts {
		userID, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || userID <= 0 {
			return nil, false
		}
		if _, duplicate := seen[userID]; duplicate {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	return result, true
}

func activityActor(c *gin.Context) (int, bool) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return 0, false
	}
	return uc.UserID, true
}

func writeActivityResult(c *gin.Context, result any, err error) {
	if err == nil {
		pkg.Success(c, result)
		return
	}
	switch {
	case errors.Is(err, activity.ErrForbidden):
		pkg.Error(c, http.StatusForbidden, "forbidden")
	case errors.Is(err, activity.ErrInvalidCursor):
		pkg.Error(c, http.StatusBadRequest, "invalid_cursor")
	case errors.Is(err, activity.ErrSnapshotExpired):
		pkg.Error(c, http.StatusConflict, "snapshot_expired")
	case errors.Is(err, activity.ErrInvalidQuery):
		pkg.Error(c, http.StatusBadRequest, "invalid_activity_query")
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}
