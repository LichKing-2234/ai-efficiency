package handler

import (
	"errors"
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

func (h *RelayPlanningHandler) SearchUsers(c *gin.Context) {
	providerID, _ := strconv.Atoi(strings.TrimSpace(c.Query("provider_id")))
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.Query("page_size")))
	result, err := h.service.SearchUsers(c.Request.Context(), relayplanning.UserSearchRequest{
		ProviderID: providerID,
		Platform:   c.Query("platform"),
		Query:      c.Query("q"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *RelayPlanningHandler) SearchAccounts(c *gin.Context) {
	providerID, _ := strconv.Atoi(strings.TrimSpace(c.Query("provider_id")))
	page, _ := strconv.Atoi(strings.TrimSpace(c.Query("page")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.Query("page_size")))
	result, err := h.service.SearchAccounts(c.Request.Context(), relayplanning.AccountSearchRequest{ProviderID: providerID, Platform: c.Query("platform"), Query: c.Query("q"), Page: page, PageSize: pageSize})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
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
		writeRelayPlanningExecutionError(c, err)
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

func (h *RelayPlanningHandler) PreviewMappingRenewal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req relayplanning.MappingRenewalPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping renewal preview request")
		return
	}
	preview, err := h.service.PreviewMappingRenewal(c.Request.Context(), id, req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, preview)
}

func (h *RelayPlanningHandler) Rebind(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req struct {
		DepartmentID    string  `json:"department_id"`
		TemplateGroupID int64   `json:"template_group_id"`
		SourceGroupID   int64   `json:"source_group_id"`
		GroupIDs        []int64 `json:"group_ids"`
		Status          string  `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid rebind request")
		return
	}
	mapping, err := h.service.Rebind(c.Request.Context(), id, req.DepartmentID, req.TemplateGroupID, req.SourceGroupID, req.GroupIDs, req.Status)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, mapping)
}

func (h *RelayPlanningHandler) AdoptCurrentAccounts(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	mapping, err := h.service.AdoptCurrentAccounts(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, mapping)
}

func (h *RelayPlanningHandler) SaveDesiredAccounts(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req struct {
		DesiredAccounts map[string][]relayplanning.AccountIntent `json:"desired_accounts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid desired account request")
		return
	}
	mapping, err := h.service.SaveDesiredAccounts(c.Request.Context(), id, req.DesiredAccounts)
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
		SelectedUserIDs   []int                                 `json:"selected_user_ids"`
		Assignments       []relayplanning.Assignment            `json:"assignments"`
		MemberSources     map[string]int64                      `json:"member_sources"`
		RemovedUserIDs    []int                                 `json:"removed_user_ids"`
		MemberActions     map[string]relayplanning.MemberAction `json:"member_actions"`
		AdoptRelayUserIDs []int64                               `json:"adopt_relay_user_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		pkg.Error(c, http.StatusBadRequest, "invalid replan request")
		return
	}
	plan, err := h.service.Replan(c.Request.Context(), id, req.SelectedUserIDs, req.Assignments, req.MemberSources, req.RemovedUserIDs, req.MemberActions, req.AdoptRelayUserIDs)
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
		writeRelayPlanningExecutionError(c, err)
		return
	}
	pkg.Success(c, result)
}

func writeRelayPlanningExecutionError(c *gin.Context, err error) {
	var persistence *relayplanning.MappingPersistenceError
	if errors.As(err, &persistence) {
		pkg.ErrorWithDetails(c, http.StatusUnprocessableEntity, persistence.Error(), gin.H{
			"error_code": "mapping_persistence_failed",
			"retryable":  true,
			"mappings":   persistence.Results,
		})
		return
	}
	var stale *relayplanning.StalePlanError
	if errors.As(err, &stale) {
		pkg.ErrorWithDetails(c, http.StatusConflict, stale.Error(), gin.H{
			"error_code":                        "stale_relay_plan",
			"expected_relationship_fingerprint": stale.ExpectedFingerprint,
			"current_relationship_fingerprint":  stale.CurrentFingerprint,
			"refreshed_plan":                    stale.RefreshedPlan,
			"differences":                       stale.Differences,
		})
		return
	}
	pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
}
