package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestOAuthFlowExchangesCode(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" && r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "test-access-token",
				"refresh_token": "test-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	token, err := auth.ExchangeCode(context.Background(), backend.URL, "test-code", "http://localhost:12345/callback", "test-verifier")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if token.AccessToken != "test-access-token" {
		t.Fatalf("unexpected access token: %s", token.AccessToken)
	}
}

func TestExchangeCodeShowsIngressMessageErrors(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Your IP address is not allowed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	_, err := auth.ExchangeCode(context.Background(), backend.URL, "test-code", "http://localhost:12345/callback", "test-verifier")
	if err == nil {
		t.Fatal("ExchangeCode() error = nil, want ingress message")
	}
	if !strings.Contains(err.Error(), "HTTP 403 Forbidden") ||
		!strings.Contains(err.Error(), "Your IP address is not allowed") {
		t.Fatalf("err = %v, want status and ingress message", err)
	}
}
