package toolconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCodexSessionConfig(t *testing.T) {
	dir := t.TempDir()
	err := WriteCodexSessionConfig(dir, CodexConfig{
		BaseURL:  "http://127.0.0.1:43123/openai/v1",
		TokenEnv: "AE_LOCAL_PROXY_TOKEN",
		Model:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("WriteCodexSessionConfig: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if !strings.Contains(string(data), "model_provider = \"ae_local_proxy\"") {
		t.Fatalf("missing model_provider in config: %s", string(data))
	}
}

func TestWriteClaudeSessionEnv(t *testing.T) {
	env := ClaudeEnv{
		BaseURL: "http://127.0.0.1:43123/anthropic",
		Token:   "proxy-token-1",
	}
	got := BuildClaudeEnv(env)
	if got["ANTHROPIC_BASE_URL"] == "" || got["ANTHROPIC_AUTH_TOKEN"] == "" {
		t.Fatalf("unexpected claude env: %+v", got)
	}
}
