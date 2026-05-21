package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestRefreshAccessToken(t *testing.T) {
	t.Parallel()

	var gotRefreshToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Fatalf("path = %q, want /api/v1/auth/refresh", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotRefreshToken = req.RefreshToken
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"tokens": map[string]any{
					"access_token":  "new-access-token",
					"refresh_token": "new-refresh-token",
					"expires_in":    7200,
				},
			},
		})
	}))
	defer srv.Close()

	result, err := auth.RefreshAccessToken(context.Background(), srv.URL, "refresh-123")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if gotRefreshToken != "refresh-123" {
		t.Fatalf("refresh_token = %q, want refresh-123", gotRefreshToken)
	}
	if result.AccessToken != "new-access-token" {
		t.Fatalf("access token = %q, want new-access-token", result.AccessToken)
	}
	if result.RefreshToken != "new-refresh-token" {
		t.Fatalf("refresh token = %q, want new-refresh-token", result.RefreshToken)
	}
	if result.ExpiresIn != 7200 {
		t.Fatalf("expires_in = %d, want 7200", result.ExpiresIn)
	}
}

func TestRefreshAccessTokenReturnsErrorOnUnauthorized(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":401,"message":"invalid refresh token"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := auth.RefreshAccessToken(context.Background(), srv.URL, "bad-refresh"); err == nil {
		t.Fatal("expected refresh error")
	}
}
