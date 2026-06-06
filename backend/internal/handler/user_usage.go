package handler

import (
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type UserUsageHandler struct {
	entClient       *ent.Client
	providerHandler *ProviderHandler
	encryptionKey   string
}

func NewUserUsageHandler(entClient *ent.Client, providerHandler *ProviderHandler, encryptionKey string) *UserUsageHandler {
	return &UserUsageHandler{
		entClient:       entClient,
		providerHandler: providerHandler,
		encryptionKey:   encryptionKey,
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
		// Fallback to ID 1 if no primary is set
		return h.providerHandler.Resolve(c.Request.Context(), 1)
	}
	return h.providerHandler.Resolve(c.Request.Context(), providers[0].ID)
}

func (h *UserUsageHandler) Stats(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, nil)
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

	stats, err := relayProvider.GetUserUsageStats(c.Request.Context(), login, password)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage stats: "+err.Error())
		return
	}

	pkg.Success(c, stats)
}

func (h *UserUsageHandler) Trend(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UsageTrendResponse{})
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

	params := relay.UsageTrendParams{
		StartDate:   c.Query("start_date"),
		EndDate:     c.Query("end_date"),
		Granularity: c.DefaultQuery("granularity", "day"),
		Timezone:    c.Query("timezone"),
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	trend, err := relayProvider.GetUserUsageTrend(c.Request.Context(), login, password, params)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage trend: "+err.Error())
		return
	}

	pkg.Success(c, trend)
}

func (h *UserUsageHandler) Models(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UsageModelResponse{})
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

	params := relay.UsageModelParams{
		StartDate: c.Query("start_date"),
		EndDate:   c.Query("end_date"),
		Timezone:  c.Query("timezone"),
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	models, err := relayProvider.GetUserUsageModels(c.Request.Context(), login, password, params)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "get usage models: "+err.Error())
		return
	}

	pkg.Success(c, models)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
