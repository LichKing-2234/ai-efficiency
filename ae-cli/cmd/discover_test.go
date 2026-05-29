package cmd

import (
	"bytes"
	"context"
	"strings"
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
	configureDiscoveredTools = func(opts toolconfig.Options) (toolconfig.Result, error) {
		if len(opts.Provider.Credentials) != 1 {
			t.Fatalf("provider credentials = %+v, want one openai credential", opts.Provider.Credentials)
		}
		if opts.Provider.Credentials[0].Platform != "openai" || opts.Provider.Credentials[0].APIKey != "sk-openai" {
			t.Fatalf("provider credential = %+v, want openai/sk-openai", opts.Provider.Credentials[0])
		}
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{Name: "codex"}}}, nil
	}
	apiClient = &client.Client{}

	var called bool
	listProvidersForDiscover = func(ctx context.Context) ([]client.ProviderInfo, error) {
		called = true
		return []client.ProviderInfo{{
			Name:      "primary",
			BaseURL:   "https://relay.example.com/v1",
			IsPrimary: true,
			Credentials: []client.ProviderCredentialInfo{
				{Platform: "openai", APIKey: "sk-openai"},
			},
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

func TestDiscoverCommandPrintsGeminiModelGuidance(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldProviderLister := listProvidersForDiscover
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		listProvidersForDiscover = oldProviderLister
	})

	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = &client.Client{}
	discoverProviderName = ""
	discoverDryRun = false
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}}, nil
	}
	configureDiscoveredTools = func(opts toolconfig.Options) (toolconfig.Result, error) {
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{
			Name:  "gemini",
			Paths: []string{"/home/alice/.ae-cli/env.sh", "/home/alice/.zshrc"},
		}}}, nil
	}
	listProvidersForDiscover = func(ctx context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{
			Name:      "primary",
			BaseURL:   "https://relay.example.com/v1",
			IsPrimary: true,
			Credentials: []client.ProviderCredentialInfo{
				{Platform: "gemini", APIKey: "sk-gemini"},
			},
		}}, nil
	}

	buf := new(bytes.Buffer)
	discoverCmd.SetOut(buf)
	discoverCmd.SetErr(buf)

	if err := runDiscover(discoverCmd, nil); err != nil {
		t.Fatalf("runDiscover: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Set GEMINI_MODEL so Gemini starts with the preview model directly.",
		`export GEMINI_MODEL="gemini-3.1-pro-preview"`,
		"Do not switch models manually inside Gemini.",
		`source "$HOME/.zshrc"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("discover output missing %q\n%s", want, out)
		}
	}
}
