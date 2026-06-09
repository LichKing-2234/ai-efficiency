package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type userUsageProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type UserUsageHandler struct {
	entClient        *ent.Client
	providerResolver userUsageProviderResolver
	encryptionKey    string
}

func NewUserUsageHandler(entClient *ent.Client, providerResolver userUsageProviderResolver, encryptionKey string) *UserUsageHandler {
	return &UserUsageHandler{
		entClient:        entClient,
		providerResolver: providerResolver,
		encryptionKey:    encryptionKey,
	}
}

func (h *UserUsageHandler) resolvePrimaryProvider(c *gin.Context) (relay.Provider, error) {
	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true)).
		All(c.Request.Context())
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return h.providerResolver.Resolve(c.Request.Context(), 1)
	}
	return h.providerResolver.Resolve(c.Request.Context(), providers[0].ID)
}

func (h *UserUsageHandler) Dashboard(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UserUsageDashboardResponse{
			Configured: false,
			Range: relay.UserUsageDashboardRange{
				StartDate:   params.StartDate,
				EndDate:     params.EndDate,
				Granularity: params.Granularity,
				Timezone:    params.Timezone,
			},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		})
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "decrypt relay password: "+err.Error())
		return
	}

	relayProvider, err := h.resolvePrimaryProvider(c)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "resolve relay provider: "+err.Error())
		return
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	snapshot, err := relayProvider.GetUserUsageDashboard(c.Request.Context(), login, password, params)
	if err != nil {
		if errors.Is(err, relay.ErrInvalidCredentials) {
			pkg.Error(c, http.StatusConflict, "Relay credentials need attention. Please update AI service configuration.")
			return
		}
		pkg.Error(c, http.StatusBadGateway, "get usage dashboard: "+err.Error())
		return
	}

	if snapshot == nil {
		pkg.Error(c, http.StatusBadGateway, "get usage dashboard: empty response")
		return
	}
	pkg.Success(c, snapshot)
}

func parseUserUsageDashboardParams(c *gin.Context) (relay.UserUsageDashboardParams, bool) {
	granularity := strings.TrimSpace(c.DefaultQuery("granularity", "day"))
	if granularity != "day" && granularity != "hour" {
		pkg.Error(c, http.StatusBadRequest, "granularity must be day or hour")
		return relay.UserUsageDashboardParams{}, false
	}

	params := relay.UserUsageDashboardParams{
		StartDate:   strings.TrimSpace(c.Query("start_date")),
		EndDate:     strings.TrimSpace(c.Query("end_date")),
		Granularity: granularity,
		Timezone:    strings.TrimSpace(c.Query("timezone")),
	}
	if params.StartDate == "" || params.EndDate == "" {
		start, end := defaultUserUsageRange(time.Now())
		if params.StartDate == "" {
			params.StartDate = start
		}
		if params.EndDate == "" {
			params.EndDate = end
		}
	}
	return params, true
}

func defaultUserUsageRange(now time.Time) (string, string) {
	today := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -6).Format("2006-01-02")
	return start, today
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
