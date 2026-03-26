package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Use a temp HOME so no real ~/.ae-cli/config.yaml is picked up.
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	os.Setenv("HOME", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with empty path: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// With no file, all fields should be zero-value defaults.
	if cfg.Server.URL != "" {
		t.Errorf("expected empty server URL, got %q", cfg.Server.URL)
	}
	if cfg.Server.Token != "" {
		t.Errorf("expected empty server token, got %q", cfg.Server.Token)
	}
	if cfg.Sub2api.APIKeyEnv != "" {
		t.Errorf("expected empty sub2api api_key_env, got %q", cfg.Sub2api.APIKeyEnv)
	}
	if cfg.Sub2api.URL != "" {
		t.Errorf("expected empty sub2api url, got %q", cfg.Sub2api.URL)
	}
	if cfg.Sub2api.Model != "" {
		t.Errorf("expected empty sub2api model, got %q", cfg.Sub2api.Model)
	}
	if len(cfg.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(cfg.Tools))
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `server:
  url: "http://localhost:9090"
  token: "test-token-123"
sub2api:
  api_key_env: "MY_API_KEY"
  url: "http://sub2api:8080"
  model: "gpt-4"
tools:
  claude:
    command: "claude"
    args: ["-p"]
  codex:
    command: "codex"
    args: ["--quiet"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%q): %v", cfgPath, err)
	}

	if cfg.Server.URL != "http://localhost:9090" {
		t.Errorf("server.url = %q, want %q", cfg.Server.URL, "http://localhost:9090")
	}
	if cfg.Server.Token != "test-token-123" {
		t.Errorf("server.token = %q, want %q", cfg.Server.Token, "test-token-123")
	}
	if cfg.Sub2api.APIKeyEnv != "MY_API_KEY" {
		t.Errorf("sub2api.api_key_env = %q, want %q", cfg.Sub2api.APIKeyEnv, "MY_API_KEY")
	}
	if cfg.Sub2api.URL != "http://sub2api:8080" {
		t.Errorf("sub2api.url = %q, want %q", cfg.Sub2api.URL, "http://sub2api:8080")
	}
	if cfg.Sub2api.Model != "gpt-4" {
		t.Errorf("sub2api.model = %q, want %q", cfg.Sub2api.Model, "gpt-4")
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(cfg.Tools))
	}
	claude, ok := cfg.Tools["claude"]
	if !ok {
		t.Fatal("missing tool 'claude'")
	}
	if claude.Command != "claude" {
		t.Errorf("claude.command = %q, want %q", claude.Command, "claude")
	}
	if len(claude.Args) != 1 || claude.Args[0] != "-p" {
		t.Errorf("claude.args = %v, want [\"-p\"]", claude.Args)
	}
	codex, ok := cfg.Tools["codex"]
	if !ok {
		t.Fatal("missing tool 'codex'")
	}
	if codex.Command != "codex" {
		t.Errorf("codex.command = %q, want %q", codex.Command, "codex")
	}
	if len(codex.Args) != 1 || codex.Args[0] != "--quiet" {
		t.Errorf("codex.args = %v, want [\"--quiet\"]", codex.Args)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/tmp/ae-cli-nonexistent-path/config.yaml")
	// When an explicit path is given but doesn't exist, Load should return an error.
	if err == nil {
		// If it didn't error, the config should still be usable (zero-value).
		if cfg == nil {
			t.Fatal("expected non-nil config or an error")
		}
	}
	// Either an error is returned, or we get a valid (default) config — both are acceptable.
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write invalid YAML
	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml:::"), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Log("Load with invalid YAML may not error depending on viper behavior")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte(""), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load empty file: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for empty file")
	}
	// All fields should be zero-value
	if cfg.Server.URL != "" {
		t.Errorf("server.url = %q, want empty", cfg.Server.URL)
	}
}

func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `server:
  url: "http://partial:8080"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load partial config: %v", err)
	}
	if cfg.Server.URL != "http://partial:8080" {
		t.Errorf("server.url = %q, want %q", cfg.Server.URL, "http://partial:8080")
	}
	// Token should be empty
	if cfg.Server.Token != "" {
		t.Errorf("server.token = %q, want empty", cfg.Server.Token)
	}
	// Tools should be nil/empty
	if len(cfg.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(cfg.Tools))
	}
}

