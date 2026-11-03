// Package authsettings stores administrator-managed login configuration in the
// database so it can be changed at runtime and survives restarts and multiple
// replicas. Secrets are encrypted with the platform encryption key.
package authsettings

import (
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/config"
)

const (
	// OIDCKey is the SystemSetting key holding browser OIDC configuration.
	OIDCKey = "auth.oidc"
	// LDAPKey is the SystemSetting key holding LDAP configuration.
	LDAPKey = "auth.ldap"
)

// OIDC is the administrator-managed browser OIDC configuration.
type OIDC struct {
	Enabled        bool
	Label          string
	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	RedirectURLs   []string
	AllowEmailLink bool
	Scopes         []string
	// UpdatedAt is the database write timestamp; it is not part of the stored
	// document and only versions the runtime reload across replicas.
	UpdatedAt time.Time
}

// LDAP is the administrator-managed LDAP configuration.
type LDAP struct {
	URL          string
	BaseDN       string
	BindDN       string
	BindPassword string
	UserFilter   string
	TLS          bool
}

// DefaultScopes are used when the administrator leaves scopes empty.
func DefaultScopes() []string { return []string{"openid", "profile", "email"} }

// FromConfigOIDC converts deployment OIDC configuration into stored settings.
func FromConfigOIDC(cfg config.OIDCConfig) OIDC {
	return OIDC{
		Enabled:        cfg.Enabled,
		Issuer:         cfg.Issuer,
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		RedirectURL:    cfg.RedirectURL,
		RedirectURLs:   append([]string(nil), cfg.RedirectURLs...),
		AllowEmailLink: cfg.AllowEmailLink,
		Scopes:         append([]string(nil), cfg.Scopes...),
	}
}

// FromConfigLDAP converts deployment LDAP configuration into stored settings.
func FromConfigLDAP(cfg config.LDAPConfig) LDAP {
	return LDAP{
		URL:          cfg.URL,
		BaseDN:       cfg.BaseDN,
		BindDN:       cfg.BindDN,
		BindPassword: cfg.BindPassword,
		UserFilter:   cfg.UserFilter,
		TLS:          cfg.TLS,
	}
}

// AuthConfig renders the stored settings into the runtime OIDC client config.
// The single redirect URL is either the explicitly configured one or derived
// from the frontend URL; the allowlist keeps the explicit browser hosts.
func (o OIDC) AuthConfig(frontendURL string) auth.OIDCConfig {
	redirect := strings.TrimSpace(o.RedirectURL)
	if redirect == "" {
		redirect = strings.TrimRight(strings.TrimSpace(frontendURL), "/") + "/auth/oidc/callback"
	}
	scopes := o.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes()
	}
	redirectURLs := make([]string, 0, len(o.RedirectURLs))
	for _, value := range o.RedirectURLs {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			redirectURLs = append(redirectURLs, trimmed)
		}
	}
	return auth.OIDCConfig{
		Enabled:        o.Enabled,
		Label:          strings.TrimSpace(o.Label),
		Issuer:         strings.TrimSpace(o.Issuer),
		ClientID:       strings.TrimSpace(o.ClientID),
		ClientSecret:   o.ClientSecret,
		RedirectURL:    redirect,
		RedirectURLs:   redirectURLs,
		AllowEmailLink: o.AllowEmailLink,
		Scopes:         append([]string(nil), scopes...),
	}
}

// Config renders the stored LDAP settings into the runtime config shape.
func (l LDAP) Config() config.LDAPConfig {
	return config.LDAPConfig{
		URL:          strings.TrimSpace(l.URL),
		BaseDN:       strings.TrimSpace(l.BaseDN),
		BindDN:       strings.TrimSpace(l.BindDN),
		BindPassword: l.BindPassword,
		UserFilter:   strings.TrimSpace(l.UserFilter),
		TLS:          l.TLS,
	}
}
