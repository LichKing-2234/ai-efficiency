package toolconfig

import (
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
