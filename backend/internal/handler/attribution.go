package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	ledger        *attributionledger.Service
	correlation   *attributionledger.CorrelationStore
	claims        *attributionclaim.Service
}

func NewAttributionHandler(installations *attributionledger.InstallationService, ledger *attributionledger.Service, correlation *attributionledger.CorrelationStore, claims ...*attributionclaim.Service) *AttributionHandler {
	handler := &AttributionHandler{
		installations: installations,
		ledger:        ledger,
		correlation:   correlation,
	}
	if len(claims) > 0 {
		handler.claims = claims[0]
	}
	return handler
}

type ensureInstallationRequest struct {
	InstallationID string `json:"installation_id"`
	Label          string `json:"label"`
	ClientVersion  string `json:"client_version"`
}

type setInstallationEnabledRequest struct {
	ReportingEnabled *bool `json:"reporting_enabled,omitempty"`
	OTelEnabled      *bool `json:"otel_enabled,omitempty"`
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
	if req.ReportingEnabled == nil && req.OTelEnabled == nil {
		pkg.Error(c, http.StatusBadRequest, "at least one enabled flag is required")
		return
	}
	result, err := h.installations.SetEnabled(c.Request.Context(), uc.UserID, c.Param("installation_id"), req.ReportingEnabled, req.OTelEnabled)
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

func (h *AttributionHandler) CreateBuckets(c *gin.Context) {
	principal := getInstallationPrincipal(c)
	if principal == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req attributionledger.BatchRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.ledger.CreateBuckets(c.Request.Context(), *principal, req)
	if err != nil {
		writeAttributionMutationError(c, err)
		return
	}
	pkg.Created(c, result)
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

func (h *AttributionHandler) CreateRevision(c *gin.Context) {
	principal := getInstallationPrincipal(c)
	if principal == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req attributionledger.RevisionRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	created, err := h.ledger.CreateRevision(c.Request.Context(), *principal, c.Param("bucket_id"), req)
	if err != nil {
		writeAttributionMutationError(c, err)
		return
	}
	pkg.Success(c, gin.H{"created": created, "revision_id": req.RevisionID})
}

func (h *AttributionHandler) Report(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	to := time.Now().UTC()
	from := to.Add(-7 * 24 * time.Hour)
	var err error
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "invalid from")
			return
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, "invalid to")
			return
		}
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		pkg.Error(c, http.StatusBadRequest, "invalid report window")
		return
	}
	userID := uc.UserID
	if uc.Role == "admin" && strings.TrimSpace(c.Query("user_id")) != "" {
		userID, err = strconv.Atoi(c.Query("user_id"))
		if err != nil || userID <= 0 {
			pkg.Error(c, http.StatusBadRequest, "invalid user_id")
			return
		}
	}
	report, err := h.ledger.Report(c.Request.Context(), userID, from, to)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, report)
}

func (h *AttributionHandler) IngestOTLP(c *gin.Context) {
	principal := getInstallationPrincipal(c)
	if principal == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAttributionBody))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "OTLP payload too large"})
		return
	}
	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid OTLP JSON"})
		return
	}
	evidence := attributionledger.ExtractCodexRequestEvidence(root)
	if h.correlation == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "correlation store unavailable"})
		return
	}
	if err := h.correlation.Put(c.Request.Context(), principal.InstallationID, evidence); err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "correlation store unavailable"})
		return
	}
	updated, err := h.ledger.RefreshCorrelation(c.Request.Context(), *principal, evidence)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "correlation metadata refresh unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"partialSuccess": gin.H{}, "accepted": len(evidence), "updated_buckets": updated})
}

func requireInstallationToken(service *attributionledger.InstallationService, otlp bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		var principal attributionledger.InstallationPrincipal
		var err error
		if otlp {
			principal, err = service.AuthenticateOTLP(c.Request.Context(), token)
		} else {
			principal, err = service.AuthenticateReporter(c.Request.Context(), token)
		}
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

func writeAttributionMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, attributionledger.ErrImmutableBucketConflict), errors.Is(err, attributionledger.ErrRevisionConflict):
		pkg.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, attributionledger.ErrInstallationForbidden), errors.Is(err, attributionledger.ErrAllocationForbidden):
		pkg.Error(c, http.StatusForbidden, err.Error())
	default:
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
	}
}
