package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	appauth "github.com/ai-efficiency/backend/internal/auth"
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
	req.ExistingMappingID = 0
	plan, err := h.service.Preview(c.Request.Context(), req)
	if err != nil {
		if writeRelayPlanningExistingMappingError(c, err) {
			return
		}
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
	req.ExistingMappingID = 0
	if actor := appauth.GetUserContext(c); actor != nil {
		req.InitiatedByUserID = actor.UserID
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

func (h *RelayPlanningHandler) AuditLegacyMigration(c *gin.Context) {
	report, err := h.service.AuditLegacyOperations(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, report)
}

func (h *RelayPlanningHandler) ApplyLegacyMigration(c *gin.Context) {
	actor := appauth.GetUserContext(c)
	if actor == nil || actor.UserID <= 0 {
		pkg.Error(c, http.StatusUnauthorized, "authenticated administrator is required")
		return
	}
	report, err := h.service.MigrateLegacyOperations(c.Request.Context(), relayplanning.LegacyMigrationRequest{Apply: true, InitiatedByUserID: actor.UserID})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, report)
}

func (h *RelayPlanningHandler) GetOperation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("operation_id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid operation id")
		return
	}
	result, err := h.service.GetRelationshipOperation(c.Request.Context(), id)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *RelayPlanningHandler) PreviewRecovery(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("operation_id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid operation id")
		return
	}
	var req struct {
		Direction relayplanning.RecoveryDirection `json:"direction"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid recovery preview request")
		return
	}
	result, err := h.service.PreviewRecovery(c.Request.Context(), id, req.Direction)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *RelayPlanningHandler) ConfirmRecovery(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("operation_id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid operation id")
		return
	}
	var req struct {
		Direction                       relayplanning.RecoveryDirection `json:"direction"`
		ExpectedBaselineRevisions       map[string]int64                `json:"expected_baseline_revisions"`
		ExpectedRelationshipFingerprint string                          `json:"expected_relationship_fingerprint"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid recovery confirmation request")
		return
	}
	request := relayplanning.RecoveryConfirmRequest{OperationID: id, Direction: req.Direction, ExpectedBaselineRevisions: req.ExpectedBaselineRevisions, ExpectedRelationshipFingerprint: req.ExpectedRelationshipFingerprint}
	if actor := appauth.GetUserContext(c); actor != nil {
		request.InitiatedByUserID = actor.UserID
	}
	result, err := h.service.ConfirmRecovery(c.Request.Context(), request)
	if err != nil {
		var stale *relayplanning.StaleRecoveryError
		if errors.As(err, &stale) {
			pkg.ErrorWithDetails(c, http.StatusConflict, stale.Error(), gin.H{"error_code": "stale_recovery_preview", "reason": stale.Reason, "current_preview": stale.Current})
			return
		}
		var blocker *relayplanning.ExternalRecoveryBlockerError
		if errors.As(err, &blocker) {
			pkg.ErrorWithDetails(c, http.StatusConflict, blocker.Error(), gin.H{"error_code": "relationship_operation_blocked_external", "resource_type": blocker.ResourceType, "resource_id": blocker.ResourceID, "relationship": blocker.Relationship, "retryable": false})
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
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

func (h *RelayPlanningHandler) ExecuteMappingRenewal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req relayplanning.MappingRenewalExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid mapping renewal execution request")
		return
	}
	result, err := h.service.ExecuteMappingRenewal(c.Request.Context(), id, req)
	if err != nil {
		var stale *relayplanning.StaleMappingRenewalError
		if errors.As(err, &stale) {
			pkg.ErrorWithDetails(c, http.StatusConflict, stale.Error(), gin.H{
				"error_code":                        "stale_relay_plan",
				"expected_relationship_fingerprint": stale.ExpectedFingerprint,
				"current_relationship_fingerprint":  stale.CurrentFingerprint,
				"refreshed_preview":                 stale.RefreshedPreview,
				"differences":                       stale.Differences,
			})
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
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
	if actor := appauth.GetUserContext(c); actor != nil {
		req.InitiatedByUserID = actor.UserID
	}
	result, err := h.service.ExecuteReplan(c.Request.Context(), id, req)
	if err != nil {
		writeRelayPlanningExecutionError(c, err)
		return
	}
	pkg.Success(c, result)
}

func writeRelayPlanningExecutionError(c *gin.Context, err error) {
	if writeRelayPlanningExistingMappingError(c, err) {
		return
	}
	var persistence *relayplanning.MappingPersistenceError
	if errors.As(err, &persistence) {
		pkg.ErrorWithDetails(c, http.StatusUnprocessableEntity, persistence.Error(), gin.H{
			"error_code": "mapping_persistence_failed",
			"retryable":  true,
			"mappings":   persistence.Results,
		})
		return
	}
	var activeOperation *relayplanning.ActiveRelationshipOperationError
	if errors.As(err, &activeOperation) {
		pkg.ErrorWithDetails(c, http.StatusConflict, activeOperation.Error(), gin.H{
			"error_code": "relationship_operation_active",
			"mapping_id": activeOperation.MappingID,
			"retryable":  false,
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
	var legacy *relayplanning.LegacyOperationConflictError
	if errors.As(err, &legacy) {
		pkg.ErrorWithDetails(c, http.StatusConflict, legacy.Error(), gin.H{
			"error_code": "legacy_operation_conflict",
			"reason":     legacy.Reason,
			"retryable":  false,
		})
		return
	}
	pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
}

func writeRelayPlanningExistingMappingError(c *gin.Context, err error) bool {
	var existing *relayplanning.ExistingMappingError
	if !errors.As(err, &existing) {
		return false
	}
	pkg.ErrorWithDetails(c, http.StatusConflict, existing.Error(), gin.H{
		"error_code": "existing_mapping",
		"mapping_id": existing.MappingID,
	})
	return true
}
