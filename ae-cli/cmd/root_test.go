package cmd

import (
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
