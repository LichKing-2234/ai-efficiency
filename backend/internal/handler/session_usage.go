package handler

import (
	"errors"
	"net/http"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/sessionevent"
	"github.com/ai-efficiency/backend/internal/sessionusage"
	"github.com/gin-gonic/gin"
)

type SessionUsageHandler struct {
	usageService *sessionusage.Service
	eventService *sessionevent.Service
}

func NewSessionUsageHandler(usageService *sessionusage.Service, eventService *sessionevent.Service) *SessionUsageHandler {
	return &SessionUsageHandler{
		usageService: usageService,
		eventService: eventService,
	}
}

func (h *SessionUsageHandler) CreateUsage(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req sessionusage.CreateUsageEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.usageService.Create(c.Request.Context(), uc.UserID, req); err != nil {
		if errors.Is(err, sessionusage.ErrInvalidRequest) {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, sessionusage.ErrSessionForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *SessionUsageHandler) CreateEvent(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req sessionevent.CreateSessionEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.eventService.Create(c.Request.Context(), uc.UserID, req); err != nil {
		if errors.Is(err, sessionevent.ErrInvalidRequest) {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, sessionevent.ErrSessionForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}
