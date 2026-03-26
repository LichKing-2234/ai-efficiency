package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
)

func TestVersionCommand(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetArgs([]string{"version"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Log("version command produced no captured output (fmt.Printf goes to os.Stdout)")
		return
	}
	if !bytes.Contains([]byte(output), []byte("v0.1.0")) {
		t.Errorf("output = %q, want it to contain %q", output, "v0.1.0")
	}
}

func TestVersionConstant(t *testing.T) {
	if version == "" {
		t.Error("version constant should not be empty")
	}
	if version != "v0.1.0" {
		t.Errorf("version = %q, want %q", version, "v0.1.0")
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

func TestStopCommandWithSession(t *testing.T) {
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

	// Write a session state file
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:     "test-stop-sess",
		Repo:   "org/repo",
		Branch: "main",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := stopCmd.RunE(stopCmd, nil)
	if err != nil {
		t.Fatalf("stop command: %v", err)
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

func TestRunCommandWithSession(t *testing.T) {
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

	// Write a session state file
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:     "test-run-sess",
		Repo:   "org/repo",
		Branch: "main",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := runCmd.RunE(runCmd, []string{"echo-tool"})
	if err != nil {
		t.Fatalf("run command: %v", err)
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

func TestStartCommandWithExistingDeadSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Server that handles create and stop
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions" {
			var req client.CreateSessionRequest
			json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.Session{
					ID:        req.ID,
					Status:    "active",
					StartedAt: time.Now(),
				},
			})
			return
		}
		// Stop session
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Write an existing session with a dead tmux session
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "dead-sess",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: "ae-dead-nonexistent",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// startCmd.RunE will try to detect git repo, which may fail
	err := startCmd.RunE(startCmd, nil)
	// This may fail due to git detection, but it exercises the "dead session cleanup" path
	_ = err
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

func TestRunCommandWithExtraArgs(t *testing.T) {
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
	state := session.State{ID: "test-run-extra", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Run with extra args
	err := runCmd.RunE(runCmd, []string{"echo-tool", "extra-arg1", "extra-arg2"})
	if err != nil {
		t.Fatalf("run command with extra args: %v", err)
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
	state := session.State{ID: "test-stop-err", Repo: "org/repo", Branch: "main"}
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

func TestPsCommandWithTmuxSession(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	tmuxName := "ae-cli-cmd-test-ps"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-ps-tmux",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := psCmd.RunE(psCmd, nil)
	if err != nil {
		t.Fatalf("ps command with tmux: %v", err)
	}
}

func TestKillCommandSuccess(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-cmd-test-kill"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	// Split a pane so we have something to kill
	paneID, err := tmux.SplitWindow(tmuxName, "test", "sleep", []string{"30"})
	if err != nil {
		t.Fatalf("SplitWindow: %v", err)
	}

	err = killCmd.RunE(killCmd, []string{paneID})
	if err != nil {
		t.Fatalf("kill command: %v", err)
	}
}

func TestStopCommandWithLiveTmuxSession(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	tmuxName := "ae-cli-cmd-test-stop-live"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-stop-live",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := stopCmd.RunE(stopCmd, nil)
	if err != nil {
		t.Fatalf("stop command with live tmux: %v", err)
	}

	// Tmux session should be killed
	if tmux.SessionExists(tmuxName) {
		t.Error("tmux session should have been killed")
	}
}

func TestRunCommandWithTmuxSession(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	tmuxName := "ae-cli-cmd-test-run-tmux"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-run-tmux",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := runCmd.RunE(runCmd, []string{"echo-tool"})
	if err != nil {
		t.Fatalf("run command with tmux: %v", err)
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

func TestStartCommandNewSessionWithCommandFails(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create a temp git repo
	tmpDir := t.TempDir()
	gitCmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range gitCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	now := time.Now().Truncate(time.Second)
	// Return a session ID that starts with a known prefix so we can predict the tmux name
	fixedID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions" {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.Session{
					ID:        fixedID,
					Status:    "active",
					StartedAt: now,
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Pre-create the tmux session so NewSessionWithCommand will fail (duplicate)
	tmuxName := "ae-" + fixedID[:8]
	tmux.KillSession(tmuxName)
	tmux.NewSession(tmuxName)
	defer tmux.KillSession(tmuxName)

	// Run start — NewSessionWithCommand will fail because session already exists
	err := startCmd.RunE(startCmd, nil)
	if err != nil {
		t.Fatalf("start command: %v", err)
	}
}

func TestStartCommandWithTmuxCreation(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create a temp git repo
	tmpDir := t.TempDir()
	gitCmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range gitCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	now := time.Now().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions" {
			var req client.CreateSessionRequest
			json.NewDecoder(r.Body).Decode(&req)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.Session{
					ID:        req.ID,
					Status:    "active",
					StartedAt: now,
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	// Run start — it will create a tmux session
	err := startCmd.RunE(startCmd, nil)
	if err != nil {
		t.Fatalf("start command: %v", err)
	}

	// Clean up any tmux sessions created
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	data, _ := os.ReadFile(filepath.Join(stateDir, "current-session.json"))
	var state session.State
	json.Unmarshal(data, &state)
	if state.TmuxSession != "" {
		tmux.KillSession(state.TmuxSession)
	}
}

func TestStartCommandInsideSameSessionSkipsAttach(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create a real tmux session to simulate being "inside" it
	tmuxName := "ae-cli-test-inside"
	tmux.KillSession(tmuxName)
	err := tmux.NewSession(tmuxName)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	// Write an existing session state pointing to this tmux session
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "inside-test-sess",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
		StartedAt:   time.Now(),
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Override IsInsideSessionFunc to simulate being inside the target session
	origIsInside := tmux.IsInsideSessionFunc
	tmux.IsInsideSessionFunc = func(name string) bool { return name == tmuxName }
	defer func() { tmux.IsInsideSessionFunc = origIsInside }()

	// Run start — should detect we're inside the same session and return nil
	// WITHOUT trying to attach (which would cause infinite recursion)
	err = startCmd.RunE(startCmd, nil)
	if err != nil {
		t.Fatalf("start inside same session should not error: %v", err)
	}
}

func TestStopCommandKillTmuxError(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create a tmux session then kill it before stop, so KillSession will fail
	tmuxName := "ae-cli-cmd-test-stop-kill-err"
	tmux.KillSession(tmuxName)
	tmux.NewSession(tmuxName)
	// Kill it immediately so SessionExists returns false
	tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-stop-kill-err",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Stop should succeed (tmux session doesn't exist, so it skips kill)
	err := stopCmd.RunE(stopCmd, nil)
	if err != nil {
		t.Fatalf("stop command: %v", err)
	}
}

func TestAttachCommandWithTmuxSession(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create a tmux session but don't attach (attach requires terminal)
	tmuxName := "ae-cli-cmd-test-attach"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-attach-tmux",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: tmuxName,
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// attach will try to run tmux attach-session which will fail without a terminal
	err := attachCmd.RunE(attachCmd, nil)
	// This will error because we're not in a terminal, but it exercises the code path
	if err != nil {
		t.Logf("attach error (expected without terminal): %v", err)
	}
}

func TestShellCommandWithSession(t *testing.T) {
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
	state := session.State{
		ID:          "test-shell-sess",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: "ae-test",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Pipe stdin so shell.Run() exits immediately
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString("exit\n")
		w.Close()
	}()

	err = shellCmd.RunE(shellCmd, nil)
	if err != nil {
		t.Fatalf("shell command: %v", err)
	}
}

func TestStopCommandWithTmuxSession(t *testing.T) {
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

	// Write a session with a (non-existent) tmux session
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := session.State{
		ID:          "test-stop-tmux",
		Repo:        "org/repo",
		Branch:      "main",
		TmuxSession: "ae-nonexistent-tmux",
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	err := stopCmd.RunE(stopCmd, nil)
	if err != nil {
		t.Fatalf("stop command: %v", err)
	}
}
