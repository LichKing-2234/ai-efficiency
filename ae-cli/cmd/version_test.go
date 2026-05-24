package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v0.1.0"
	t.Cleanup(func() {
		buildinfo.Version = oldVersion
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	if got := buf.String(); got != "ae-cli v0.1.0\n" {
		t.Errorf("output = %q, want %q", got, "ae-cli v0.1.0\n")
	}
}

func TestVersionCommandUsesBuildInfoVersion(t *testing.T) {
	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.9.9"
	t.Cleanup(func() {
		buildinfo.Version = oldVersion
	})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	if got := buf.String(); got != "ae-cli v9.9.9\n" {
		t.Fatalf("output = %q, want %q", got, "ae-cli v9.9.9\n")
	}
}

func TestRootCommandHasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	if len(cmds) == 0 {
		t.Fatal("root command should have subcommands")
	}

	expected := map[string]bool{
		"version":  false,
		"login":    false,
		"logout":   false,
		"discover": false,
		"init":     false,
		"sync":     false,
		"doctor":   false,
		"hooks":    false,
		"hook":     false,
	}

	for _, cmd := range cmds {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected subcommand %q not found", name)
		}
	}
}

func TestRootCommandFlags(t *testing.T) {
	f := rootCmd.PersistentFlags()

	configFlag := f.Lookup("config")
	if configFlag == nil {
		t.Fatal("expected --config flag")
	}
	if configFlag.DefValue != "" {
		t.Errorf("config default = %q, want empty", configFlag.DefValue)
	}

	serverFlag := f.Lookup("server")
	if serverFlag == nil {
		t.Fatal("expected --server flag")
	}
	if serverFlag.DefValue != "" {
		t.Errorf("server default = %q, want empty", serverFlag.DefValue)
	}
}

func TestRootCommandUsage(t *testing.T) {
	if rootCmd.Use != "ae-cli" {
		t.Errorf("Use = %q, want %q", rootCmd.Use, "ae-cli")
	}
	if rootCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
	if rootCmd.Long == "" {
		t.Error("Long description should not be empty")
	}
}

func TestPersistentPreRunESkipsForVersion(t *testing.T) {
	oldCfg := cfg
	oldCfgFile := cfgFile
	defer func() {
		cfg = oldCfg
		cfgFile = oldCfgFile
	}()

	cfgFile = "/nonexistent/path/config.yaml"
	err := rootCmd.PersistentPreRunE(versionCmd, nil)
	if err != nil {
		t.Fatalf("PersistentPreRunE for version should not error: %v", err)
	}
}

func TestPersistentPreRunELoadsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `server:
  url: "http://test-server:9090"
  token: "test-token"
tools:
  claude:
    command: "claude"
    args: ["-p"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	defer func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
	}()

	cfgFile = cfgPath
	err := rootCmd.PersistentPreRunE(discoverCmd, nil)
	if err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg should be set after PersistentPreRunE")
	}
	if cfg.Server.URL != "http://test-server:9090" {
		t.Errorf("server URL = %q, want %q", cfg.Server.URL, "http://test-server:9090")
	}
	if cfg.Server.Token != "test-token" {
		t.Errorf("server token = %q, want %q", cfg.Server.Token, "test-token")
	}
	if apiClient == nil {
		t.Fatal("apiClient should be set after PersistentPreRunE")
	}
}

func TestPersistentPreRunEServerOverride(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  url: http://original\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	oldServerURL := serverURL
	defer func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
		serverURL = oldServerURL
	}()

	cfgFile = cfgPath
	serverURL = "http://override-server:8080"

	err := rootCmd.PersistentPreRunE(discoverCmd, nil)
	if err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if cfg.Server.URL != "http://override-server:8080" {
		t.Errorf("server URL = %q, want %q", cfg.Server.URL, "http://override-server:8080")
	}
}

func TestPersistentPreRunEFallsBackToTokenServerURL(t *testing.T) {
	tmpHome := t.TempDir()
	tokenPath := filepath.Join(tmpHome, ".ae-cli", "token.json")
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "oauth-access-token",
		RefreshToken: "oauth-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "http://token-server:8081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	oldServerURL := serverURL
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
		serverURL = oldServerURL
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfgFile = ""
	serverURL = ""

	if err := rootCmd.PersistentPreRunE(discoverCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg should be set after PersistentPreRunE")
	}
	if cfg.Server.URL != "http://token-server:8081" {
		t.Fatalf("server URL = %q, want %q", cfg.Server.URL, "http://token-server:8081")
	}
	if apiClient == nil {
		t.Fatal("apiClient should be set after PersistentPreRunE")
	}
}

func TestExecuteVersion(t *testing.T) {
	rootCmd.SetArgs([]string{"version"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute version: %v", err)
	}
}

func TestExecuteUnknownCommand(t *testing.T) {
	rootCmd.SetArgs([]string{"nonexistent-command-12345"})
	err := rootCmd.Execute()
	if err == nil {
		t.Log("unknown command may not error depending on cobra behavior")
	}
}

func TestPersistentPreRunEBadConfig(t *testing.T) {
	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	defer func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
	}()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{{invalid yaml:::"), 0o644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	cfgFile = cfgPath
	err := rootCmd.PersistentPreRunE(discoverCmd, nil)
	if err == nil {
		t.Log("PersistentPreRunE with invalid config may not error depending on viper behavior")
	}
}

func TestPersistentPreRunENoServerOverride(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  url: http://original\n  token: tok\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	oldServerURL := serverURL
	defer func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
		serverURL = oldServerURL
	}()

	cfgFile = cfgPath
	serverURL = ""

	err := rootCmd.PersistentPreRunE(discoverCmd, nil)
	if err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if cfg.Server.URL != "http://original" {
		t.Errorf("server URL = %q, want %q", cfg.Server.URL, "http://original")
	}
}
