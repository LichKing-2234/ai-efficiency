package handler

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles authentication HTTP requests.
type AuthHandler struct {
	authService           *auth.Service
	entClient             *ent.Client
	adminSettingsHandler  *AdminSettingsHandler
	reportingCapabilities ReportingCapabilities
	guides                Guides
}

// Guides carries externally maintained documentation links. A guide with an
// empty URL is omitted so deployments that do not maintain one never render a
// dead link.
type Guides struct {
	CCSwitch *GuideLink `json:"ccswitch,omitempty"`
}

type GuideLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// NewGuides builds the guide payload, dropping entries without a URL.
func NewGuides(ccSwitchLabel, ccSwitchURL string) Guides {
	guides := Guides{}
	if url := strings.TrimSpace(ccSwitchURL); url != "" {
		guides.CCSwitch = &GuideLink{Label: strings.TrimSpace(ccSwitchLabel), URL: url}
	}
	return guides
}

type ReportingCapabilities struct {
	SetupAvailable     bool `json:"setup_available"`
	ReadinessAvailable bool `json:"readiness_available"`
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(authService *auth.Service, entClient *ent.Client, adminSettingsHandler *AdminSettingsHandler, reportingCapabilities ReportingCapabilities, guides Guides) *AuthHandler {
	return &AuthHandler{
		authService: authService, entClient: entClient, adminSettingsHandler: adminSettingsHandler,
		reportingCapabilities: reportingCapabilities, guides: guides,
	}
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

// Options handles GET /api/v1/auth/options.
func (h *AuthHandler) Options(c *gin.Context) {
	if h.adminSettingsHandler != nil {
		h.adminSettingsHandler.RefreshOIDC(c.Request.Context())
	}
	pkg.Success(c, gin.H{
		"ldap_enabled":      h.ldapEnabled(),
		"dev_login_enabled": devLoginEnabled(),
		"oidc_enabled":      h.authService.OIDCEnabled(),
		"oidc_label":        h.authService.OIDCLabel(),
	})
}

func (h *AuthHandler) OIDCStart(c *gin.Context) {
	if h.adminSettingsHandler != nil {
		h.adminSettingsHandler.RefreshOIDC(c.Request.Context())
	}
	provider := h.authService.OIDC()
	if provider == nil {
		c.Status(http.StatusNotFound)
		return
	}
	redirect := c.Query("redirect")
	if redirect == "" || redirect[0] != '/' || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}
	var state, target string
	var err error
	for _, callback := range oidcCallbackCandidates(c) {
		state, target, err = provider.Begin(c.Request.Context(), redirect, callback)
		if err == nil {
			break
		}
	}
	if err != nil {
		pkg.Error(c, http.StatusServiceUnavailable, "OIDC login unavailable")
		return
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{Name: "ae_oidc_state", Value: state, Path: "/auth/oidc/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	c.Redirect(http.StatusFound, target)
}

func oidcCallbackCandidates(c *gin.Context) []string {
	scheme := "http"
	forwardedProto := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	if c.Request.TLS != nil || strings.EqualFold(forwardedProto, "https") {
		scheme = "https"
	}
	hosts := make([]string, 0, 4)
	if requestedHost := c.Query("host"); requestedHost != "" {
		hosts = append(hosts, requestedHost)
	}
	if referer, err := url.Parse(c.GetHeader("Referer")); err == nil && referer.Host != "" {
		hosts = append(hosts, referer.Host)
	}
	// Kong can retain a route's forwarded host while the request Host is the
	// public hostname the browser actually used. Prefer that public Host so a
	// canonical-domain request does not receive a callback for another domain.
	if !isInternalRequestHost(c.Request.Host) {
		hosts = append(hosts, c.Request.Host)
	}
	hosts = append(hosts, c.GetHeader("X-Forwarded-Host"), c.GetHeader("X-Original-Host"), c.Request.Host)
	seen := make(map[string]struct{}, len(hosts))
	callbacks := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		host := strings.TrimSpace(strings.Split(raw, ",")[0])
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		if host == "" {
			continue
		}
		if parsedHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = parsedHost
		}
		callback := scheme + "://" + host + "/auth/oidc/callback"
		if _, ok := seen[callback]; !ok {
			seen[callback] = struct{}{}
			callbacks = append(callbacks, callback)
		}
	}
	return callbacks
}

func isInternalRequestHost(raw string) bool {
	host := strings.TrimSpace(strings.Split(raw, ",")[0])
	if host == "" {
		return true
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || host == "internal" || strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".cluster.local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback()
	}
	return false
}

func (h *AuthHandler) OIDCCallback(c *gin.Context) {
	provider := h.authService.OIDC()
	if provider == nil {
		c.Status(http.StatusNotFound)
		return
	}
	cookie, err := c.Request.Cookie("ae_oidc_state")
	queryState := c.Query("state")
	if err != nil || cookie.Value == "" || queryState == "" || queryState != cookie.Value || c.Query("error") != "" {
		pkg.Error(c, http.StatusUnauthorized, "invalid OIDC state")
		return
	}
	handoff, redirect, err := provider.Complete(c.Request.Context(), cookie.Value, c.Query("code"), h.authService)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{Name: "ae_oidc_state", Value: "", Path: "/auth/oidc/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if err != nil {
		if authstate.Unavailable(err) {
			pkg.Error(c, http.StatusServiceUnavailable, "OIDC login unavailable")
			return
		}
		pkg.Error(c, http.StatusUnauthorized, "OIDC login could not be completed")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "ae_oidc_handoff", Value: handoff, Path: "/auth/oidc/complete", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 120})
	location := "/login?oidc=complete&redirect=" + url.QueryEscape(redirect)
	c.Redirect(http.StatusFound, location)
}

