package handler

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/gin-gonic/gin"
)

func TestOIDCCallbackURLUsesForwardedHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://internal:8081/auth/oidc/start", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "staging.ai-efficiency.example.com")

	got := oidcCallbackCandidates(c)
	if len(got) == 0 || got[0] != "https://staging.ai-efficiency.example.com/auth/oidc/callback" {
		t.Fatalf("oidcCallbackCandidates() = %q", got)
	}
}

func TestOIDCCallbackURLFallsBackToRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "http://ai-efficiency.example.test/auth/oidc/start", nil)

	got := oidcCallbackCandidates(c)
	if len(got) == 0 || got[0] != "http://ai-efficiency.example.test/auth/oidc/callback" {
		t.Fatalf("oidcCallbackCandidates() = %q", got)
	}
}

func TestOIDCCallbackURLPrefersPublicRequestHostOverStaleForwardedHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://ai-efficiency.example.com/auth/oidc/start", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "staging.ai-efficiency.example.com")

	got := oidcCallbackCandidates(c)
	if len(got) == 0 || got[0] != "https://ai-efficiency.example.com/auth/oidc/callback" {
		t.Fatalf("oidcCallbackCandidates() = %q", got)
	}
}

func TestOIDCCallbackURLUsesExplicitBrowserHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "https://internal:8081/auth/oidc/start?host=ai-efficiency.example.com", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "staging.ai-efficiency.example.com")

	got := oidcCallbackCandidates(c)
	if len(got) == 0 || got[0] != "https://ai-efficiency.example.com/auth/oidc/callback" {
		t.Fatalf("oidcCallbackCandidates() = %q", got)
	}
}

func TestOIDCCallbackFailureLocation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "identity conflict", err: fmt.Errorf("resolve OIDC identity: %w", auth.ErrOIDCIdentityConflict), want: "/login?oidc=conflict"},
		{name: "email linking unavailable", err: fmt.Errorf("resolve OIDC identity: %w", auth.ErrOIDCEmailLinkUnavailable), want: "/login?oidc=failed"},
		{name: "state store unavailable", err: fmt.Errorf("load pending: %w", authstate.ErrUnavailable), want: "/login?oidc=unavailable"},
		{name: "generic failure", err: errors.New("code exchange failed"), want: "/login?oidc=failed"},
		{name: "missing state", err: nil, want: "/login?oidc=failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oidcCallbackFailureLocation(tc.err); got != tc.want {
				t.Fatalf("oidcCallbackFailureLocation() = %q, want %q", got, tc.want)
			}
		})
	}
}
