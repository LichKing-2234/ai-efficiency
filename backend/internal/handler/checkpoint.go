package handler

import (
	"errors"
	"net/http"

	"github.com/ai-efficiency/backend/internal/auth"
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
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req checkpoint.CommitCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordCheckpointForUser(c.Request.Context(), uc.UserID, req); err != nil {
		if errors.Is(err, checkpoint.ErrCheckpointForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *CheckpointHandler) Rewrite(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req checkpoint.CommitRewriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordRewriteForUser(c.Request.Context(), uc.UserID, req); err != nil {
		if errors.Is(err, checkpoint.ErrCheckpointForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}
