package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

func TestDiscoverCommandRequiresLogin(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	cfg = nil
	apiClient = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})

	if err := runDiscover(discoverCmd, nil); err == nil {
		t.Fatal("expected login error")
	}
}

func TestDiscoverCommandConfiguresDetectedTools(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
	})

	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = client.New("http://example.com", "tok")
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}}, nil
	}
	configureDiscoveredTools = func(toolconfig.Options) (toolconfig.Result, error) {
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{Name: "codex"}}}, nil
	}
	apiClient = &client.Client{}

	var called bool
	listProvidersForDiscover = func(ctx context.Context) ([]client.ProviderInfo, error) {
		called = true
		return []client.ProviderInfo{{
			Name:      "primary",
			BaseURL:   "https://relay.example.com/v1",
			APIKey:    "sk-test",
			IsPrimary: true,
		}}, nil
	}
	t.Cleanup(func() { listProvidersForDiscover = defaultListProvidersForDiscover })

	buf := new(bytes.Buffer)
	discoverCmd.SetOut(buf)
	discoverCmd.SetErr(buf)

	if err := runDiscover(discoverCmd, nil); err != nil {
		t.Fatalf("runDiscover: %v", err)
	}
	if !called {
		t.Fatal("expected providers to be requested")
	}
	if got := buf.String(); got == "" {
		t.Fatal("expected command output")
	}
}
