package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relayplanning"
	"github.com/gin-gonic/gin"
)

// RelayPlanningHandler exposes the explicit admin-only department/group
// planning workflow. It deliberately keeps the HTTP layer thin; all relay
// mutations and allocation decisions live in relayplanning.Service.
type RelayPlanningHandler struct {
	service *relayplanning.Service
}

func NewRelayPlanningHandler(service *relayplanning.Service) *RelayPlanningHandler {
	return &RelayPlanningHandler{service: service}
}

func (h *RelayPlanningHandler) Preview(c *gin.Context) {
	var req relayplanning.PreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid planning request")
		return
	}
	plan, err := h.service.Preview(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, plan)
}

func (h *RelayPlanningHandler) Execute(c *gin.Context) {
	var req relayplanning.ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid planning execution request")
		return
	}
	result, err := h.service.Execute(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *RelayPlanningHandler) ListMappings(c *gin.Context) {
	providerID, _ := strconv.Atoi(strings.TrimSpace(c.Query("provider_id")))
	items, err := h.service.ListMappings(c.Request.Context(), providerID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{"items": items})
}

func (h *RelayPlanningHandler) Rebind(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req struct {
		SourceGroupID int64   `json:"source_group_id"`
		GroupIDs      []int64 `json:"group_ids"`
		Status        string  `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid rebind request")
		return
	}
	mapping, err := h.service.Rebind(c.Request.Context(), id, req.SourceGroupID, req.GroupIDs, req.Status)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, mapping)
}

func (h *RelayPlanningHandler) Replan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req struct {
		SelectedUserIDs []int `json:"selected_user_ids"`
		GroupCount      int   `json:"group_count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		pkg.Error(c, http.StatusBadRequest, "invalid replan request")
		return
	}
	plan, err := h.service.Replan(c.Request.Context(), id, req.SelectedUserIDs, req.GroupCount)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, plan)
}

func (h *RelayPlanningHandler) ReplanExecute(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req relayplanning.ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid replan execution request")
		return
	}
	if req.ExistingMappingID == 0 {
		req.ExistingMappingID = id
	}
	result, err := h.service.ExecuteReplan(c.Request.Context(), id, req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}
