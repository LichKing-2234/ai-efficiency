package cmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestResolveTokenFromTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "oauth-access-token",
		RefreshToken: "oauth-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}
	if err := auth.WriteToken(tokenPath, token); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	got := resolveToken("", tokenPath)
	if got != "oauth-access-token" {
		t.Errorf("resolveToken with empty config token: got %q, want %q", got, "oauth-access-token")
	}
}

func TestResolveTokenOAuthTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "oauth-access-token",
		RefreshToken: "oauth-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}
	if err := auth.WriteToken(tokenPath, token); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	// When both config token and valid token.json exist, token.json wins
	got := resolveToken("config-token", tokenPath)
	if got != "oauth-access-token" {
		t.Errorf("resolveToken should prefer token.json: got %q, want %q", got, "oauth-access-token")
	}
}

func TestResolveTokenFallsBackToConfig(t *testing.T) {
	// When token.json is missing, fall back to config token
	got := resolveToken("config-token", "/nonexistent/token.json")
	if got != "config-token" {
		t.Errorf("resolveToken should fall back to config: got %q, want %q", got, "config-token")
	}
}

func TestResolveTokenFallsBackToConfigWhenExpired(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "expired-oauth-token",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}
	if err := auth.WriteToken(tokenPath, token); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	// When token.json is expired, fall back to config token
	got := resolveToken("config-token", tokenPath)
	if got != "config-token" {
		t.Errorf("resolveToken should fall back to config when token.json expired: got %q, want %q", got, "config-token")
	}
}

func TestResolveTokenExpiredTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	token := &auth.TokenFile{
		AccessToken:  "expired-token",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		ServerURL:    "http://localhost:8081",
	}
	if err := auth.WriteToken(tokenPath, token); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	got := resolveToken("", tokenPath)
	if got != "" {
		t.Errorf("resolveToken with expired token: got %q, want empty", got)
	}
}

func TestResolveTokenMissingFile(t *testing.T) {
	got := resolveToken("", "/nonexistent/token.json")
	if got != "" {
		t.Errorf("resolveToken with missing file: got %q, want empty", got)
	}
}

func TestResolveTokenFileWithRefreshRefreshesExpiringToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "old-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Minute),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	oldRefresh := refreshAccessToken
	defer func() { refreshAccessToken = oldRefresh }()

	refreshAccessToken = func(_ context.Context, serverURL, refreshToken string) (*auth.OAuthResult, error) {
		if serverURL != "http://localhost:18081" {
			t.Fatalf("serverURL = %q, want http://localhost:18081", serverURL)
		}
		if refreshToken != "refresh-token" {
			t.Fatalf("refreshToken = %q, want refresh-token", refreshToken)
		}
		return &auth.OAuthResult{
			AccessToken:  "new-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    7200,
		}, nil
	}

	tf, ok := resolveTokenFileWithRefresh("", tokenPath)
	if !ok {
		t.Fatal("expected refreshed token file")
	}
	if tf.AccessToken != "new-token" {
		t.Fatalf("access token = %q, want new-token", tf.AccessToken)
	}
	if tf.RefreshToken != "new-refresh-token" {
		t.Fatalf("refresh token = %q, want new-refresh-token", tf.RefreshToken)
	}

	saved, err := auth.ReadToken(tokenPath)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if saved.AccessToken != "new-token" {
		t.Fatalf("saved access token = %q, want new-token", saved.AccessToken)
	}
}

func TestResolveTokenFileWithRefreshKeepsValidTokenWhenRefreshFails(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "still-valid-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Minute),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	oldRefresh := refreshAccessToken
	defer func() { refreshAccessToken = oldRefresh }()

	refreshAccessToken = func(context.Context, string, string) (*auth.OAuthResult, error) {
		return nil, context.DeadlineExceeded
	}

	tf, ok := resolveTokenFileWithRefresh("", tokenPath)
	if !ok {
		t.Fatal("expected existing valid token to be kept")
	}
	if tf.AccessToken != "still-valid-token" {
		t.Fatalf("access token = %q, want still-valid-token", tf.AccessToken)
	}
}

func TestResolveTokenFileWithRefreshRejectsExpiredTokenWhenRefreshFails(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")

	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	oldRefresh := refreshAccessToken
	defer func() { refreshAccessToken = oldRefresh }()

	refreshAccessToken = func(context.Context, string, string) (*auth.OAuthResult, error) {
		return nil, context.DeadlineExceeded
	}

	if tf, ok := resolveTokenFileWithRefresh("", tokenPath); ok || tf != nil {
		t.Fatalf("expected expired token to remain unusable after refresh failure, got ok=%v token=%v", ok, tf)
	}
}
