package handler

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authsettings"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
	"github.com/go-ldap/ldap/v3"
)

const maskedSecret = "***"

// AuthSettingsStore persists administrator-managed login settings.
type AuthSettingsStore interface {
	LoadOIDC(ctx context.Context) (*authsettings.OIDC, bool, error)
	SaveOIDC(ctx context.Context, settings authsettings.OIDC) error
	DeleteOIDC(ctx context.Context) error
	LoadLDAP(ctx context.Context) (*authsettings.LDAP, bool, error)
	SaveLDAP(ctx context.Context, settings authsettings.LDAP) error
}

// AdminSettingsHandler manages login configuration stored in the database.
// Database values override deployment configuration; deleting the OIDC row
// restores the deployment values.
type AdminSettingsHandler struct {
	store       AuthSettingsStore
	authService *auth.Service
	ldapConfig  *atomic.Pointer[config.LDAPConfig]
	defaultOIDC config.OIDCConfig
	frontendURL string
	httpClient  *http.Client
	buildOIDC   func(context.Context, auth.OIDCConfig) (*auth.OIDCService, error)

	oidcRefreshMu    sync.Mutex
	oidcRefreshAt    time.Time
	oidcCachedSource string
	oidcCachedAt     time.Time
}

// NewAdminSettingsHandler creates a new admin settings handler.
func NewAdminSettingsHandler(
	store AuthSettingsStore,
	authService *auth.Service,
	ldapConfig *atomic.Pointer[config.LDAPConfig],
	defaultOIDC config.OIDCConfig,
	frontendURL string,
	httpClient *http.Client,
) *AdminSettingsHandler {
	handler := &AdminSettingsHandler{
		store:       store,
		authService: authService,
		ldapConfig:  ldapConfig,
		defaultOIDC: defaultOIDC,
		frontendURL: frontendURL,
		httpClient:  httpClient,
	}
	handler.buildOIDC = func(ctx context.Context, cfg auth.OIDCConfig) (*auth.OIDCService, error) {
		cfg.HTTPClient = handler.httpClient
		return auth.NewOIDCService(ctx, cfg)
	}
	return handler
}

type oidcSettingsResponse struct {
	Enabled        bool     `json:"enabled"`
	Label          string   `json:"label"`
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	RedirectURLs   []string `json:"redirect_urls"`
	AllowEmailLink bool     `json:"allow_email_link"`
	Scopes         []string `json:"scopes"`
	Source         string   `json:"source"`
	CallbackPath   string   `json:"callback_path"`
}

type oidcSettingsRequest struct {
	Enabled        bool     `json:"enabled"`
	Label          string   `json:"label"`
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	RedirectURLs   []string `json:"redirect_urls"`
	AllowEmailLink bool     `json:"allow_email_link"`
	Scopes         []string `json:"scopes"`
}

type ldapSettingsResponse struct {
	URL          string `json:"url"`
	BaseDN       string `json:"base_dn"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"`
	UserFilter   string `json:"user_filter"`
	TLS          bool   `json:"tls"`
	Source       string `json:"source"`
}

type ldapSettingsRequest struct {
	URL          string `json:"url" binding:"required"`
	BaseDN       string `json:"base_dn" binding:"required"`
	BindDN       string `json:"bind_dn" binding:"required"`
	BindPassword string `json:"bind_password"`
	UserFilter   string `json:"user_filter" binding:"required"`
	TLS          bool   `json:"tls"`
}

// GetLDAP handles GET /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) GetLDAP(c *gin.Context) {
	settings, source, err := h.effectiveLDAP(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load LDAP settings")
		return
	}
	pkg.Success(c, ldapSettingsResponse{
		URL:          settings.URL,
		BaseDN:       settings.BaseDN,
		BindDN:       settings.BindDN,
		BindPassword: maskSecret(settings.BindPassword),
		UserFilter:   settings.UserFilter,
		TLS:          settings.TLS,
		Source:       source,
	})
}

