package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type UserUsageHandler struct {
	service *personalusage.Service
}

func NewUserUsageHandler(service *personalusage.Service) *UserUsageHandler {
	return &UserUsageHandler{service: service}
}

func (h *UserUsageHandler) Dashboard(c *gin.Context) {
	userContext := auth.GetUserContext(c)
	if userContext == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	includeGroupQuotas, ok := parseIncludeGroupQuotas(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "personal usage service is unavailable")
		return
	}

	snapshot, err := h.service.Dashboard(c.Request.Context(), personalusage.Request{
		UserID:             userContext.UserID,
		Params:             params,
		IncludeGroupQuotas: includeGroupQuotas,
	})
	if err != nil {
		writeUserUsageError(c, "get usage dashboard", err)
		return
	}
	pkg.Success(c, snapshot)
}

func (h *UserUsageHandler) GroupQuotas(c *gin.Context) {
	userContext := auth.GetUserContext(c)
	if userContext == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "personal usage service is unavailable")
		return
	}

	response, err := h.service.GroupQuotas(c.Request.Context(), personalusage.Request{
		UserID: userContext.UserID,
		Params: params,
	})
	if err != nil {
		writeUserUsageError(c, "get usage group quotas", err)
		return
	}
	pkg.Success(c, response)
}

func (h *UserUsageHandler) GroupPoolUsage(c *gin.Context) {
	userContext := auth.GetUserContext(c)
	if userContext == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "personal usage service is unavailable")
		return
	}

	response, err := h.service.GroupPoolUsage(c.Request.Context(), personalusage.Request{
		UserID: userContext.UserID,
		Params: params,
	})
	if err != nil {
		writeUserUsageError(c, "get usage group pool usage", err)
		return
	}
	pkg.Success(c, response)
}

func parseIncludeGroupQuotas(c *gin.Context) (bool, bool) {
	raw, exists := c.GetQuery("include_group_quotas")
	if !exists {
		return true, true
	}
	include, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "include_group_quotas must be a boolean")
		return false, false
	}
	return include, true
}

func writeUserUsageError(c *gin.Context, operation string, err error) {
	if errors.Is(err, relay.ErrInvalidCredentials) {
		pkg.Error(c, http.StatusConflict, "Relay credentials need attention. Please update AI service configuration.")
		return
	}
	if errors.Is(err, personalusage.ErrConfiguration) {
		pkg.Error(c, http.StatusUnprocessableEntity, operation+": "+err.Error())
		return
	}
	pkg.Error(c, http.StatusBadGateway, operation+": "+err.Error())
}

func parseUserUsageDashboardParams(c *gin.Context) (relay.UserUsageDashboardParams, bool) {
	granularity := strings.TrimSpace(c.DefaultQuery("granularity", "day"))
	if granularity != "day" && granularity != "hour" {
		pkg.Error(c, http.StatusBadRequest, "granularity must be day or hour")
		return relay.UserUsageDashboardParams{}, false
	}

	params := relay.UserUsageDashboardParams{
		StartDate:   strings.TrimSpace(c.Query("start_date")),
		EndDate:     strings.TrimSpace(c.Query("end_date")),
		Granularity: granularity,
		Timezone:    strings.TrimSpace(c.Query("timezone")),
	}
	if params.StartDate == "" || params.EndDate == "" {
		start, end := defaultUserUsageRange(time.Now())
		if params.StartDate == "" {
			params.StartDate = start
		}
		if params.EndDate == "" {
			params.EndDate = end
		}
	}
	return params, true
}

func defaultUserUsageRange(now time.Time) (string, string) {
	today := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -6).Format("2006-01-02")
	return start, today
}
