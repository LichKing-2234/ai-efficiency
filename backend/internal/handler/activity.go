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
	default:
		pkg.Error(c, http.StatusInternalServerError, err.Error())
	}
}
