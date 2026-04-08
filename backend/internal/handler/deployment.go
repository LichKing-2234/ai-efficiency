package handler

import (
	"context"
	"net/http"

	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/gin-gonic/gin"
)

type deploymentStatusReader interface {
	Status(ctx context.Context) (deployment.DeploymentStatus, error)
	CheckForUpdate(ctx context.Context) (deployment.DeploymentStatus, error)
	ApplyUpdate(ctx context.Context, req deployment.ApplyRequest) (deployment.UpdateStatus, error)
	RollbackUpdate(ctx context.Context) (deployment.UpdateStatus, error)
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
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": resp})
}

func (h *DeploymentHandler) CheckForUpdate(c *gin.Context) {
	resp, err := h.status.CheckForUpdate(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": resp})
}

func (h *DeploymentHandler) ApplyUpdate(c *gin.Context) {
	var req deployment.ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid request body"})
		return
	}

	resp, err := h.status.ApplyUpdate(c.Request.Context(), req)
	if err != nil {
		if deployment.IsPolicyError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": resp})
}

func (h *DeploymentHandler) RollbackUpdate(c *gin.Context) {
	resp, err := h.status.RollbackUpdate(c.Request.Context())
	if err != nil {
		if deployment.IsPolicyError(err) {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": resp})
}
