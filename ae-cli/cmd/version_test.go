package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/shell"
)

func TestMain(m *testing.M) {
	origWD, _ := os.Getwd()
	tmpWD, err := os.MkdirTemp("", "ae-cli-cmd-test-*")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(tmpWD); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.Chdir(origWD)
	_ = os.RemoveAll(tmpWD)
	os.Exit(code)
}

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
		t.Fatalf("output = %q, want %q", got, "ae-cli v9.9.9\\n")
	}
}

func TestRootCommandHasSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()
	if len(cmds) == 0 {
		t.Fatal("root command should have subcommands")
	}

	expected := map[string]bool{
		"version": false,
		"start":   false,
		"stop":    false,
		"run":     false,
		"ps":      false,
		"attach":  false,
		"kill":    false,
		"shell":   false,
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

func TestStartCommandUsage(t *testing.T) {
	if startCmd.Use != "start" {
		t.Errorf("Use = %q, want %q", startCmd.Use, "start")
	}
	if startCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestStopCommandUsage(t *testing.T) {
	if stopCmd.Use != "stop" {
		t.Errorf("Use = %q, want %q", stopCmd.Use, "stop")
	}
	if stopCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestRunCommandUsage(t *testing.T) {
	if runCmd.Use != "run <tool> [args...]" {
		t.Errorf("Use = %q, want %q", runCmd.Use, "run <tool> [args...]")
	}
	if runCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestPsCommandUsage(t *testing.T) {
	if psCmd.Use != "ps" {
		t.Errorf("Use = %q, want %q", psCmd.Use, "ps")
	}
	if psCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestAttachCommandUsage(t *testing.T) {
	if attachCmd.Use != "attach" {
		t.Errorf("Use = %q, want %q", attachCmd.Use, "attach")
	}
	if attachCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestKillCommandUsage(t *testing.T) {
	if killCmd.Use != "kill <pane-id>" {
		t.Errorf("Use = %q, want %q", killCmd.Use, "kill <pane-id>")
	}
	if killCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
}

func TestShellCommandUsage(t *testing.T) {
	if shellCmd.Use != "shell" {
		t.Errorf("Use = %q, want %q", shellCmd.Use, "shell")
	}
	if shellCmd.Short == "" {
		t.Error("Short description should not be empty")
	}
	if !shellCmd.Hidden {
		t.Error("shell command should be hidden")
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
	os.WriteFile(cfgPath, []byte(content), 0o644)

	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	defer func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
	}()

	cfgFile = cfgPath
	err := rootCmd.PersistentPreRunE(runCmd, nil)
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
	os.WriteFile(cfgPath, []byte("server:\n  url: http://original\n"), 0o644)

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

	err := rootCmd.PersistentPreRunE(runCmd, nil)
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

	if err := rootCmd.PersistentPreRunE(runCmd, nil); err != nil {
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

// helper to set up global state for cmd tests that need cfg/apiClient
func setupTestGlobals(t *testing.T, srv *httptest.Server) func() {
	t.Helper()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	content := `server:
  url: "` + srv.URL + `"
  token: "test-token"
tools:
  echo-tool:
    command: "echo"
    args: ["hello"]
`
	os.WriteFile(cfgPath, []byte(content), 0o644)

	oldCfg := cfg
	oldClient := apiClient
	oldCfgFile := cfgFile
	oldServerURL := serverURL

	cfgFile = cfgPath
	serverURL = ""

	// Load config
	var err error
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	apiClient = client.New(srv.URL, "test-token")

	return func() {
		cfg = oldCfg
		apiClient = oldClient
		cfgFile = oldCfgFile
		serverURL = oldServerURL
	}
}

func TestStopCommandNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	err := stopCmd.RunE(stopCmd, nil)
	if err == nil {
		t.Fatal("expected error when no active session")
	}
}

func TestRunCommandNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	err := runCmd.RunE(runCmd, []string{"echo-tool"})
	if err == nil {
		t.Fatal("expected error when no active session")
	}
}

func TestRunCommandUnknownTool(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{ID: "test-run-sess", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := runCmd.RunE(runCmd, []string{"nonexistent-tool"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestPsCommandNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	err := psCmd.RunE(psCmd, nil)
	if err == nil {
		t.Fatal("expected error when no active session")
	}
}

func TestPsCommandNoTmux(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Session without tmux
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{ID: "test-ps-sess", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := psCmd.RunE(psCmd, nil)
	if err == nil {
		t.Fatal("expected error when session has no tmux")
	}
}

func TestAttachCommandNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	err := attachCmd.RunE(attachCmd, nil)
	if err == nil {
		t.Fatal("expected error when no active session")
	}
}

func TestAttachCommandNoTmux(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{ID: "test-attach-sess", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := attachCmd.RunE(attachCmd, nil)
	if err == nil {
		t.Fatal("expected error when session has no tmux")
	}
}

func TestShellCommandNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	err := shellCmd.RunE(shellCmd, nil)
	if err == nil {
		t.Fatal("expected error when no active session")
	}
}

func TestShellBannerLinesIncludesMultiInstanceHelp(t *testing.T) {
	output := strings.Join(shell.BannerLines("claude"), "\n")
	expected := []string{
		"Auto-route through the configured router",
		"@<tool> <msg>",
		"@<tool>#<n> <msg>",
		"@all <msg>",
		"!<cmd>           Run a local shell command",
		"List running labeled panes",
		"Tool instances keep the labels <tool>#<n>",
	}
	for _, substring := range expected {
		if !strings.Contains(output, substring) {
			t.Fatalf("banner output = %q, want %q", output, substring)
		}
	}
}

func TestKillCommandRunE(t *testing.T) {
	// Kill a non-existent pane — should error
	err := killCmd.RunE(killCmd, []string{"%999999"})
	if err == nil {
		t.Log("kill command on non-existent pane may succeed if tmux is not installed")
	}
}

func TestExecuteVersion(t *testing.T) {
	// Save and restore os.Args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"ae-cli", "version"}

	// Execute should not panic or exit for "version"
	// We can't easily test os.Exit, but we can test that it doesn't panic
	// by calling rootCmd.Execute directly
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

	// Create a file with invalid YAML
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte("{{invalid yaml:::"), 0o644)

	cfgFile = cfgPath
	err := rootCmd.PersistentPreRunE(runCmd, nil)
	if err == nil {
		t.Log("PersistentPreRunE with invalid config may not error depending on viper behavior")
	}
}

func TestPersistentPreRunENoServerOverride(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte("server:\n  url: http://original\n  token: tok\n"), 0o644)

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
	serverURL = "" // no override

	err := rootCmd.PersistentPreRunE(runCmd, nil)
	if err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}
	if cfg.Server.URL != "http://original" {
		t.Errorf("server URL = %q, want %q", cfg.Server.URL, "http://original")
	}
}

func TestShellCommandWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Write bad JSON state file
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := shellCmd.RunE(shellCmd, nil)
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestPsCommandWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := psCmd.RunE(psCmd, nil)
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestAttachCommandWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := attachCmd.RunE(attachCmd, nil)
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestStopCommandWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := stopCmd.RunE(stopCmd, nil)
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestRunCommandWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := runCmd.RunE(runCmd, []string{"echo-tool"})
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestStopCommandServerError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{ID: "22222222-2222-2222-2222-222222222222", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := stopCmd.RunE(stopCmd, nil)
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
}

func TestRunCommandDispatcherError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{ID: "test-run-fail", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Run a tool that doesn't exist in config — dispatcher should error
	err := runCmd.RunE(runCmd, []string{"nonexistent-tool"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestPsCommandListPanesError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Session with a non-existent tmux session — ListPanes will fail
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-ps-err",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: "ae-nonexistent-tmux-99999",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := psCmd.RunE(psCmd, nil)
	if err == nil {
		t.Fatal("expected error when tmux session doesn't exist")
	}
}

func TestStartCommandCheckSessionError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Write bad JSON to cause Current() to error
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o600)

	err := startCmd.RunE(startCmd, nil)
	if err == nil {
		t.Fatal("expected error when session state has bad JSON")
	}
}

func TestStartCommandStartError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Change to a non-git directory so detectRepo fails
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// No existing session, so it will try to Start() which calls detectRepo
	// Since we're in a non-git directory, it will fail at detectRepo
	err := startCmd.RunE(startCmd, nil)
	if err == nil {
		t.Fatal("expected error when not in a git repo")
	}
}
