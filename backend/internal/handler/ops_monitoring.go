package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/opsmonitoring"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// OpsMonitoringHandler exposes the admin-only, read-only Ops Monitoring view.
// It deliberately keeps the HTTP layer thin: every upstream fact and every
// cache decision lives in opsmonitoring.Service, and no handler here mutates
// relay state.
type OpsMonitoringHandler struct {
	service *opsmonitoring.Service
}

func NewOpsMonitoringHandler(service *opsmonitoring.Service) *OpsMonitoringHandler {
	return &OpsMonitoringHandler{service: service}
}

func (h *OpsMonitoringHandler) ListGroups(c *gin.Context) {
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "ops monitoring service is unavailable")
		return
	}
	providerID, ok := parseOpsMonitoringProviderID(c)
	if !ok {
		return
	}
	if _, _, err := opsmonitoring.ParseWindow(c.Query("window")); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	snapshot, err := h.service.Groups(c.Request.Context(), opsmonitoring.GroupsRequest{
		ProviderID: providerID,
		Window:     c.Query("window"),
		Force:      parseOpsMonitoringForce(c),
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, snapshot)
}

func (h *OpsMonitoringHandler) ListGroupAccounts(c *gin.Context) {
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "ops monitoring service is unavailable")
		return
	}
	providerID, ok := parseOpsMonitoringProviderID(c)
	if !ok {
		return
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(c.Param("group_id")), 10, 64)
	if err != nil || groupID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "valid group_id is required")
		return
	}

	snapshot, err := h.service.GroupAccounts(c.Request.Context(), opsmonitoring.GroupAccountsRequest{
		ProviderID: providerID,
		GroupID:    groupID,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, snapshot)
}

func (h *OpsMonitoringHandler) TestProxy(c *gin.Context) {
	if h == nil || h.service == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "ops monitoring service is unavailable")
		return
	}
	providerID, ok := parseOpsMonitoringProviderID(c)
	if !ok {
		return
	}
	proxyID, err := strconv.ParseInt(strings.TrimSpace(c.Param("proxy_id")), 10, 64)
	if err != nil || proxyID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "valid proxy_id is required")
		return
	}

	record, err := h.service.ProxyEgress(c.Request.Context(), opsmonitoring.ProxyEgressRequest{
		ProviderID: providerID,
		ProxyID:    proxyID,
		Force:      parseOpsMonitoringForce(c),
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, record)
}

func parseOpsMonitoringProviderID(c *gin.Context) (int, bool) {
	providerID, err := strconv.Atoi(strings.TrimSpace(c.Query("provider_id")))
	if err != nil || providerID <= 0 {
		pkg.Error(c, http.StatusBadRequest, "valid provider_id is required")
		return 0, false
	}
	return providerID, true
}

// parseOpsMonitoringForce reads the explicit-refresh flag. Only "true" and "1"
// count: anything else keeps the cached read path.
func parseOpsMonitoringForce(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("force"))) {
	case "1", "true":
		return true
	default:
		return false
	}
}
