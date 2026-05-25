package toolconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestSelectProviderPrefersExplicitName(t *testing.T) {
	providers := []Provider{
		{Name: "primary", IsPrimary: true},
		{Name: "gemini"},
	}

	got, err := SelectProvider(providers, "gemini")
	if err != nil {
		t.Fatalf("SelectProvider: %v", err)
	}
	if got.Name != "gemini" {
		t.Fatalf("provider = %q, want %q", got.Name, "gemini")
	}
}

func TestSelectProviderFallsBackToPrimary(t *testing.T) {
	providers := []Provider{
		{Name: "secondary"},
		{Name: "primary", IsPrimary: true},
	}

	got, err := SelectProvider(providers, "")
	if err != nil {
		t.Fatalf("SelectProvider: %v", err)
	}
	if got.Name != "primary" {
		t.Fatalf("provider = %q, want %q", got.Name, "primary")
	}
}

func TestDetectInstalledToolsFindsKnownCommands(t *testing.T) {
	tmpBin := t.TempDir()
	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", tmpBin); err != nil {
		t.Fatalf("Setenv(PATH): %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	writeExecutable(t, tmpBin, "codex")
	writeExecutable(t, tmpBin, "gemini")

	got, err := DetectInstalledTools([]string{"codex", "claude", "gemini"})
	if err != nil {
		t.Fatalf("DetectInstalledTools: %v", err)
	}

	wantNames := []string{"codex", "gemini"}
	var gotNames []string
	for _, item := range got {
		gotNames = append(gotNames, item.Name)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("tool names = %v, want %v", gotNames, wantNames)
	}
}

func TestConfigureToolsWritesCodexClaudeAndGeminiWithPlatformCredentials(t *testing.T) {
	tmpHome := t.TempDir()
	provider := Provider{
		Name:         "relay-primary",
		DisplayName:  "Relay Primary",
		BaseURL:      "https://relay.example.com/v1",
		DefaultModel: "gpt-5.4",
		IsPrimary:    true,
		Credentials: []PlatformCredential{
			{Platform: "openai", APIKey: "sk-openai"},
			{Platform: "anthropic", APIKey: "sk-anthropic"},
			{Platform: "gemini", APIKey: "sk-gemini"},
		},
	}

	result, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider:  provider,
		Tools: []InstalledTool{
			{Name: "codex", Path: "/usr/local/bin/codex"},
			{Name: "claude", Path: "/usr/local/bin/claude"},
			{Name: "gemini", Path: "/usr/local/bin/gemini"},
		},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}
	if len(result.Configured) != 3 {
		t.Fatalf("configured count = %d, want 3", len(result.Configured))
	}
	if reflect.DeepEqual(result.Configured[0].Paths, []string{
		filepath.Join(tmpHome, ".codex", "config.toml"),
		filepath.Join(tmpHome, ".codex", "auth.json"),
	}) == false {
		t.Fatalf("codex paths = %v", result.Configured[0].Paths)
	}

	envPath := filepath.Join(tmpHome, ".ae-cli", "env.sh")
	envBody := mustReadFile(t, envPath)
	for _, want := range []string{
		`export GEMINI_API_KEY="sk-gemini"`,
		`export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"`,
	} {
		if !contains(envBody, want) {
			t.Fatalf("%s missing %q\n%s", envPath, want, envBody)
		}
	}
	if contains(envBody, "OPENAI_API_KEY") {
		t.Fatalf("codex key should not be written to env.sh:\n%s", envBody)
	}

	zshrc := mustReadFile(t, filepath.Join(tmpHome, ".zshrc"))
	if !contains(zshrc, "source \"$HOME/.ae-cli/env.sh\"") {
		t.Fatalf(".zshrc missing env source line:\n%s", zshrc)
	}

	codexCfg := mustReadFile(t, filepath.Join(tmpHome, ".codex", "config.toml"))
	for _, want := range []string{
		"model_provider = 'relay-primary'",
		"model = 'gpt-5.4'",
		"review_model = 'gpt-5.4'",
		"model_reasoning_effort = 'xhigh'",
		"disable_response_storage = true",
		"network_access = 'enabled'",
		"windows_wsl_setup_acknowledged = true",
		"model_context_window = 1000000",
		"model_auto_compact_token_limit = 900000",
		"[model_providers.relay-primary]",
		"name = 'relay-primary'",
		"base_url = 'https://relay.example.com/v1'",
		"wire_api = 'responses'",
		"requires_openai_auth = true",
	} {
		if !contains(codexCfg, want) {
			t.Fatalf("codex config missing %q\n%s", want, codexCfg)
		}
	}
	codexAuth := mustReadFile(t, filepath.Join(tmpHome, ".codex", "auth.json"))
	if !contains(codexAuth, `"OPENAI_API_KEY": "sk-openai"`) {
		t.Fatalf("codex auth missing OPENAI_API_KEY:\n%s", codexAuth)
	}

	claudeCfg := mustReadFile(t, filepath.Join(tmpHome, ".claude", "settings.json"))
	for _, want := range []string{
		"\"ANTHROPIC_AUTH_TOKEN\": \"sk-anthropic\"",
		"\"ANTHROPIC_BASE_URL\": \"https://relay.example.com/v1\"",
		"\"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC\": \"1\"",
		"\"CLAUDE_CODE_ATTRIBUTION_HEADER\": \"0\"",
	} {
		if !contains(claudeCfg, want) {
			t.Fatalf("claude settings missing %q\n%s", want, claudeCfg)
		}
	}
	if contains(claudeCfg, "ANTHROPIC_API_KEY") {
		t.Fatalf("claude settings should use ANTHROPIC_AUTH_TOKEN:\n%s", claudeCfg)
	}

	if _, err := os.Stat(filepath.Join(tmpHome, ".gemini", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no gemini settings file, got err=%v", err)
	}
}

func TestConfigureToolsWritesCodexUsingRelayProviderName(t *testing.T) {
	tmpHome := t.TempDir()

	_, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider: Provider{
			Name:    "relay.main",
			BaseURL: "https://relay.example.com/v1",
			Credentials: []PlatformCredential{
				{Platform: "openai", APIKey: "sk-openai"},
			},
		},
		Tools: []InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}

	body := mustReadFile(t, filepath.Join(tmpHome, ".codex", "config.toml"))
	if !contains(body, "model_provider = 'relay.main'") {
		t.Fatalf("codex config missing provider name:\n%s", body)
	}
	if !contains(body, "[model_providers.'relay.main']") {
		t.Fatalf("codex config missing quoted provider block:\n%s", body)
	}

	var parsed map[string]any
	if err := toml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("parse codex config: %v", err)
	}
	modelProviders, ok := parsed["model_providers"].(map[string]any)
	if !ok {
		t.Fatalf("model_providers type = %T, want map[string]any", parsed["model_providers"])
	}
	if _, ok := modelProviders["relay.main"]; !ok {
		t.Fatalf("model_providers missing relay.main key: %#v", modelProviders)
	}
}

