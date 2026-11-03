package handler

import (
	"net/http/httptest"
	"testing"

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
