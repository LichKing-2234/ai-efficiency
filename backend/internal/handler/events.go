package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/toolusage"
	"github.com/gin-gonic/gin"
)

type EventsHandler struct {
	service *toolusage.QueryService
}

func NewEventsHandler(service *toolusage.QueryService) *EventsHandler {
	return &EventsHandler{service: service}
}

func (h *EventsHandler) Summary(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	req, err := parseEventsSummaryRequest(c, uc)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	summary, err := h.service.GetSummary(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, summary)
}

func (h *EventsHandler) List(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	req, err := parseEventsListRequest(c, uc)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	rows, total, err := h.service.ListEvents(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !isAdminRole(uc.Role) {
		for i := range rows {
			rows[i].Username = ""
		}
	}

	pkg.Success(c, gin.H{
		"items":     rows,
		"total":     total,
		"page":      req.Offset / maxInt(req.Limit, 1),
		"page_size": req.Limit,
	})
}

func (h *EventsHandler) Get(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	detail, err := h.service.GetEventDetail(c.Request.Context(), toolusage.GetEventDetailRequest{
		ActorUserID: uc.UserID,
		ActorRole:   uc.Role,
		EventID:     id,
	})
	if err != nil {
		switch {
		case errors.Is(err, toolusage.ErrUsageEventForbidden):
			pkg.Error(c, http.StatusForbidden, err.Error())
		case ent.IsNotFound(err):
			pkg.Error(c, http.StatusNotFound, "event not found")
		default:
			pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		}
		return
	}
	pkg.Success(c, detail)
}

func (h *EventsHandler) Users(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !isAdminRole(uc.Role) {
		pkg.Error(c, http.StatusForbidden, "admin access required")
		return
	}

	limit := parseOptionalInt(c.DefaultQuery("limit", "20"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	users, err := h.service.SearchEventUsers(c.Request.Context(), toolusage.EventUserSearchRequest{
		Q:     strings.TrimSpace(c.Query("q")),
		Limit: limit,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, users)
}

func parseEventsSummaryRequest(c *gin.Context, uc *auth.UserContext) (toolusage.SummaryRequest, error) {
	from, to, err := parseEventsTimeWindow(c)
	if err != nil {
		return toolusage.SummaryRequest{}, err
	}
	userID, err := parseOptionalUserID(c, uc)
	if err != nil {
		return toolusage.SummaryRequest{}, err
	}
	return toolusage.SummaryRequest{
		ActorUserID:   uc.UserID,
		ActorRole:     uc.Role,
		From:          from,
		To:            to,
		Tool:          strings.TrimSpace(c.Query("tool")),
		RepoID:        parseOptionalInt(c.Query("repo_id")),
		BindingStatus: strings.TrimSpace(c.Query("binding_status")),
		UserID:        userID,
		Q:             strings.TrimSpace(c.Query("q")),
	}, nil
}

func parseEventsListRequest(c *gin.Context, uc *auth.UserContext) (toolusage.ListEventsRequest, error) {
	from, to, err := parseEventsTimeWindow(c)
	if err != nil {
		return toolusage.ListEventsRequest{}, err
	}
	userID, err := parseOptionalUserID(c, uc)
	if err != nil {
		return toolusage.ListEventsRequest{}, err
	}
	limit := parseOptionalInt(c.DefaultQuery("limit", strconv.Itoa(toolusage.DefaultEventPageSize)))
	if limit <= 0 {
		limit = toolusage.DefaultEventPageSize
	} else if limit > toolusage.MaxEventPageSize {
		limit = toolusage.MaxEventPageSize
	}
	offset := parseOptionalInt(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	return toolusage.ListEventsRequest{
		ActorUserID:   uc.UserID,
		ActorRole:     uc.Role,
		From:          from,
		To:            to,
		Tool:          strings.TrimSpace(c.Query("tool")),
		RepoID:        parseOptionalInt(c.Query("repo_id")),
		BindingStatus: strings.TrimSpace(c.Query("binding_status")),
		UserID:        userID,
		Q:             strings.TrimSpace(c.Query("q")),
		Limit:         limit,
		Offset:        offset,
	}, nil
}

func parseEventsTimeWindow(c *gin.Context) (time.Time, time.Time, error) {
	var from, to time.Time
	var err error

	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	return from, to, nil
}

func parseOptionalUserID(c *gin.Context, uc *auth.UserContext) (int, error) {
	if !isAdminRole(uc.Role) {
		return 0, nil
	}
	if raw := strings.TrimSpace(c.Query("user_id")); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, nil
}

func parseOptionalInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

func isAdminRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = sql.Dialect("")
