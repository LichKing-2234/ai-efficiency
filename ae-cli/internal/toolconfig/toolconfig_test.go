package toolconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCodexSessionConfig(t *testing.T) {
	dir := t.TempDir()
	codexHome := filepath.Join(dir, "runtime", "sess-1", "codex-home")
	err := WriteCodexSessionConfig(codexHome, CodexConfig{
		BaseURL:  "http://127.0.0.1:43123/openai/v1",
		TokenEnv: "AE_LOCAL_PROXY_TOKEN",
		Model:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("WriteCodexSessionConfig: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if !strings.Contains(string(data), "model_provider = \"ae_local_proxy\"") {
		t.Fatalf("missing model_provider in config: %s", string(data))
	}
	if !strings.Contains(string(data), "[features]") || !strings.Contains(string(data), "codex_hooks = true") {
		t.Fatalf("missing codex_hooks feature flag in config: %s", string(data))
	}
	hooksData, err := os.ReadFile(filepath.Join(codexHome, "hooks.json"))
	if err != nil {
		t.Fatalf("ReadFile hooks.json: %v", err)
	}
	hooks := string(hooksData)
	for _, want := range []string{
		"SessionStart",
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"Stop",
		"hook session-event --tool codex",
		`"matcher": "Bash"`,
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("missing %q in hooks config: %s", want, hooks)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no workspace-local codex config, got err=%v", err)
	}
}

func TestCleanupLegacyWorkspaceCodexConfigRemovesManagedConfig(t *testing.T) {
	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	legacyManaged := `model = "gpt-5.4"
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
base_url = "http://127.0.0.1:43123/openai/v1"
env_key = "AE_LOCAL_PROXY_TOKEN"
wire_api = "responses"
supports_websockets = false
`
	if err := os.WriteFile(configPath, []byte(legacyManaged), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := CleanupLegacyWorkspaceCodexConfig(workspaceRoot); err != nil {
		t.Fatalf("CleanupLegacyWorkspaceCodexConfig: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected managed legacy config removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("expected empty .codex dir removed, got err=%v", err)
	}
}

func TestCleanupLegacyWorkspaceCodexConfigKeepsUserConfig(t *testing.T) {
	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	userConfig := `model = "gpt-5.4"
model_provider = "openai"
`
	if err := os.WriteFile(configPath, []byte(userConfig), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := CleanupLegacyWorkspaceCodexConfig(workspaceRoot); err != nil {
		t.Fatalf("CleanupLegacyWorkspaceCodexConfig: %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != userConfig {
		t.Fatalf("unexpected config content after cleanup: %q", string(got))
	}
}

func TestCleanupLegacyWorkspaceCodexConfigKeepsNearMatchConfig(t *testing.T) {
	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	nearMatchConfig := `model = "gpt-5.4"
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
env_key = "USER_LOCAL_PROXY_TOKEN"
wire_api = "responses"
`
	if err := os.WriteFile(configPath, []byte(nearMatchConfig), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := CleanupLegacyWorkspaceCodexConfig(workspaceRoot); err != nil {
		t.Fatalf("CleanupLegacyWorkspaceCodexConfig: %v", err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != nearMatchConfig {
		t.Fatalf("unexpected config content after cleanup: %q", string(got))
	}
}

func TestWriteClaudeSessionEnv(t *testing.T) {
	env := ClaudeEnv{
		BaseURL: "http://127.0.0.1:43123/anthropic",
		Token:   "proxy-token-1",
	}
	got := ApplyClaudeProxyEnv(map[string]string{
		"ANTHROPIC_API_KEY": "upstream-secret",
	}, env)
	if got["ANTHROPIC_BASE_URL"] == "" || got["ANTHROPIC_AUTH_TOKEN"] == "" {
		t.Fatalf("unexpected claude env: %+v", got)
	}
	if _, exists := got["ANTHROPIC_API_KEY"]; exists {
		t.Fatalf("expected ANTHROPIC_API_KEY to be removed in proxy mode: %+v", got)
	}
}

func TestWriteClaudeSessionConfigMergesAndCleanupRemovesOnlyManagedHooks(t *testing.T) {
	workspaceRoot := t.TempDir()
	settingsPath := filepath.Join(workspaceRoot, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	userSettings := `{
  "theme": "dark",
  "hooks": {
    "Notification": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "echo notify-user"
          }
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(settingsPath, []byte(userSettings), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := WriteClaudeSessionConfig(workspaceRoot, ClaudeHookConfig{
		SessionID: "sess-claude-1",
		SelfPath:  "/tmp/ae-cli",
	})
	if err != nil {
		t.Fatalf("WriteClaudeSessionConfig: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"Notification",
		"PreToolUse",
		"PostToolUse",
		"SessionStart",
		"UserPromptSubmit",
		"Stop",
		"hook session-event --tool claude",
		"ae-session-managed session=sess-claude-1 tool=claude",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in merged Claude settings: %s", want, content)
		}
	}
	if strings.Contains(content, `"matcher": "Bash"`) {
		t.Fatalf("expected Claude tool hooks to cover all tools, got Bash-only matcher: %s", content)
	}

	if err := CleanupClaudeSessionConfig(workspaceRoot, "sess-claude-1"); err != nil {
		t.Fatalf("CleanupClaudeSessionConfig: %v", err)
	}

	cleaned, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile after cleanup: %v", err)
	}
	if strings.Contains(string(cleaned), "hook session-event --tool claude") {
		t.Fatalf("managed Claude hook command still present after cleanup: %s", string(cleaned))
	}

	var decoded map[string]any
	if err := json.Unmarshal(cleaned, &decoded); err != nil {
		t.Fatalf("Unmarshal cleaned settings: %v", err)
	}
	if decoded["theme"] != "dark" {
		t.Fatalf("theme = %#v, want %q", decoded["theme"], "dark")
	}
	hooksMap, _ := decoded["hooks"].(map[string]any)
	if _, ok := hooksMap["Notification"]; !ok {
		t.Fatalf("expected Notification hook to survive cleanup: %+v", hooksMap)
	}
	if _, ok := hooksMap["PreToolUse"]; ok {
		t.Fatalf("expected managed PreToolUse hook to be removed: %+v", hooksMap)
	}
}

func TestCleanupClaudeSessionConfigRemovesManagedFileWhenOnlyManagedHooksExist(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := WriteClaudeSessionConfig(workspaceRoot, ClaudeHookConfig{
		SessionID: "sess-claude-2",
		SelfPath:  "/tmp/ae-cli",
	}); err != nil {
		t.Fatalf("WriteClaudeSessionConfig: %v", err)
	}

	if err := CleanupClaudeSessionConfig(workspaceRoot, "sess-claude-2"); err != nil {
		t.Fatalf("CleanupClaudeSessionConfig: %v", err)
	}

	settingsPath := filepath.Join(workspaceRoot, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("expected managed-only settings.local.json removed, got err=%v", err)
	}
}
