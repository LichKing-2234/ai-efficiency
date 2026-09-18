package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func TestPreserveDiscoveredRelayProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := reporting.Save("", &reporting.Config{Version: 1, InstallationID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	if err := preserveDiscoveredRelayProvider(17); err != nil {
		t.Fatal(err)
	}
	config, err := reporting.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if config.RelayProviderID != 17 {
		t.Fatalf("relay_provider_id = %d, want 17", config.RelayProviderID)
	}
}

func TestDiscoverCommandRequiresLogin(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	oldToolNames := discoverToolNames
	cfg = nil
	apiClient = nil
	discoverToolNames = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverToolNames = oldToolNames
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
	oldActivation := activateAfterDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
	})

	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = client.New("http://example.com", "tok")
	discoverToolNames = nil
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
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return &reporting.Config{}, nil
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

func TestDiscoverCommandActivatesReportingAndPreservesSelectedProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", home+"/.gitconfig")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	var ensureCalls, enableCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			var request client.EnsureInstallationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + request.InstallationID + `","reporter_token":"reporter-secret","reporting_enabled":false,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/attribution/installations/"):
			enableCalls++
			installationID := strings.TrimPrefix(r.URL.Path, "/api/v1/attribution/installations/")
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + installationID + `","reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	if err := auth.WriteToken(home+"/.ae-cli/token.json", &auth.TokenFile{
		AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour), ServerURL: server.URL, AuthSubject: "user:123",
	}); err != nil {
		t.Fatal(err)
	}

	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldActivation := activateAfterDiscover
	oldProviderLister := listProvidersForDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		listProvidersForDiscover = oldProviderLister
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL, Token: "access-token"}}
	apiClient = client.New(server.URL, "access-token")
	discoverProviderName = ""
	discoverDryRun = false
	discoverToolNames = nil
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}}, nil
	}
	configureDiscoveredTools = func(toolconfig.Options) (toolconfig.Result, error) {
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{Name: "codex"}}}, nil
	}
	listProvidersForDiscover = func(context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{ID: 17, Name: "primary", IsPrimary: true}}, nil
	}

	var stdout, stderr bytes.Buffer
	discoverCmd.SetOut(&stdout)
	discoverCmd.SetErr(&stderr)
	if err := runDiscover(discoverCmd, nil); err != nil {
		t.Fatalf("runDiscover: %v", err)
	}
	if ensureCalls != 1 || enableCalls != 1 {
		t.Fatalf("reporting calls ensure=%d enable=%d, want 1/1; stdout=%q stderr=%q", ensureCalls, enableCalls, stdout.String(), stderr.String())
	}
	reportingConfig, err := reporting.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if reportingConfig.RelayProviderID != 17 || !reportingConfig.ReportingEnabled {
		t.Fatalf("reporting config = %+v, want provider 17 and enabled", reportingConfig)
	}
}

func TestDiscoverCommandDoesNotActivateWhenNoSupportedToolIsDetected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldActivation := activateAfterDiscover
	oldProviderLister := listProvidersForDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		activateAfterDiscover = oldActivation
		listProvidersForDiscover = oldProviderLister
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "access-token"}}
	apiClient = &client.Client{}
	discoverProviderName = ""
	discoverDryRun = false
	discoverToolNames = nil
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) { return nil, nil }
	listProvidersForDiscover = func(context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{ID: 17, Name: "primary", IsPrimary: true}}, nil
	}
	var activationCalls int
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		activationCalls++
		return &reporting.Config{}, nil
	}

	discoverCmd.SetOut(new(bytes.Buffer))
	discoverCmd.SetErr(new(bytes.Buffer))
	if err := runDiscover(discoverCmd, nil); err != nil {
		t.Fatal(err)
	}
	if activationCalls != 0 {
		t.Fatalf("reporting activation calls = %d, want 0", activationCalls)
	}
}

func TestDiscoverCommandDoesNotActivateWhenNoToolConfigurationIsWritten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldActivation := activateAfterDiscover
	oldProviderLister := listProvidersForDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		listProvidersForDiscover = oldProviderLister
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "access-token"}}
	apiClient = &client.Client{}
	discoverProviderName = ""
	discoverDryRun = false
	discoverToolNames = nil
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}}, nil
	}
	configureDiscoveredTools = func(toolconfig.Options) (toolconfig.Result, error) { return toolconfig.Result{}, nil }
	listProvidersForDiscover = func(context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{ID: 17, Name: "primary", IsPrimary: true}}, nil
	}
	var activationCalls int
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		activationCalls++
		return &reporting.Config{}, nil
	}

	discoverCmd.SetOut(new(bytes.Buffer))
	discoverCmd.SetErr(new(bytes.Buffer))
	if err := runDiscover(discoverCmd, nil); err != nil {
		t.Fatal(err)
	}
	if activationCalls != 0 {
		t.Fatalf("reporting activation calls = %d, want 0", activationCalls)
	}
}

func TestDiscoverCommandActivationBoundaries(t *testing.T) {
	t.Run("dry run does not activate", func(t *testing.T) {
		calls, _, err := runDiscoverActivationCase(t, true, nil, nil)
		if err != nil || calls != 0 {
			t.Fatalf("activation_calls=%d err=%v", calls, err)
		}
	})
	t.Run("configuration failure does not activate", func(t *testing.T) {
		calls, _, err := runDiscoverActivationCase(t, false, errors.New("config write failed"), nil)
		if err == nil || !strings.Contains(err.Error(), "config write failed") || calls != 0 {
			t.Fatalf("activation_calls=%d err=%v", calls, err)
		}
	})
	t.Run("activation failure is a warning after successful configuration", func(t *testing.T) {
		calls, stderr, err := runDiscoverActivationCase(t, false, nil, errors.New("reporting unavailable"))
		if err != nil || calls != 1 || !strings.Contains(stderr, "tool configuration succeeded, but reporting activation is degraded") {
			t.Fatalf("activation_calls=%d stderr=%q err=%v", calls, stderr, err)
		}
	})
}

func runDiscoverActivationCase(t *testing.T, dryRun bool, configureErr, activationErr error) (int, string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldActivation := activateAfterDiscover
	oldProviderLister := listProvidersForDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		listProvidersForDiscover = oldProviderLister
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "access-token"}}
	apiClient = &client.Client{}
	discoverProviderName = ""
	discoverDryRun = dryRun
	discoverToolNames = nil
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}}, nil
	}
	configureDiscoveredTools = func(toolconfig.Options) (toolconfig.Result, error) {
		if configureErr != nil {
			return toolconfig.Result{}, configureErr
		}
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{Name: "codex"}}}, nil
	}
	listProvidersForDiscover = func(context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{ID: 17, Name: "primary", IsPrimary: true}}, nil
	}
	var activationCalls int
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		activationCalls++
		return &reporting.Config{}, activationErr
	}

	var stderr bytes.Buffer
	discoverCmd.SetOut(new(bytes.Buffer))
	discoverCmd.SetErr(&stderr)
	err := runDiscover(discoverCmd, nil)
	return activationCalls, stderr.String(), err
}

func TestDiscoverCommandPrintsGeminiModelGuidance(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldActivation := activateAfterDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	oldProviderLister := listProvidersForDiscover
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
		listProvidersForDiscover = oldProviderLister
	})

	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = &client.Client{}
	discoverProviderName = ""
	discoverDryRun = false
	discoverToolNames = nil
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		return []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}}, nil
	}
	configureDiscoveredTools = func(opts toolconfig.Options) (toolconfig.Result, error) {
		return toolconfig.Result{Configured: []toolconfig.ConfiguredTool{{
			Name:  "gemini",
			Paths: []string{"/home/alice/.ae-cli/env.sh", "/home/alice/.zshrc"},
		}}}, nil
	}
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return &reporting.Config{}, nil
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

func TestResolveDiscoverToolsUsesExplicitSelection(t *testing.T) {
	oldLister := discoverInstalledTools
	discoverInstalledTools = func([]string) ([]toolconfig.InstalledTool, error) {
		t.Fatal("explicit selection must bypass installation detection")
		return nil, nil
	}
	t.Cleanup(func() { discoverInstalledTools = oldLister })

	got, err := resolveDiscoverTools([]string{"claude", "codex", "claude"})
	if err != nil {
		t.Fatalf("resolveDiscoverTools: %v", err)
	}
	want := []toolconfig.InstalledTool{{Name: "claude"}, {Name: "codex"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}

func TestResolveDiscoverToolsRejectsUnsupportedOrBlankTools(t *testing.T) {
	for _, explicit := range [][]string{{"cursor"}, {" "}} {
		_, err := resolveDiscoverTools(explicit)
		if err == nil || !strings.Contains(err.Error(), "supported tools: codex, claude, gemini") {
			t.Fatalf("resolveDiscoverTools(%q) error = %v", explicit, err)
		}
	}
}

func TestResolveDiscoverToolsUsesDetectionByDefault(t *testing.T) {
	oldLister := discoverInstalledTools
	want := []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}}
	discoverInstalledTools = func(names []string) ([]toolconfig.InstalledTool, error) {
		if !reflect.DeepEqual(names, defaultDiscoverToolNames) {
			t.Fatalf("tool names = %v, want %v", names, defaultDiscoverToolNames)
		}
		return want, nil
	}
	t.Cleanup(func() { discoverInstalledTools = oldLister })

	got, err := resolveDiscoverTools(nil)
	if err != nil {
		t.Fatalf("resolveDiscoverTools: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v, want %+v", got, want)
	}
}

func TestDiscoverCommandToolFlag(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		want          []toolconfig.InstalledTool
		wantError     bool
		wantDetection bool
	}{
		{
			name:      "rejects explicit blank value",
			args:      []string{"--tool="},
			wantError: true,
		},
		{
			name:      "rejects blank repeated value",
			args:      []string{"--tool", "codex", "--tool="},
			wantError: true,
		},
		{
			name: "preserves repeated values",
			args: []string{"--tool", "codex", "--tool", "claude"},
			want: []toolconfig.InstalledTool{{Name: "codex"}, {Name: "claude"}},
		},
		{
			name: "splits comma separated values",
			args: []string{"--tool", "codex,claude"},
			want: []toolconfig.InstalledTool{{Name: "codex"}, {Name: "claude"}},
		},
		{
			name: "preserves first seen mixed order",
			args: []string{"--tool", "claude,codex", "--tool", "gemini,claude", "--tool", "codex"},
			want: []toolconfig.InstalledTool{{Name: "claude"}, {Name: "codex"}, {Name: "gemini"}},
		},
		{
			name:          "detects tools when flag is omitted",
			want:          []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}},
			wantDetection: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detectionCalled, err := executeDiscoverWithToolArgs(t, tt.args)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "supported tools: codex, claude, gemini") {
					t.Fatalf("error = %v, want supported-tools validation error", err)
				}
			} else if err != nil {
				t.Fatalf("execute discover: %v", err)
			}
			if detectionCalled != tt.wantDetection {
				t.Fatalf("installation detection called = %v, want %v", detectionCalled, tt.wantDetection)
			}
			if !tt.wantError && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func executeDiscoverWithToolArgs(t *testing.T, args []string) ([]toolconfig.InstalledTool, bool, error) {
	t.Helper()

	oldCfg := cfg
	oldClient := apiClient
	oldLister := discoverInstalledTools
	oldConfigurer := configureDiscoveredTools
	oldActivation := activateAfterDiscover
	oldProviderName := discoverProviderName
	oldDryRun := discoverDryRun
	oldToolNames := discoverToolNames
	oldProviderLister := listProvidersForDiscover
	productionToolFlag := discoverCmd.Flags().Lookup("tool")
	testCommand := &cobra.Command{
		Use:          "discover",
		SilenceUsage: true,
		RunE:         runDiscover,
	}
	switch productionToolFlag.Value.Type() {
	case "stringSlice":
		testCommand.Flags().StringSliceVar(&discoverToolNames, "tool", nil, productionToolFlag.Usage)
	case "stringArray":
		testCommand.Flags().StringArrayVar(&discoverToolNames, "tool", nil, productionToolFlag.Usage)
	default:
		t.Fatalf("unsupported --tool flag type %q", productionToolFlag.Value.Type())
	}

	discoverProviderName = ""
	discoverDryRun = false
	discoverToolNames = nil

	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		discoverInstalledTools = oldLister
		configureDiscoveredTools = oldConfigurer
		activateAfterDiscover = oldActivation
		discoverProviderName = oldProviderName
		discoverDryRun = oldDryRun
		discoverToolNames = oldToolNames
		listProvidersForDiscover = oldProviderLister
	})

	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = &client.Client{}
	listProvidersForDiscover = func(context.Context) ([]client.ProviderInfo, error) {
		return []client.ProviderInfo{{
			Name:      "primary",
			BaseURL:   "https://relay.example.com/v1",
			IsPrimary: true,
			Credentials: []client.ProviderCredentialInfo{
				{Platform: "openai", APIKey: "sk-openai"},
				{Platform: "anthropic", APIKey: "sk-anthropic"},
				{Platform: "gemini", APIKey: "sk-gemini"},
			},
		}}, nil
	}

	detectionCalled := false
	discoverInstalledTools = func(names []string) ([]toolconfig.InstalledTool, error) {
		detectionCalled = true
		if !reflect.DeepEqual(names, defaultDiscoverToolNames) {
			t.Fatalf("tool names = %v, want %v", names, defaultDiscoverToolNames)
		}
		return []toolconfig.InstalledTool{{Name: "gemini", Path: "/usr/local/bin/gemini"}}, nil
	}

	var configured []toolconfig.InstalledTool
	configureDiscoveredTools = func(opts toolconfig.Options) (toolconfig.Result, error) {
		configured = append([]toolconfig.InstalledTool(nil), opts.Tools...)
		result := toolconfig.Result{Configured: make([]toolconfig.ConfiguredTool, 0, len(opts.Tools))}
		for _, tool := range opts.Tools {
			result.Configured = append(result.Configured, toolconfig.ConfiguredTool{Name: tool.Name})
		}
		return result, nil
	}
	activateAfterDiscover = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return &reporting.Config{}, nil
	}

	testCommand.SetOut(new(bytes.Buffer))
	testCommand.SetErr(new(bytes.Buffer))
	testCommand.SetArgs(args)
	err := testCommand.Execute()
	return configured, detectionCalled, err
}
