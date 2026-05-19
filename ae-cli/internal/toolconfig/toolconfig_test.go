package toolconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

func TestConfigureToolsWritesCodexClaudeAndGeminiEnvOnly(t *testing.T) {
	tmpHome := t.TempDir()
	provider := Provider{
		Name:         "relay-primary",
		DisplayName:  "Relay Primary",
		BaseURL:      "https://relay.example.com/v1",
		APIKey:       "sk-test-123",
		DefaultModel: "gpt-5.3-codex",
		IsPrimary:    true,
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

	envPath := filepath.Join(tmpHome, ".ae-cli", "env.sh")
	envBody := mustReadFile(t, envPath)
	for _, want := range []string{
		`export OPENAI_API_KEY="sk-test-123"`,
		`export GEMINI_API_KEY="sk-test-123"`,
		`export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"`,
	} {
		if !contains(envBody, want) {
			t.Fatalf("%s missing %q\n%s", envPath, want, envBody)
		}
	}

	zshrc := mustReadFile(t, filepath.Join(tmpHome, ".zshrc"))
	if !contains(zshrc, "source \"$HOME/.ae-cli/env.sh\"") {
		t.Fatalf(".zshrc missing env source line:\n%s", zshrc)
	}

	codexCfg := mustReadFile(t, filepath.Join(tmpHome, ".codex", "config.toml"))
	for _, want := range []string{
		"openai_base_url = 'https://relay.example.com/v1'",
		"model = 'gpt-5.3-codex'",
	} {
		if !contains(codexCfg, want) {
			t.Fatalf("codex config missing %q\n%s", want, codexCfg)
		}
	}

	claudeCfg := mustReadFile(t, filepath.Join(tmpHome, ".claude", "settings.json"))
	for _, want := range []string{
		"\"ANTHROPIC_API_KEY\": \"sk-test-123\"",
		"\"ANTHROPIC_BASE_URL\": \"https://relay.example.com/v1\"",
		"\"model\": \"gpt-5.3-codex\"",
	} {
		if !contains(claudeCfg, want) {
			t.Fatalf("claude settings missing %q\n%s", want, claudeCfg)
		}
	}

	if _, err := os.Stat(filepath.Join(tmpHome, ".gemini", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no gemini settings file, got err=%v", err)
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
			APIKey:       "sk-test-123",
			DefaultModel: "claude-sonnet-4-20250514",
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
