package cmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
)

func TestResolveLoginServerURLPrefersLoadedConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			URL: "http://localhost:18081",
		},
	}

	got := resolveLoginServerURL(cfg, "http://localhost:8081")
	if got != "http://localhost:18081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:18081")
	}
}

func TestResolveLoginServerURLFallsBackToBuildInfoDefault(t *testing.T) {
	got := resolveLoginServerURL(nil, "http://localhost:8081")
	if got != "http://localhost:8081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:8081")
	}
}

func TestResolveLoginServerURLIgnoresBlankConfiguredValue(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			URL: "   ",
		},
	}

	got := resolveLoginServerURL(cfg, "  http://localhost:8081  ")
	if got != "http://localhost:8081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:8081")
	}
}

func TestLoginCommandSkipsOAuthWhenValidTokenExists(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldLogin := loginFlow
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginFlow = oldLogin
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = false

	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatalf("DefaultTokenPath: %v", err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	called := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		called = true
		return nil, nil
	}

	buf := new(bytes.Buffer)
	loginCmd.SetOut(buf)

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if called {
		t.Fatal("expected OAuth login flow to be skipped when a valid token already exists")
	}
	if got := buf.String(); !strings.Contains(got, "Already logged in. Use --force to re-login.") {
		t.Fatalf("output = %q, want already logged in message", got)
	}
}

func TestLoginCommandForceBypassesExistingToken(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldLogin := loginFlow
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginFlow = oldLogin
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = true

	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatalf("DefaultTokenPath: %v", err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	called := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		called = true
		return &auth.OAuthResult{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
		}, nil
	}

	buf := new(bytes.Buffer)
	loginCmd.SetOut(buf)

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if !called {
		t.Fatal("expected OAuth login flow to run when --force is enabled")
	}

	saved, err := auth.ReadToken(tokenPath)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if saved.AccessToken != "new-access-token" {
		t.Fatalf("access_token = %q, want %q", saved.AccessToken, "new-access-token")
	}
}
