package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type activityService interface {
	Scope(context.Context, int) (*activity.ScopeResponse, error)
	Members(context.Context, int, activity.Window, activity.PageOptions) (*activity.MembersActivity, error)
	Member(context.Context, int, int, activity.Window, activity.DetailPageOptions) (*activity.MemberActivity, error)
	Team(context.Context, int, string, activity.Window, activity.PageOptions) (*activity.TeamActivity, error)
	Repository(context.Context, int, int, activity.Window, activity.RepositoryPageOptions) (*activity.RepositoryActivity, error)
	Bucket(context.Context, int, string) (*activity.BucketDetail, error)
}

type activityV2Service interface {
	V2Overview(context.Context, int, activity.V2Query) (*activity.V2Overview, error)
	V2Repositories(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2RepositoryRow], error)
	V2PullRequests(context.Context, int, activity.V2PageQuery) (*activity.V2Page[activity.V2PullRequestRow], error)
}

type ActivityHandler struct {
	service activityService
}

func NewActivityHandler(service activityService) *ActivityHandler {
	return &ActivityHandler{service: service}
}

func RegisterActivityRoutes(group *gin.RouterGroup, handler *ActivityHandler) {
	group.GET("/scope", handler.Scope)
	group.GET("/summary", handler.Summary)
	group.GET("/members", handler.Members)
	group.GET("/members/:user_id", handler.Member)
	group.GET("/teams/:team_id", handler.Team)
	group.GET("/repos/:repo_id", handler.Repository)
	group.GET("/buckets/:bucket_id", handler.Bucket)
	group.GET("/v2/overview", handler.V2Overview)
	group.GET("/v2/repositories", handler.V2Repositories)
	group.GET("/v2/pull-requests", handler.V2PullRequests)
}

func (h *ActivityHandler) V2Overview(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	service, ok := h.service.(activityV2Service)
	if !ok {
		pkg.Error(c, http.StatusServiceUnavailable, "Activity v2 is unavailable")
		return
	}
	result, err := service.V2Overview(c.Request.Context(), actor, query)
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) V2Repositories(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	service, ok := h.service.(activityV2Service)
	if !ok {
		pkg.Error(c, http.StatusServiceUnavailable, "Activity v2 is unavailable")
		return
	}
	result, err := service.V2Repositories(c.Request.Context(), actor, activity.V2PageQuery{V2Query: query, Search: strings.TrimSpace(c.Query("search")), Sort: strings.TrimSpace(c.DefaultQuery("sort", "tokens")), Cursor: strings.TrimSpace(c.Query("cursor"))})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) V2PullRequests(c *gin.Context) {
	actor, query, ok := parseV2ActivityQuery(c)
	if !ok {
		return
	}
	service, ok := h.service.(activityV2Service)
	if !ok {
		pkg.Error(c, http.StatusServiceUnavailable, "Activity v2 is unavailable")
		return
	}
	result, err := service.V2PullRequests(c.Request.Context(), actor, activity.V2PageQuery{V2Query: query, Search: strings.TrimSpace(c.Query("search")), Sort: strings.TrimSpace(c.DefaultQuery("sort", "tokens")), Cursor: strings.TrimSpace(c.Query("cursor"))})
	writeActivityResult(c, result, err)
}

