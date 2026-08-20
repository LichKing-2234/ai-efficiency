package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/attributionclaim"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

const (
	reporterPrincipalKey = "attribution_reporter_principal"
	maxAttributionBody   = 2 << 20
)

type AttributionHandler struct {
	installations *attributionledger.InstallationService
	claims        *attributionclaim.Service
	readiness     attributionReadinessService
}

type attributionReadinessService interface {
	V2PersonalReadiness(context.Context, int) (activity.V2Readiness, error)
}

func NewAttributionHandler(installations *attributionledger.InstallationService, claims *attributionclaim.Service, readiness attributionReadinessService) *AttributionHandler {
	return &AttributionHandler{
		installations: installations,
		claims:        claims,
		readiness:     readiness,
	}
}

type ensureInstallationRequest struct {
	InstallationID string `json:"installation_id"`
	Label          string `json:"label"`
	ClientVersion  string `json:"client_version"`
}

type setInstallationEnabledRequest struct {
	ReportingEnabled *bool `json:"reporting_enabled,omitempty"`
	// Accepted only so pre-cleanup clients can disable their retired exporter.
	OTelEnabled *bool `json:"otel_enabled,omitempty"`
}

type attributionStatusResponse struct {
	State            attributionledger.ReportingSetupState `json:"state"`
	Retryable        bool                                  `json:"retryable"`
	LatestAcceptedAt *time.Time                            `json:"latest_accepted_at,omitempty"`
}

func (h *AttributionHandler) Status(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	c.Header("Cache-Control", "no-store")
	state, err := h.installations.SetupState(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.ErrorWithDetails(c, http.StatusServiceUnavailable, "reporting readiness unavailable", gin.H{"retryable": true})
		return
	}
	response := attributionStatusResponse{State: state}
	if state == attributionledger.ReportingSetupWaiting {
		if h.readiness == nil {
			pkg.ErrorWithDetails(c, http.StatusServiceUnavailable, "reporting readiness unavailable", gin.H{"retryable": true})
			return
		}
		readiness, err := h.readiness.V2PersonalReadiness(c.Request.Context(), uc.UserID)
		if err != nil {
			pkg.ErrorWithDetails(c, http.StatusServiceUnavailable, "reporting readiness unavailable", gin.H{"retryable": true})
			return
		}
		if readiness.State == "active" {
			response.State = attributionledger.ReportingSetupActive
			response.LatestAcceptedAt = readiness.LatestAcceptedAt
		}
	}
	pkg.Success(c, response)
}

func (h *AttributionHandler) EnsureInstallation(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req ensureInstallationRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.installations.Ensure(c.Request.Context(), uc.UserID, req.InstallationID, req.Label, req.ClientVersion)
	if err != nil {
		if errors.Is(err, attributionledger.ErrInstallationForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *AttributionHandler) SetInstallationEnabled(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req setInstallationEnabledRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.ReportingEnabled == nil {
		pkg.Error(c, http.StatusBadRequest, "reporting_enabled is required")
		return
	}
	if req.OTelEnabled != nil && *req.OTelEnabled {
		pkg.Error(c, http.StatusBadRequest, "otel_enabled is retired")
		return
	}
	result, err := h.installations.SetEnabled(c.Request.Context(), uc.UserID, c.Param("installation_id"), req.ReportingEnabled)
	if err != nil {
		if errors.Is(err, attributionledger.ErrInstallationForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *AttributionHandler) RotateInstallationCredentials(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	result, err := h.installations.Rotate(c.Request.Context(), uc.UserID, c.Param("installation_id"))
	if err != nil {
		if errors.Is(err, attributionledger.ErrInstallationForbidden) {
			pkg.Error(c, http.StatusForbidden, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, result)
}

func (h *AttributionHandler) CreateV2Claims(c *gin.Context) {
	principal := getInstallationPrincipal(c)
	if principal == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.claims == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "v2 claim ingest unavailable")
		return
	}
	var req attributionclaim.BatchRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.claims.Ingest(c.Request.Context(), *principal, req)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, result)
}

func requireInstallationToken(service *attributionledger.InstallationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		principal, err := service.AuthenticateReporter(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": err.Error()})
			return
		}
		c.Set(reporterPrincipalKey, &principal)
		c.Set(auth.ContextKeyUser, &auth.UserContext{UserID: principal.UserID, Role: "user"})
		c.Next()
	}
}

func getInstallationPrincipal(c *gin.Context) *attributionledger.InstallationPrincipal {
	value, ok := c.Get(reporterPrincipalKey)
	if !ok {
		return nil
	}
	principal, _ := value.(*attributionledger.InstallationPrincipal)
	return principal
}

func bearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func decodeStrictJSON(c *gin.Context, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, maxAttributionBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return fmt.Errorf("request body must contain one JSON object")
	} else if err != io.EOF {
		return fmt.Errorf("decode trailing request JSON: %w", err)
	}
	return nil
}
