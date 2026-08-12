package doctorcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	writeFile(t, path, string(data))
}

func testProvider() toolconfig.Provider {
	return toolconfig.Provider{
		Name:      "sub2api",
		BaseURL:   "https://relay.example.com/v1",
		IsPrimary: true,
		Credentials: []toolconfig.PlatformCredential{
			{Platform: "openai", APIKey: "sk-openai", Status: "active"},
			{Platform: "anthropic", APIKey: "sk-anthropic", Status: "active"},
			{Platform: "gemini", APIKey: "sk-gemini", Status: "active"},
		},
	}
}

func TestValidateToolsReportsConfiguredToolsAndCredentialMatches(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
model = 'gpt-5.5'
review_model = 'gpt-5.4'

[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
supports_websockets = false
`)
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{"OPENAI_API_KEY": "sk-openai"})
	writeJSON(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{
		"env": map[string]any{
			"ANTHROPIC_BASE_URL":                       "https://relay.example.com/v1",
			"ANTHROPIC_AUTH_TOKEN":                     "sk-anthropic",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_ATTRIBUTION_HEADER":           "0",
		},
	})
	writeFile(t, filepath.Join(home, ".ae-cli", "env.sh"), strings.Join([]string{
		"# BEGIN AE-CLI MANAGED",
		`export GEMINI_API_KEY="sk-gemini"`,
		`export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"`,
		"# END AE-CLI MANAGED",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(home, ".zshrc"), `[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"`+"\n")

	report := ValidateTools(ValidateOptions{
		HomeDir:           home,
		ShellPath:         "/bin/zsh",
		Provider:          testProvider(),
		ProviderAvailable: true,
		Tools: []ToolState{
			{Name: "codex", ExecutablePath: "/tmp/bin/codex", Probeable: true},
			{Name: "claude", ExecutablePath: "/tmp/bin/claude", Probeable: true},
			{Name: "gemini", ExecutablePath: "/tmp/bin/gemini", Probeable: true},
		},
	})

	if report.ProviderName != "sub2api" || report.ProviderSource != "user/providers" {
		t.Fatalf("provider = %q source=%q", report.ProviderName, report.ProviderSource)
	}
	for _, name := range []string{"codex", "claude", "gemini"} {
		result := report.ByName(name)
		if result == nil {
			t.Fatalf("missing result for %s", name)
		}
		if result.Status != StatusOK {
			t.Fatalf("%s status = %s details=%v", name, result.Status, result.Details)
		}
		if result.CredentialStatus != CredentialMatch {
			t.Fatalf("%s credential status = %s", name, result.CredentialStatus)
		}
		if result.BaseURLStatus != Match {
			t.Fatalf("%s base url status = %s", name, result.BaseURLStatus)
		}
	}
	if got := report.ByName("codex").ModelContract; got != Mismatch {
		t.Fatalf("codex model contract = %s, want mismatch", got)
	}
}

func TestValidateToolsRejectsCodexWebsocketTransport(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
model = 'gpt-5.4'
[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
supports_websockets = true
`)
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{"OPENAI_API_KEY": "sk-openai"})
	report := ValidateTools(ValidateOptions{HomeDir: home, Provider: testProvider(), ProviderAvailable: true, Tools: []ToolState{{Name: "codex", ExecutablePath: "/tmp/bin/codex", Probeable: true}}})
	result := report.ByName("codex")
	if result.Status != StatusFailed || !strings.Contains(strings.Join(result.Details, ","), "supports_websockets is not false") {
		t.Fatalf("result = %+v", result)
	}
}

func TestValidateToolsDoesNotPrintSecretOnCredentialMismatch(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
`)
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{"OPENAI_API_KEY": "wrong-secret"})

	report := ValidateTools(ValidateOptions{
		HomeDir:           home,
		ShellPath:         "/bin/zsh",
		Provider:          testProvider(),
		ProviderAvailable: true,
		Tools:             []ToolState{{Name: "codex", ExecutablePath: "/tmp/bin/codex", Probeable: true}},
	})

	line := FormatConfigResult(report.ByName("codex"))
	if strings.Contains(line, "wrong-secret") || strings.Contains(line, "sk-openai") {
		t.Fatalf("formatted line leaked secret: %s", line)
	}
	if !strings.Contains(line, "credential=mismatch") {
		t.Fatalf("formatted line = %s, want credential mismatch", line)
	}
}

func TestValidateToolsReportsMissingExecutableAsFailedWhenCredentialExists(t *testing.T) {
	report := ValidateTools(ValidateOptions{
		HomeDir:           t.TempDir(),
		ShellPath:         "/bin/zsh",
		Provider:          testProvider(),
		ProviderAvailable: true,
		Tools:             []ToolState{{Name: "gemini", Missing: true}},
	})
	result := report.ByName("gemini")
	if result.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if result.SkipReason != "executable not found" {
		t.Fatalf("skip reason = %q", result.SkipReason)
	}
}

func TestValidateToolsReportsMissingExecutableAsSkippedWithoutCredential(t *testing.T) {
	provider := toolconfig.Provider{Name: "sub2api", BaseURL: "https://relay.example.com/v1", IsPrimary: true}
	report := ValidateTools(ValidateOptions{
		HomeDir:           t.TempDir(),
		ShellPath:         "/bin/zsh",
		Provider:          provider,
		ProviderAvailable: true,
		Tools:             []ToolState{{Name: "claude", Missing: true}},
	})
	result := report.ByName("claude")
	if result.Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", result.Status)
	}
}
