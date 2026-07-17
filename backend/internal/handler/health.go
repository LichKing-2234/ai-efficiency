package handler

import (
	"errors"
	"net/http"

	"github.com/ai-efficiency/backend/internal/health"
	"github.com/ai-efficiency/backend/internal/versioncheck"
	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	health       *health.Service
	versionCheck *versioncheck.Service
}

func NewHealthHandler(health *health.Service, versionChecks ...*versioncheck.Service) *HealthHandler {
	var versionCheck *versioncheck.Service
	if len(versionChecks) > 0 {
		versionCheck = versionChecks[0]
	}
	return &HealthHandler{
		health:       health,
		versionCheck: versionCheck,
	}
}

func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, h.health.Live())
}

func (h *HealthHandler) Ready(c *gin.Context) {
	report := h.health.Ready(c.Request.Context())
	status := http.StatusOK
	if report.Status == "not_ready" {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, report)
}

func (h *HealthHandler) Version(c *gin.Context) {
	if h.versionCheck == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "version check is not configured"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": h.versionCheck.Status()})
}

func (h *HealthHandler) CheckVersion(c *gin.Context) {
	if h.versionCheck == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "version check is not configured"})
		return
	}
	status, err := h.versionCheck.CheckForUpdate(c.Request.Context())
	if err != nil {
		if errors.Is(err, versioncheck.ErrCheckDisabled) {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": status})
}
