package cmd

import (
	"testing"

	"github.com/ai-efficiency/ae-cli/config"
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