func (h *AuthHandler) OIDCComplete(c *gin.Context) {
	provider := h.authService.OIDC()
	if provider == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	cookie, err := c.Request.Cookie("ae_oidc_handoff")
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	http.SetCookie(c.Writer, &http.Cookie{Name: "ae_oidc_handoff", Value: "", Path: "/auth/oidc/complete", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if err != nil || cookie.Value == "" {
		pkg.Error(c, http.StatusUnauthorized, "OIDC handoff missing")
		return
	}
	user, redirect, err := provider.Take(c.Request.Context(), cookie.Value)
	if err != nil {
		if authstate.Unavailable(err) {
			pkg.Error(c, http.StatusServiceUnavailable, "OIDC login unavailable")
			return
		}
		pkg.Error(c, http.StatusUnauthorized, "OIDC handoff expired")
		return
	}
	tokens, err := h.authService.GenerateTokenPairForUser(user)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "OIDC token issuance failed")
		return
	}
	pkg.Success(c, gin.H{"tokens": tokens, "user": user, "redirect": redirect})
}

// OIDCBackchannelLogout handles POST /auth/oidc/backchannel-logout. The
// endpoint is called server-to-server by the identity provider when a browser
// session ends or an administrator deactivates the user, so it must stay
// reachable without a browser session and must not redirect.
func (h *AuthHandler) OIDCBackchannelLogout(c *gin.Context) {
	if h.adminSettingsHandler != nil {
		h.adminSettingsHandler.RefreshOIDC(c.Request.Context())
	}
	provider := h.authService.OIDC()
	if provider == nil {
		c.Status(http.StatusNotFound)
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid logout request")
		return
	}
	raw := strings.TrimSpace(c.PostForm("logout_token"))
	if raw == "" {
		pkg.Error(c, http.StatusBadRequest, "logout_token is required")
		return
	}
	identity, err := provider.VerifyLogoutToken(c.Request.Context(), raw)
	if err != nil {
		if authstate.Unavailable(err) {
			pkg.Error(c, http.StatusServiceUnavailable, "logout token check unavailable")
			return
		}
		pkg.Error(c, http.StatusBadRequest, "invalid logout token")
		return
	}
	if _, err := h.authService.RevokeTokensByOIDCSubject(c.Request.Context(), identity.Issuer, identity.Subject); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to revoke tokens")
		return
	}
	c.Status(http.StatusOK)
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
	if h.entClient == nil {
		pkg.Success(c, gin.H{
			"id":                     uc.UserID,
			"username":               uc.Username,
			"email":                  "",
			"role":                   uc.Role,
			"auth_source":            "",
			"reporting_capabilities": h.reportingCapabilities,
			"guides":                 h.guides,
		})
		return
	}
	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load user")
		return
	}
	pkg.Success(c, gin.H{
		"id":                     u.ID,
		"username":               u.Username,
		"email":                  u.Email,
		"role":                   string(u.Role),
		"auth_source":            string(u.AuthSource),
		"reporting_capabilities": h.reportingCapabilities,
		"guides":                 h.guides,
	})
}

// DevLogin handles POST /api/v1/auth/dev-login (debug mode only)
// Creates or finds an admin user and returns a token pair without password.
// WARNING: This endpoint is only available in debug mode. Do not run debug mode in production.
func (h *AuthHandler) DevLogin(c *gin.Context, entClient *ent.Client) {
	// Extra safeguard: require AE_DEV_LOGIN_ENABLED=true
	if !devLoginEnabled() {
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

func (h *AuthHandler) ldapEnabled() bool {
	if h.adminSettingsHandler == nil || h.adminSettingsHandler.ldapConfig == nil {
		return false
	}
	cfg := h.adminSettingsHandler.ldapConfig.Load()
	return cfg != nil && strings.TrimSpace(cfg.URL) != ""
}

func devLoginEnabled() bool {
	return gin.Mode() == gin.DebugMode && os.Getenv("AE_DEV_LOGIN_ENABLED") == "true"
}