func TestConfigureCodexAuthOnlyKeepsOpenAIAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	authPath := filepath.Join(tmpHome, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{"OPENAI_API_KEY":"old","OTHER_TOKEN":"remove-me","nested":{"x":1}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider: Provider{
			Name:    "relay-primary",
			BaseURL: "https://relay.example.com/v1",
			Credentials: []PlatformCredential{
				{Platform: "openai", APIKey: "sk-openai"},
			},
		},
		Tools: []InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}

	var auth map[string]any
	if err := json.Unmarshal([]byte(mustReadFile(t, authPath)), &auth); err != nil {
		t.Fatalf("parse auth.json: %v", err)
	}
	if len(auth) != 1 || auth["OPENAI_API_KEY"] != "sk-openai" {
		t.Fatalf("unexpected auth contents: %#v", auth)
	}
}

func TestConfigureToolsDoesNotTouchCodexAuthWithoutOpenAICredential(t *testing.T) {
	tmpHome := t.TempDir()
	authPath := filepath.Join(tmpHome, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	before := `{"OPENAI_API_KEY":"old","OTHER_TOKEN":"keep"}`
	if err := os.WriteFile(authPath, []byte(before), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider: Provider{
			Name:    "relay-primary",
			BaseURL: "https://relay.example.com/v1",
			Credentials: []PlatformCredential{
				{Platform: "anthropic", APIKey: "sk-anthropic"},
			},
		},
		Tools: []InstalledTool{{Name: "claude", Path: "/usr/local/bin/claude"}},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}

	if got := mustReadFile(t, authPath); got != before {
		t.Fatalf("auth.json changed unexpectedly:\n%s", got)
	}
}

func TestConfigureToolsSkipsToolsWithoutMatchingPlatformCredential(t *testing.T) {
	tmpHome := t.TempDir()
	provider := Provider{
		Name:         "openai-only",
		BaseURL:      "https://relay.example.com/v1",
		APIKey:       "sk-openai",
		DefaultModel: "gpt-5.4",
		Credentials: []PlatformCredential{
			{Platform: "openai", APIKey: "sk-openai"},
		},
	}

	result, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider:  provider,
		Tools: []InstalledTool{
			{Name: "codex", Path: "/usr/local/bin/codex"},
			{Name: "claude", Path: "/usr/local/bin/claude"},
			{Name: "gemini", Path: "/usr/local/bin/gemini"},
		},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}
	if len(result.Configured) != 1 || result.Configured[0].Name != "codex" {
		t.Fatalf("configured = %+v, want only codex", result.Configured)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings should not be written without anthropic credential, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".ae-cli", "env.sh")); !os.IsNotExist(err) {
		t.Fatalf("env.sh should not be written when only codex is configured, err=%v", err)
	}
}