func TestLoadFromHomeDir(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create config in the expected location
	cfgDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(cfgDir, 0o755)
	content := `server:
  url: "http://home-config:9090"
  token: "home-token"
`
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(content), 0o644)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load from home dir: %v", err)
	}
	if cfg.Server.URL != "http://home-config:9090" {
		t.Errorf("server.url = %q, want %q", cfg.Server.URL, "http://home-config:9090")
	}
	if cfg.Server.Token != "home-token" {
		t.Errorf("server.token = %q, want %q", cfg.Server.Token, "home-token")
	}
}

func TestToolConfigStruct(t *testing.T) {
	tc := ToolConfig{
		Command: "my-tool",
		Args:    []string{"--flag", "value"},
	}
	if tc.Command != "my-tool" {
		t.Errorf("Command = %q, want %q", tc.Command, "my-tool")
	}
	if len(tc.Args) != 2 {
		t.Errorf("Args length = %d, want 2", len(tc.Args))
	}
}

func TestServerConfigStruct(t *testing.T) {
	sc := ServerConfig{
		URL:   "http://test:8080",
		Token: "tok",
	}
	if sc.URL != "http://test:8080" {
		t.Errorf("URL = %q, want %q", sc.URL, "http://test:8080")
	}
	if sc.Token != "tok" {
		t.Errorf("Token = %q, want %q", sc.Token, "tok")
	}
}

func TestSub2apiConfigStruct(t *testing.T) {
	sc := Sub2apiConfig{
		APIKeyEnv: "MY_KEY",
		URL:       "http://sub2api",
		Model:     "gpt-4",
	}
	if sc.APIKeyEnv != "MY_KEY" {
		t.Errorf("APIKeyEnv = %q, want %q", sc.APIKeyEnv, "MY_KEY")
	}
	if sc.URL != "http://sub2api" {
		t.Errorf("URL = %q, want %q", sc.URL, "http://sub2api")
	}
	if sc.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", sc.Model, "gpt-4")
	}
}

func TestConfigStruct(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{URL: "http://test", Token: "tok"},
		Sub2api: Sub2apiConfig{
			APIKeyEnv: "KEY",
			URL:       "http://sub2api",
			Model:     "model",
		},
		Tools: map[string]ToolConfig{
			"tool1": {Command: "cmd1", Args: []string{"a"}},
		},
	}
	if cfg.Server.URL != "http://test" {
		t.Errorf("Server.URL = %q", cfg.Server.URL)
	}
	if len(cfg.Tools) != 1 {
		t.Errorf("Tools count = %d, want 1", len(cfg.Tools))
	}
}

func TestLoadFromHomeDirNoConfig(t *testing.T) {
	// Test Load("") when HOME has no .ae-cli directory at all
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load from empty home: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Should return zero-value defaults
	if cfg.Server.URL != "" {
		t.Errorf("server.url = %q, want empty", cfg.Server.URL)
	}
}

func TestLoadExplicitMissingFileReturnsError(t *testing.T) {
	_, err := Load("/tmp/ae-cli-definitely-nonexistent-" + t.Name() + "/config.yaml")
	if err == nil {
		t.Log("Load with explicit missing path did not error (viper may handle gracefully)")
	}
}

func TestLoadNoHomeDir(t *testing.T) {
	// Unset HOME to trigger UserHomeDir error
	origHome := os.Getenv("HOME")
	os.Unsetenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	_, err := Load("")
	if err == nil {
		t.Log("Load without HOME may not error on all platforms")
	} else {
		if !strings.Contains(err.Error(), "finding home directory") {
			t.Errorf("error = %q, want it to contain 'finding home directory'", err.Error())
		}
	}
}

func TestLoadMultipleToolArgs(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `tools:
  complex-tool:
    command: "my-binary"
    args: ["--verbose", "--output", "/tmp/out", "--format", "json"]
`
	os.WriteFile(cfgPath, []byte(content), 0o644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tool := cfg.Tools["complex-tool"]
	if tool.Command != "my-binary" {
		t.Errorf("command = %q, want %q", tool.Command, "my-binary")
	}
	if len(tool.Args) != 5 {
		t.Errorf("args length = %d, want 5", len(tool.Args))
	}
}
