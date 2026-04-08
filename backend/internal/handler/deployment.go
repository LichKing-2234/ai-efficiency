package handler

import (
	"context"
	"net/http"

	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/gin-gonic/gin"
)

type deploymentStatusReader interface {
	Status(ctx context.Context) (map[string]any, error)
}

type DeploymentHandler struct {
	health *deployment.HealthService
	status deploymentStatusReader
}

func NewDeploymentHandler(health *deployment.HealthService, status deploymentStatusReader) *DeploymentHandler {
	return &DeploymentHandler{
		health: health,
		status: status,
	}
}

func (h *DeploymentHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, h.health.Live())
}

func (h *DeploymentHandler) Ready(c *gin.Context) {
	c.JSON(http.StatusOK, h.health.Ready(c.Request.Context()))
}

func (h *DeploymentHandler) Status(c *gin.Context) {
	resp, err := h.status.Status(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "failed to read deployment status"})
		return
	}
	c.JSON(http.StatusOK, resp)
}