// UpdateLDAP handles PUT /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) UpdateLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	current, _, err := h.effectiveLDAP(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load LDAP settings")
		return
	}
	bindPassword := req.BindPassword
	if bindPassword == "" || bindPassword == maskedSecret {
		bindPassword = current.BindPassword
	}

	settings := authsettings.LDAP{
		URL:          strings.TrimSpace(req.URL),
		BaseDN:       strings.TrimSpace(req.BaseDN),
		BindDN:       strings.TrimSpace(req.BindDN),
		BindPassword: bindPassword,
		UserFilter:   strings.TrimSpace(req.UserFilter),
		TLS:          req.TLS,
	}
	if err := h.store.SaveLDAP(c.Request.Context(), settings); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to save LDAP settings: "+err.Error())
		return
	}

	runtimeConfig := settings.Config()
	h.ldapConfig.Store(&runtimeConfig)

	pkg.Success(c, ldapSettingsResponse{
		URL:          settings.URL,
		BaseDN:       settings.BaseDN,
		BindDN:       settings.BindDN,
		BindPassword: maskSecret(settings.BindPassword),
		UserFilter:   settings.UserFilter,
		TLS:          settings.TLS,
		Source:       "database",
	})
}

// TestLDAP handles POST /api/v1/admin/settings/ldap/test
func (h *AdminSettingsHandler) TestLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	bindPassword := req.BindPassword
	if bindPassword == "" || bindPassword == maskedSecret {
		current, _, err := h.effectiveLDAP(c.Request.Context())
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to load LDAP settings")
			return
		}
		bindPassword = current.BindPassword
	}

	conn, err := ldap.DialURL(req.URL)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "failed to connect to LDAP server: "+err.Error())
		return
	}
	defer conn.Close()

	if req.TLS {
		if err := conn.StartTLS(&tls.Config{InsecureSkipVerify: false}); err != nil {
			pkg.Error(c, http.StatusBadGateway, "LDAP StartTLS failed: "+err.Error())
			return
		}
	}

	if err := conn.Bind(req.BindDN, bindPassword); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "LDAP bind failed: "+err.Error())
		return
	}

	pkg.Success(c, gin.H{"message": "LDAP connection successful"})
}

// GetOIDC handles GET /api/v1/admin/settings/oidc
func (h *AdminSettingsHandler) GetOIDC(c *gin.Context) {
	settings, source, err := h.effectiveOIDC(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load OIDC settings")
		return
	}
	pkg.Success(c, oidcResponse(settings, source))
}

// UpdateOIDC handles PUT /api/v1/admin/settings/oidc
func (h *AdminSettingsHandler) UpdateOIDC(c *gin.Context) {
	var req oidcSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := h.mergeOIDCRequest(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load OIDC settings")
		return
	}
	if err := validateOIDC(settings); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var provider *auth.OIDCService
	if settings.Enabled {
		buildCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		provider, err = h.buildOIDC(buildCtx, settings.AuthConfig(h.frontendURL))
		if err != nil {
			pkg.Error(c, http.StatusUnprocessableEntity, "OIDC provider could not be initialized: "+err.Error())
			return
		}
	}
	if err := h.store.SaveOIDC(c.Request.Context(), settings); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to save OIDC settings: "+err.Error())
		return
	}
	h.authService.SetOIDCService(provider)
	h.rememberOIDCState(c.Request.Context(), "database")
	pkg.Success(c, oidcResponse(settings, "database"))
}

