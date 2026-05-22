package handler

import (
	"net/http"

	"github.com/ai-efficiency/backend/internal/health"
	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	health *health.Service
}

func NewHealthHandler(health *health.Service) *HealthHandler {
	return &HealthHandler{health: health}
}

func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, h.health.Live())
}

func (h *HealthHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, h.health.Ready(c.Request.Context()))
}
