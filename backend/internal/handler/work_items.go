package handler

import (
	"context"
	"net/http"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/workitems"
	"github.com/gin-gonic/gin"
)

type workItemsCounter interface {
	Counts(ctx context.Context, userID int, admin bool) (*workitems.CountsResponse, error)
}

type WorkItemsHandler struct {
	counter workItemsCounter
}

func NewWorkItemsHandler(counter workItemsCounter) *WorkItemsHandler {
	return &WorkItemsHandler{counter: counter}
}

func RegisterWorkItemsRoutes(group *gin.RouterGroup, handler *WorkItemsHandler) {
	if handler == nil {
		return
	}
	workItemsGroup := group.Group("/work-items")
	workItemsGroup.GET("/counts", handler.Counts)
}

func (h *WorkItemsHandler) Counts(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp, err := h.counter.Counts(c.Request.Context(), uc.UserID, uc.Role == "admin")
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, resp)
}