func TestConfigureCodexUsesCodexModelNotProviderDefaultModel(t *testing.T) {
	tmpHome := t.TempDir()

	_, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider: Provider{
			Name:         "sub2api",
			BaseURL:      "https://relay.example.com/v1",
			DefaultModel: "claude-sonnet-4-20250514",
			Credentials: []PlatformCredential{
				{Platform: "openai", APIKey: "sk-openai"},
			},
		},
		Tools: []InstalledTool{{Name: "codex", Path: "/usr/local/bin/codex"}},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}

	codexCfg := mustReadFile(t, filepath.Join(tmpHome, ".codex", "config.toml"))
	for _, want := range []string{
		"model = 'gpt-5.4'",
		"review_model = 'gpt-5.4'",
	} {
		if !contains(codexCfg, want) {
			t.Fatalf("codex config missing %q\n%s", want, codexCfg)
		}
	}
	if contains(codexCfg, "claude-sonnet-4-20250514") {
		t.Fatalf("codex config should not use provider default model:\n%s", codexCfg)
	}
}

func TestConfigureToolsPreservesExistingUserSettings(t *testing.T) {
	tmpHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpHome, ".claude", "settings.json"), []byte(`{"permissions":{"allow":["Bash(git status)"]},"env":{"FOO":"bar"}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ConfigureTools(Options{
		HomeDir:   tmpHome,
		ShellPath: "/bin/zsh",
		Provider: Provider{
			BaseURL:      "https://relay.example.com/v1",
			DefaultModel: "claude-sonnet-4-20250514",
			Credentials: []PlatformCredential{
				{Platform: "anthropic", APIKey: "sk-test-123"},
			},
		},
		Tools: []InstalledTool{{Name: "claude", Path: "/usr/local/bin/claude"}},
	})
	if err != nil {
		t.Fatalf("ConfigureTools: %v", err)
	}

	body := mustReadFile(t, filepath.Join(tmpHome, ".claude", "settings.json"))
	if !contains(body, "\"allow\": [") || !contains(body, "\"FOO\": \"bar\"") {
		t.Fatalf("existing settings were not preserved:\n%s", body)
	}
}

func writeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func contains(body, want string) bool {
	return strings.Contains(body, want)
}
