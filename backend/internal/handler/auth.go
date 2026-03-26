package handler

import (
	"context"
	"net/http"
	"os"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication HTTP requests.
type AuthHandler struct {
	authService *auth.Service
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(authService *auth.Service) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req auth.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	tokens, userInfo, err := h.authService.Login(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"tokens": tokens,
		"user":   userInfo,
	})
}

// Refresh handles POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	tokens, userInfo, err := h.authService.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		pkg.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"tokens": tokens,
		"user":   userInfo,
	})
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	pkg.Success(c, uc)
}

// DevLogin handles POST /api/v1/auth/dev-login (debug mode only)
// Creates or finds an admin user and returns a token pair without password.
// WARNING: This endpoint is only available in debug mode. Do not run debug mode in production.
func (h *AuthHandler) DevLogin(c *gin.Context, entClient *ent.Client) {
	// Extra safeguard: require AE_DEV_LOGIN_ENABLED=true
	if os.Getenv("AE_DEV_LOGIN_ENABLED") != "true" {
		pkg.Error(c, http.StatusForbidden, "dev login disabled (set AE_DEV_LOGIN_ENABLED=true)")
		return
	}

	ctx := context.Background()

	// Find or create dev admin user
	u, err := entClient.User.Query().
		Where(entuser.UsernameEQ("admin")).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			u, err = entClient.User.Create().
				SetUsername("admin").
				SetEmail("admin@dev.local").
				SetAuthSource("sub2api_sso").
				SetRole("admin").
				Save(ctx)
			if err != nil {
				pkg.Error(c, http.StatusInternalServerError, "create dev user: "+err.Error())
				return
			}
		} else {
			pkg.Error(c, http.StatusInternalServerError, "query user: "+err.Error())
			return
		}
	}

	pair, err := h.authService.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: u.Username,
		Email:    u.Email,
		Role:     string(u.Role),
	})
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "generate token: "+err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"token":         pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
		"user": gin.H{
			"id":       u.ID,
			"username": u.Username,
			"email":    u.Email,
			"role":     string(u.Role),
		},
	})
}
