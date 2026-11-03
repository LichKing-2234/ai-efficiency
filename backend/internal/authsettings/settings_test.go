package authsettings

import (
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
)

func TestFromConfigOIDCAndAuthConfig(t *testing.T) {
	settings := FromConfigOIDC(config.OIDCConfig{
		Enabled:        true,
		Issuer:         "https://idp.example.test/",
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		RedirectURLs:   []string{"https://app.example.test/auth/oidc/callback"},
		AllowEmailLink: true,
	})
	authConfig := settings.AuthConfig("https://app.example.test")
	if authConfig.Issuer != "https://idp.example.test/" ||
		authConfig.ClientID != "client-id" ||
		authConfig.ClientSecret != "client-secret" ||
		!authConfig.AllowEmailLink {
		t.Fatalf("AuthConfig() = %+v", authConfig)
	}
	if authConfig.RedirectURL != "https://app.example.test/auth/oidc/callback" {
		t.Fatalf("RedirectURL = %q", authConfig.RedirectURL)
	}
	if len(authConfig.Scopes) != 3 || authConfig.Scopes[0] != "openid" {
		t.Fatalf("Scopes = %v", authConfig.Scopes)
	}
}

func TestAuthConfigHonorsExplicitRedirectAndLabel(t *testing.T) {
	settings := OIDC{
		Enabled:     true,
		Label:       " Company SSO ",
		RedirectURL: "https://explicit.example.test/auth/oidc/callback",
	}
	authConfig := settings.AuthConfig("https://app.example.test")
	if authConfig.RedirectURL != "https://explicit.example.test/auth/oidc/callback" {
		t.Fatalf("RedirectURL = %q", authConfig.RedirectURL)
	}
	if authConfig.Label != "Company SSO" {
		t.Fatalf("Label = %q", authConfig.Label)
	}
}

func TestLDAPConfigRoundTrip(t *testing.T) {
	settings := FromConfigLDAP(config.LDAPConfig{
		URL:          " ldap://ldap.example.test:389 ",
		BaseDN:       "dc=example,dc=test",
		BindDN:       "cn=service,dc=example,dc=test",
		BindPassword: "bind-password",
		UserFilter:   "(uid=%s)",
		TLS:          true,
	})
	runtimeConfig := settings.Config()
	if runtimeConfig.URL != "ldap://ldap.example.test:389" ||
		runtimeConfig.BindPassword != "bind-password" ||
		!runtimeConfig.TLS {
		t.Fatalf("Config() = %+v", runtimeConfig)
	}
}
