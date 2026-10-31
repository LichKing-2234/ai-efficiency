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
	c.Request.Header.Set("X-Forwarded-Host", "ai-efficiency-staging.la3.agoralab.co")

	if got, want := oidcCallbackURL(c), "https://ai-efficiency-staging.la3.agoralab.co/auth/oidc/callback"; got != want {
		t.Fatalf("oidcCallbackURL() = %q, want %q", got, want)
	}
}

func TestOIDCCallbackURLFallsBackToRequestHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "http://ai-efficiency.example.test/auth/oidc/start", nil)

	if got, want := oidcCallbackURL(c), "http://ai-efficiency.example.test/auth/oidc/callback"; got != want {
		t.Fatalf("oidcCallbackURL() = %q, want %q", got, want)
	}
}