// DeleteOIDC handles DELETE /api/v1/admin/settings/oidc and restores deployment
// configuration.
func (h *AdminSettingsHandler) DeleteOIDC(c *gin.Context) {
	if err := h.store.DeleteOIDC(c.Request.Context()); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to reset OIDC settings: "+err.Error())
		return
	}
	fallback := authsettings.FromConfigOIDC(h.defaultOIDC)
	var provider *auth.OIDCService
	if fallback.Enabled {
		buildCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		built, err := h.buildOIDC(buildCtx, fallback.AuthConfig(h.frontendURL))
		if err != nil {
			pkg.Error(c, http.StatusUnprocessableEntity, "deployment OIDC configuration is invalid: "+err.Error())
			return
		}
		provider = built
	}
	h.authService.SetOIDCService(provider)
	h.rememberOIDCState(c.Request.Context(), "environment")
	pkg.Success(c, oidcResponse(fallback, "environment"))
}

// TestOIDC handles POST /api/v1/admin/settings/oidc/test. It validates the
// provider discovery and returns the discovered endpoints without saving.
func (h *AdminSettingsHandler) TestOIDC(c *gin.Context) {
	var req oidcSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := h.mergeOIDCRequest(c.Request.Context(), req)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to load OIDC settings")
		return
	}
	if err := validateOIDC(settings); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if settings.Issuer == "" || settings.ClientID == "" || settings.ClientSecret == "" {
		pkg.Error(c, http.StatusBadRequest, "issuer, client id and client secret are required to test OIDC")
		return
	}
	buildCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	provider, err := h.buildOIDC(buildCtx, settings.AuthConfig(h.frontendURL))
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "OIDC provider could not be initialized: "+err.Error())
		return
	}
	pkg.Success(c, provider.Discovery())
}

const oidcSettingsRefreshInterval = 10 * time.Second

// RefreshOIDC reloads administrator-managed OIDC settings when another replica
// changed them, so a save does not require a restart on every replica. Reload
// failures keep the current runtime client. The check is throttled so public
// login reads do not query the database on every request.
func (h *AdminSettingsHandler) RefreshOIDC(ctx context.Context) {
	if h == nil || h.store == nil || h.authService == nil {
		return
	}
	h.oidcRefreshMu.Lock()
	defer h.oidcRefreshMu.Unlock()
	if !h.oidcRefreshAt.IsZero() && time.Since(h.oidcRefreshAt) < oidcSettingsRefreshInterval {
		return
	}
	h.oidcRefreshAt = time.Now()

	stored, found, err := h.store.LoadOIDC(ctx)
	if err != nil {
		return
	}
	if !found {
		if h.oidcCachedSource == "environment" {
			return
		}
		fallback := authsettings.FromConfigOIDC(h.defaultOIDC)
		provider, ok := h.buildOIDCForRuntime(ctx, fallback)
		if !ok {
			return
		}
		h.authService.SetOIDCService(provider)
		h.oidcCachedSource = "environment"
		h.oidcCachedAt = time.Time{}
		return
	}
	if h.oidcCachedSource == "database" && stored.UpdatedAt.Equal(h.oidcCachedAt) {
		return
	}
	provider, ok := h.buildOIDCForRuntime(ctx, *stored)
	if !ok {
		return
	}
	h.authService.SetOIDCService(provider)
	h.oidcCachedSource = "database"
	h.oidcCachedAt = stored.UpdatedAt
}

func (h *AdminSettingsHandler) buildOIDCForRuntime(ctx context.Context, settings authsettings.OIDC) (*auth.OIDCService, bool) {
	if !settings.Enabled {
		return nil, true
	}
	buildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	provider, err := h.buildOIDC(buildCtx, settings.AuthConfig(h.frontendURL))
	if err != nil {
		return nil, false
	}
	return provider, true
}

func (h *AdminSettingsHandler) rememberOIDCState(ctx context.Context, source string) {
	if h == nil {
		return
	}
	h.oidcRefreshMu.Lock()
	defer h.oidcRefreshMu.Unlock()
	h.oidcRefreshAt = time.Now()
	h.oidcCachedSource = source
	h.oidcCachedAt = time.Time{}
	if source == "environment" {
		return
	}
	if stored, found, err := h.store.LoadOIDC(ctx); err == nil && found {
		h.oidcCachedAt = stored.UpdatedAt
	}
}