func parseV2ActivityQuery(c *gin.Context) (int, activity.V2Query, bool) {
	actor, ok := activityActor(c)
	if !ok {
		return 0, activity.V2Query{}, false
	}
	query := activity.V2Query{Scope: activity.V2ScopeKind(strings.TrimSpace(c.DefaultQuery("scope", "personal"))), TeamID: strings.TrimSpace(c.Query("team_id")), FromDate: strings.TrimSpace(c.Query("from")), ToDate: strings.TrimSpace(c.Query("to")), Timezone: strings.TrimSpace(c.Query("timezone"))}
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

func (h *ActivityHandler) Scope(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	result, err := h.service.Scope(c.Request.Context(), actor)
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Summary(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	window, ok := parseActivityWindow(c)
	if !ok {
		return
	}
	pages, ok := parseActivityDetailPages(c, 20)
	if !ok {
		return
	}
	result, err := h.service.Member(c.Request.Context(), actor, actor, window, pages)
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Members(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	window, ok := parseActivityWindow(c)
	if !ok {
		return
	}
	limit, ok := parseActivityLimit(c, "limit", 50)
	if !ok {
		return
	}
	result, err := h.service.Members(c.Request.Context(), actor, window, activity.PageOptions{Limit: limit, Cursor: strings.TrimSpace(c.Query("cursor"))})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Member(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	target, ok := parseRequiredIntPathParam(c, "user_id")
	if !ok {
		return
	}
	window, ok := parseActivityWindow(c)
	if !ok {
		return
	}
	pages, ok := parseActivityDetailPages(c, 20)
	if !ok {
		return
	}
	result, err := h.service.Member(c.Request.Context(), actor, target, window, pages)
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Team(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	window, ok := parseActivityWindow(c)
	if !ok {
		return
	}
	limit, ok := parseActivityLimit(c, "limit", 50)
	if !ok {
		return
	}
	result, err := h.service.Team(c.Request.Context(), actor, strings.TrimSpace(c.Param("team_id")), window, activity.PageOptions{Limit: limit, Cursor: strings.TrimSpace(c.Query("cursor"))})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Repository(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	repoID, ok := parseRequiredIntPathParam(c, "repo_id")
	if !ok {
		return
	}
	window, ok := parseActivityWindow(c)
	if !ok {
		return
	}
	memberLimit, ok := parseActivityLimit(c, "member_limit", 50)
	if !ok {
		return
	}
	prLimit, ok := parseActivityLimit(c, "pr_limit", 20)
	if !ok {
		return
	}
	commitLimit, ok := parseActivityLimit(c, "commit_limit", 20)
	if !ok {
		return
	}
	result, err := h.service.Repository(c.Request.Context(), actor, repoID, window, activity.RepositoryPageOptions{
		MemberLimit: memberLimit, MemberCursor: strings.TrimSpace(c.Query("member_cursor")),
		PRLimit: prLimit, PRCursor: strings.TrimSpace(c.Query("pr_cursor")),
		CommitLimit: commitLimit, CommitCursor: strings.TrimSpace(c.Query("commit_cursor")),
	})
	writeActivityResult(c, result, err)
}

func (h *ActivityHandler) Bucket(c *gin.Context) {
	actor, ok := activityActor(c)
	if !ok {
		return
	}
	result, err := h.service.Bucket(c.Request.Context(), actor, strings.TrimSpace(c.Param("bucket_id")))
	writeActivityResult(c, result, err)
}

func activityActor(c *gin.Context) (int, bool) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return 0, false
	}
	return uc.UserID, true
}

func parseActivityWindow(c *gin.Context) (activity.Window, bool) {
	now := time.Now().UTC()
	to := now
	from := time.Time{}
	var err error
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "invalid to")
			return activity.Window{}, false
		}
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "invalid from")
			return activity.Window{}, false
		}
	} else {
		from = to.Add(-30 * 24 * time.Hour)
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		pkg.Error(c, http.StatusBadRequest, "invalid activity window")
		return activity.Window{}, false
	}
	return activity.Window{From: from.UTC(), To: to.UTC()}, true
}

func parseActivityDetailPages(c *gin.Context, defaultLimit int) (activity.DetailPageOptions, bool) {
	prLimit, ok := parseActivityLimit(c, "pr_limit", defaultLimit)
	if !ok {
		return activity.DetailPageOptions{}, false
	}
	commitLimit, ok := parseActivityLimit(c, "commit_limit", defaultLimit)
	if !ok {
		return activity.DetailPageOptions{}, false
	}
	bucketLimit, ok := parseActivityLimit(c, "bucket_limit", defaultLimit)
	if !ok {
		return activity.DetailPageOptions{}, false
	}
	return activity.DetailPageOptions{
		PRLimit: prLimit, PRCursor: strings.TrimSpace(c.Query("pr_cursor")),
		CommitLimit: commitLimit, CommitCursor: strings.TrimSpace(c.Query("commit_cursor")),
		BucketLimit: bucketLimit, BucketCursor: strings.TrimSpace(c.Query("bucket_cursor")),
	}, true
}

func parseActivityLimit(c *gin.Context, name string, defaultValue int) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 100 {
		pkg.Error(c, http.StatusBadRequest, name+" must be between 1 and 100")
		return 0, false
	}
	return value, true
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
