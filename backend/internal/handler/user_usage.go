package handler

import (
	"net/http"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type UserUsageHandler struct {
	entClient     *ent.Client
	relayProvider relay.Provider
	encryptionKey string
}

func NewUserUsageHandler(entClient *ent.Client, relayProvider relay.Provider, encryptionKey string) *UserUsageHandler {
	return &UserUsageHandler{
		entClient:     entClient,
		relayProvider: relayProvider,
		encryptionKey: encryptionKey,
	}
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

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	stats, err := h.relayProvider.GetUserUsageStats(c.Request.Context(), login, password)
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

	trend, err := h.relayProvider.GetUserUsageTrend(c.Request.Context(), login, password, params)
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

	models, err := h.relayProvider.GetUserUsageModels(c.Request.Context(), login, password, params)
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