func (h *AdminSettingsHandler) effectiveOIDC(ctx context.Context) (authsettings.OIDC, string, error) {
	stored, found, err := h.store.LoadOIDC(ctx)
	if err != nil {
		return authsettings.OIDC{}, "", err
	}
	if found {
		return *stored, "database", nil
	}
	return authsettings.FromConfigOIDC(h.defaultOIDC), "environment", nil
}

func (h *AdminSettingsHandler) effectiveLDAP(ctx context.Context) (authsettings.LDAP, string, error) {
	stored, found, err := h.store.LoadLDAP(ctx)
	if err != nil {
		return authsettings.LDAP{}, "", err
	}
	if found {
		return *stored, "database", nil
	}
	current := h.ldapConfig.Load()
	if current == nil {
		return authsettings.LDAP{}, "environment", nil
	}
	return authsettings.FromConfigLDAP(*current), "environment", nil
}

func (h *AdminSettingsHandler) mergeOIDCRequest(ctx context.Context, req oidcSettingsRequest) (authsettings.OIDC, error) {
	current, _, err := h.effectiveOIDC(ctx)
	if err != nil {
		return authsettings.OIDC{}, err
	}
	clientSecret := req.ClientSecret
	if clientSecret == "" || clientSecret == maskedSecret {
		clientSecret = current.ClientSecret
	}
	settings := authsettings.OIDC{
		Enabled:        req.Enabled,
		Label:          strings.TrimSpace(req.Label),
		Issuer:         strings.TrimSpace(req.Issuer),
		ClientID:       strings.TrimSpace(req.ClientID),
		ClientSecret:   clientSecret,
		RedirectURLs:   normalizeRedirectURLs(req.RedirectURLs),
		AllowEmailLink: req.AllowEmailLink,
		Scopes:         normalizeScopes(req.Scopes),
	}
	if settings.Enabled && settings.Issuer == "" {
		// A stored row that disables OIDC keeps no issuer; fall back to the
		// deployment issuer so the administrator sees an actionable form.
		settings.Issuer = strings.TrimSpace(h.defaultOIDC.Issuer)
	}
	return settings, nil
}

func validateOIDC(settings authsettings.OIDC) error {
	if len([]rune(settings.Label)) > 64 {
		return fmt.Errorf("label must be at most 64 characters")
	}
	if settings.Issuer != "" {
		if err := validateHTTPSURL(settings.Issuer); err != nil {
			return fmt.Errorf("issuer must be an exact HTTPS URL: %w", err)
		}
	}
	for _, redirect := range settings.RedirectURLs {
		if err := validateHTTPSURL(redirect); err != nil {
			return fmt.Errorf("redirect URLs must be exact HTTPS URLs without query or fragment: %w", err)
		}
	}
	if settings.Enabled {
		if settings.Issuer == "" || settings.ClientID == "" || settings.ClientSecret == "" {
			return fmt.Errorf("issuer, client id and client secret are required when OIDC is enabled")
		}
	}
	return nil
}

func validateHTTPSURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%q is not an exact HTTPS URL", raw)
	}
	return nil
}

func normalizeRedirectURLs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func normalizeScopes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func oidcResponse(settings authsettings.OIDC, source string) oidcSettingsResponse {
	return oidcSettingsResponse{
		Enabled:        settings.Enabled,
		Label:          settings.Label,
		Issuer:         settings.Issuer,
		ClientID:       settings.ClientID,
		ClientSecret:   maskSecret(settings.ClientSecret),
		RedirectURLs:   settings.RedirectURLs,
		AllowEmailLink: settings.AllowEmailLink,
		Scopes:         settings.Scopes,
		Source:         source,
		CallbackPath:   "/auth/oidc/callback",
	}
}

func maskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}
	return maskedSecret
}
