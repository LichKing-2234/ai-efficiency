package handler

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication HTTP requests.
type AuthHandler struct {
	authService *auth.Service
	entClient   *ent.Client
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(authService *auth.Service, entClient *ent.Client) *AuthHandler {
	return &AuthHandler{authService: authService, entClient: entClient}
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

// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.RefreshToken) != "" {
		if err := h.authService.RevokeRefreshToken(c.Request.Context(), req.RefreshToken); err != nil {
			pkg.Error(c, http.StatusUnauthorized, err.Error())
			return
		}
	}
	pkg.Success(c, gin.H{"status": "logged_out"})
}

// LogoutAll handles POST /api/v1/auth/logout-all.
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := h.authService.RevokeUserRefreshSessions(c.Request.Context(), uc.UserID); err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{"status": "logged_out_all"})
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	if h.entClient == nil {
		pkg.Success(c, gin.H{
			"id":          uc.UserID,
			"username":    uc.Username,
			"email":       "",
			"role":        uc.Role,
			"auth_source": "",
		})
		return
	}
	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load user")
		return
	}
	pkg.Success(c, gin.H{
		"id":          u.ID,
		"username":    u.Username,
		"email":       u.Email,
		"role":        string(u.Role),
		"auth_source": string(u.AuthSource),
	})
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
