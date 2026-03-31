package handler

import (
	"net/http"

	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type CheckpointHandler struct {
	service *checkpoint.Service
}

func NewCheckpointHandler(service *checkpoint.Service) *CheckpointHandler {
	return &CheckpointHandler{service: service}
}

func (h *CheckpointHandler) Commit(c *gin.Context) {
	var req checkpoint.CommitCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordCheckpoint(c.Request.Context(), req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *CheckpointHandler) Rewrite(c *gin.Context) {
	var req checkpoint.CommitRewriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordRewrite(c.Request.Context(), req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}
