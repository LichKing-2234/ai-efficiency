package handler

import (
	"net/http"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/kirocredits"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// KiroUsageHandler exposes the read-only kiro2cc credits of the current user.
type KiroUsageHandler struct {
	service *kirocredits.Service
}

// NewKiroUsageHandler creates the handler. A nil service renders an
// unavailable snapshot instead of failing the request.
func NewKiroUsageHandler(service *kirocredits.Service) *KiroUsageHandler {
	return &KiroUsageHandler{service: service}
}

// Get handles GET /api/v1/user/usage/kiro.
func (h *KiroUsageHandler) Get(c *gin.Context) {
	userContext := auth.GetUserContext(c)
	if userContext == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h == nil || h.service == nil {
		pkg.Success(c, kirocredits.Snapshot{
			Status:  kirocredits.StatusError,
			Message: "Kiro 用量服务未配置",
		})
		return
	}
	params := kirocredits.Params{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	}
	pkg.Success(c, h.service.Credits(c.Request.Context(), userContext.UserID, params))
}
