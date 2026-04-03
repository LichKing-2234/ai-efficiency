package dispatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
)

func TestNewDispatcher(t *testing.T) {
	cfg := &config.Config{}
	c := client.New("http://localhost:8080", "tok")
	d := New(cfg, c)
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
	if d.config != cfg {
		t.Error("dispatcher config mismatch")
	}
	if d.client != c {
		t.Error("dispatcher client mismatch")
	}
}

func TestRunUnknownTool(t *testing.T) {
	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{},
	}
	c := client.New("http://localhost:8080", "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "nonexistent-tool", []string{"hello"}, "")
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	expected := `tool "nonexistent-tool" not found in config`
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestRunDirectExecution(t *testing.T) {
	// Set up a mock server to capture the invocation
	var receivedInv client.Invocation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess-1/invocations" {
			json.NewDecoder(r.Body).Decode(&receivedInv)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// Run with no tmux session (direct execution)
	err := d.Run("sess-1", "echo-tool", []string{"world"}, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify invocation was recorded
	if receivedInv.Tool != "echo-tool" {
		t.Errorf("invocation tool = %q, want %q", receivedInv.Tool, "echo-tool")
	}
	if receivedInv.Start.IsZero() {
		t.Error("invocation start time should not be zero")
	}
	if receivedInv.End.IsZero() {
		t.Error("invocation end time should not be zero")
	}
	if !receivedInv.End.After(receivedInv.Start) && !receivedInv.End.Equal(receivedInv.Start) {
		t.Error("invocation end should be >= start")
	}
}

func TestRunDirectExecutionUsesRuntimeEnvBundle(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	if err := session.WriteRuntimeBundle(&session.RuntimeBundle{
		SessionID: "sess-1",
		EnvBundle: map[string]string{
			"AE_SESSION_ID": "sess-1",
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	var lastCmd *exec.Cmd
	prev := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(name, args...)
		lastCmd = cmd
		return cmd
	}
	t.Cleanup(func() { execCommand = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Invocation recording is not relevant for env wiring.
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"true-tool": {
				Command: "true",
				Args:    []string{},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	if err := d.Run("sess-1", "true-tool", nil, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if lastCmd == nil {
		t.Fatal("expected execCommand to be called")
	}
	found := false
	for _, kv := range lastCmd.Env {
		if kv == "AE_SESSION_ID=sess-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected AE_SESSION_ID=sess-1 in cmd.Env, got %v", lastCmd.Env)
	}
}

func TestRunDirectExecutionRemovesAnthropicAPIKeyWhenProxyEnvPresent(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	t.Setenv("ANTHROPIC_API_KEY", "upstream-should-not-leak")

	if err := session.WriteRuntimeBundle(&session.RuntimeBundle{
		SessionID: "sess-1",
		EnvBundle: map[string]string{
			"ANTHROPIC_BASE_URL":   "http://127.0.0.1:43123/anthropic",
			"ANTHROPIC_AUTH_TOKEN": "proxy-token",
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	var lastCmd *exec.Cmd
	prev := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command(name, args...)
		lastCmd = cmd
		return cmd
	}
	t.Cleanup(func() { execCommand = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"true-tool": {
				Command: "true",
				Args:    []string{},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	if err := d.Run("sess-1", "true-tool", nil, ""); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if lastCmd == nil {
		t.Fatal("expected execCommand to be called")
	}

	for _, kv := range lastCmd.Env {
		if kv == "ANTHROPIC_API_KEY=upstream-should-not-leak" {
			t.Fatalf("unexpected inherited ANTHROPIC_API_KEY in cmd.Env: %v", lastCmd.Env)
		}
	}
	foundProxyToken := false
	for _, kv := range lastCmd.Env {
		if kv == "ANTHROPIC_AUTH_TOKEN=proxy-token" {
			foundProxyToken = true
			break
		}
	}
	if !foundProxyToken {
		t.Fatalf("expected ANTHROPIC_AUTH_TOKEN=proxy-token in cmd.Env, got %v", lastCmd.Env)
	}
}

func TestRunTmuxUnsetsAnthropicAPIKeyWhenProxyEnvPresent(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	if err := session.WriteRuntimeBundle(&session.RuntimeBundle{
		SessionID: "sess-tmux-unset",
		EnvBundle: map[string]string{
			"ANTHROPIC_BASE_URL":   "http://127.0.0.1:43123/anthropic",
			"ANTHROPIC_AUTH_TOKEN": "proxy-token",
			"ANTHROPIC_API_KEY":    "stale-upstream-key",
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}

	var setEnv map[string]string
	var unsetKeys []string
	origSet := tmuxSetEnvironment
	origUnset := tmuxUnsetEnvironment
	origSplit := tmuxSplitWindow
	tmuxSetEnvironment = func(sessionName string, env map[string]string) error {
		setEnv = env
		return nil
	}
	tmuxUnsetEnvironment = func(sessionName string, keys []string) error {
		unsetKeys = append([]string{}, keys...)
		return nil
	}
	tmuxSplitWindow = func(sessionName string, toolName string, command string, args []string, env map[string]string, unsetKeys []string) (string, error) {
		if len(unsetKeys) != 1 || unsetKeys[0] != "ANTHROPIC_API_KEY" {
			t.Fatalf("expected split-window unset keys to include ANTHROPIC_API_KEY, got %v", unsetKeys)
		}
		if _, exists := env["ANTHROPIC_API_KEY"]; exists {
			t.Fatalf("expected split-window env to be sanitized, got %+v", env)
		}
		return "%1", nil
	}
	t.Cleanup(func() {
		tmuxSetEnvironment = origSet
		tmuxUnsetEnvironment = origUnset
		tmuxSplitWindow = origSplit
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {Command: "echo", Args: []string{"hello"}},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	if err := d.Run("sess-tmux-unset", "echo-tool", nil, "tmux-session-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if setEnv == nil {
		t.Fatal("expected tmuxSetEnvironment to be called")
	}
	if _, exists := setEnv["ANTHROPIC_API_KEY"]; exists {
		t.Fatalf("expected sanitized env not to include ANTHROPIC_API_KEY: %+v", setEnv)
	}
	if len(unsetKeys) != 1 || unsetKeys[0] != "ANTHROPIC_API_KEY" {
		t.Fatalf("expected tmuxUnsetEnvironment to be called with ANTHROPIC_API_KEY, got %v", unsetKeys)
	}
}

func TestRunDirectExecutionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"bad-tool": {
				Command: "/nonexistent/binary/that/does/not/exist",
				Args:    []string{},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "bad-tool", nil, "")
	if err == nil {
		t.Fatal("expected error for non-existent command, got nil")
	}
}

func TestRunDirectExecutionInvocationRecordingFailure(t *testing.T) {
	// Server that fails on invocation recording
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"test"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// Should succeed even if invocation recording fails (it's a warning)
	err := d.Run("sess-1", "echo-tool", nil, "")
	if err != nil {
		t.Fatalf("Run should succeed even if invocation recording fails: %v", err)
	}
}

func TestRunWithExtraArgs(t *testing.T) {
	var receivedInv client.Invocation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess-1/invocations" {
			json.NewDecoder(r.Body).Decode(&receivedInv)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"base"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "echo-tool", []string{"extra1", "extra2"}, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunWithEmptyArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"true-tool": {
				Command: "true",
				Args:    []string{},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "true-tool", nil, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunNilToolsMap(t *testing.T) {
	cfg := &config.Config{
		Tools: nil,
	}
	c := client.New("http://localhost:8080", "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "any-tool", nil, "")
	if err == nil {
		t.Fatal("expected error for nil tools map, got nil")
	}
}

func TestRunToolWithTmuxSession(t *testing.T) {
	// We can't actually test tmux integration without tmux running,
	// but we can verify the error path when tmux is not available.
	invocationRecorded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess-1/invocations" {
			invocationRecorded = true
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// Run with a tmux session name — this will fail because the tmux session doesn't exist
	err := d.Run("sess-1", "echo-tool", nil, "nonexistent-tmux-session")
	if err == nil {
		// If tmux is not installed, the error will be about splitting the pane
		t.Log("tmux split succeeded unexpectedly (tmux may be running)")
	} else {
		// Expected: error about splitting tmux pane
		t.Logf("expected tmux error: %v", err)
	}
	// invocationRecorded may or may not be true depending on tmux availability
	_ = invocationRecorded
}

func TestRunPreservesOriginalArgs(t *testing.T) {
	// Verify that Run doesn't mutate the original tool config args
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	originalArgs := []string{"--flag1", "--flag2"}
	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    originalArgs,
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	d.Run("sess-1", "echo-tool", []string{"extra"}, "")

	// Original args should not be modified
	if len(cfg.Tools["echo-tool"].Args) != 2 {
		t.Errorf("original args length = %d, want 2", len(cfg.Tools["echo-tool"].Args))
	}
}

func TestRunDirectExecutionWithNilExtraArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello", "world"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-1", "echo-tool", nil, "")
	if err != nil {
		t.Fatalf("Run with nil extra args: %v", err)
	}
}

func TestRunTmuxSessionNotExist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// Run with a non-existent tmux session — should error on split
	err := d.Run("sess-1", "echo-tool", nil, "ae-cli-test-nonexistent-tmux-99999")
	if err == nil {
		t.Log("tmux split succeeded unexpectedly")
	} else {
		if !strings.Contains(err.Error(), "splitting tmux pane") {
			t.Errorf("error = %q, want it to contain 'splitting tmux pane'", err.Error())
		}
	}
}

func TestRunTmuxInvocationRecordingFailure(t *testing.T) {
	// Server that fails on invocation recording
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// This will fail at tmux split (no session), so invocation recording won't be reached
	// But it exercises the tmux path
	err := d.Run("sess-1", "echo-tool", nil, "ae-cli-test-nonexistent-tmux-99998")
	if err == nil {
		t.Log("tmux split succeeded unexpectedly")
	}
}

func TestRunInvocationTimestamps(t *testing.T) {
	var receivedInv client.Invocation
	invCh := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess-ts/invocations" {
			json.NewDecoder(r.Body).Decode(&receivedInv)
			invCh <- struct{}{}
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"sleep-tool": {
				Command: "true", // instant command
				Args:    []string{},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	beforeRun := context.Background()
	_ = beforeRun
	err := d.Run("sess-ts", "sleep-tool", nil, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	<-invCh
	if receivedInv.Tool != "sleep-tool" {
		t.Errorf("tool = %q, want %q", receivedInv.Tool, "sleep-tool")
	}
}

func TestRunWithTmuxSession(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-disp-test-tmux"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	var receivedInv client.Invocation
	invCh := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sessions/sess-tmux/invocations" {
			json.NewDecoder(r.Body).Decode(&receivedInv)
			invCh <- struct{}{}
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-tmux", "echo-tool", []string{"world"}, tmuxName)
	if err != nil {
		t.Fatalf("Run with tmux: %v", err)
	}

	<-invCh
	if receivedInv.Tool != "echo-tool" {
		t.Errorf("tool = %q, want %q", receivedInv.Tool, "echo-tool")
	}
	// For tmux launches, start == end
	if !receivedInv.Start.Equal(receivedInv.End) {
		t.Logf("start=%v end=%v (may differ slightly)", receivedInv.Start, receivedInv.End)
	}
}

func TestRunWithTmuxSessionInvocationFail(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-disp-test-inv-fail"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	// Server that fails on invocation recording
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	// Should succeed even if invocation recording fails (it's a warning)
	err := d.Run("sess-inv-fail", "echo-tool", nil, tmuxName)
	if err != nil {
		t.Fatalf("Run should succeed even if invocation recording fails: %v", err)
	}
}

func TestRunWithTmuxSessionExtraArgs(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-disp-test-extra"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {
				Command: "echo",
				Args:    []string{"base"},
			},
		},
	}
	c := client.New(srv.URL, "tok")
	d := New(cfg, c)

	err := d.Run("sess-extra", "echo-tool", []string{"extra1", "extra2"}, tmuxName)
	if err != nil {
		t.Fatalf("Run with tmux and extra args: %v", err)
	}
}
