package tmux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHasTmux(t *testing.T) {
	// This just verifies the function doesn't panic.
	// Result depends on whether tmux is installed.
	result := HasTmux()
	t.Logf("HasTmux() = %v", result)
}

func TestSessionExistsNonExistent(t *testing.T) {
	// A session with a random name should not exist
	if SessionExists("ae-cli-test-nonexistent-session-12345") {
		t.Error("expected non-existent session to return false")
	}
}

func TestKillPaneNonExistent(t *testing.T) {
	// Killing a non-existent pane should return an error
	err := KillPane("%999999")
	if err == nil {
		t.Log("KillPane on non-existent pane returned nil (tmux may not be installed)")
	}
}

func TestKillSessionNonExistent(t *testing.T) {
	err := KillSession("ae-cli-test-nonexistent-session-12345")
	if err == nil {
		t.Log("KillSession on non-existent session returned nil (tmux may not be installed)")
	}
}

func TestListPanesNonExistent(t *testing.T) {
	_, err := ListPanes("ae-cli-test-nonexistent-session-12345")
	if err == nil {
		t.Log("ListPanes on non-existent session returned nil (tmux may not be installed)")
	}
}

func TestSplitWindowNonExistent(t *testing.T) {
	_, err := SplitWindow("ae-cli-test-nonexistent-session-12345", "test-tool", "echo", []string{"hello"})
	if err == nil {
		t.Log("SplitWindow on non-existent session returned nil (tmux may not be installed)")
	}
}

func TestNewSessionAndCleanup(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-session"
	// Clean up in case a previous test left it
	KillSession(name)

	err := NewSession(name)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer KillSession(name)

	if !SessionExists(name) {
		t.Error("session should exist after creation")
	}

	// Test ListPanes
	panes, err := ListPanes(name)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) == 0 {
		t.Error("expected at least one pane")
	}

	// Test SplitWindow
	paneID, err := SplitWindow(name, "echo-tool", "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("SplitWindow: %v", err)
	}
	if paneID == "" {
		t.Error("expected non-empty pane ID")
	}

	// Test KillPane — pane may have already exited (echo is fast), so don't fail on error
	err = KillPane(paneID)
	if err != nil {
		t.Logf("KillPane: %v (pane may have already exited)", err)
	}

	// Test KillSession
	err = KillSession(name)
	if err != nil {
		t.Errorf("KillSession: %v", err)
	}
	if SessionExists(name) {
		t.Error("session should not exist after kill")
	}
}

func TestNewSessionIncludesTmuxOutputOnFailure(t *testing.T) {
	origRun := tmuxRun
	tmuxRun = func(args ...string) ([]byte, error) {
		want := []string{"new-session", "-d", "-s", "broken-session", "-x", "200", "-y", "50"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("args = %v, want %v", args, want)
		}
		return []byte("duplicate session: broken-session"), errors.New("exit status 1")
	}
	t.Cleanup(func() { tmuxRun = origRun })

	err := NewSession("broken-session")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("error = %q, want exit status", err)
	}
	if !strings.Contains(err.Error(), "duplicate session: broken-session") {
		t.Fatalf("error = %q, want tmux output", err)
	}
}

func TestNewSessionWithCommand(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-cmd-session"
	KillSession(name)

	err := NewSessionWithCommand(name, "sleep 60")
	if err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	defer KillSession(name)

	if !SessionExists(name) {
		t.Error("session should exist after creation")
	}
}

func TestNewSessionWithCommandAndListPanes(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-cmd-panes"
	KillSession(name)

	err := NewSessionWithCommand(name, "sleep 60")
	if err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	defer KillSession(name)

	panes, err := ListPanes(name)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) == 0 {
		t.Error("expected at least one pane")
	}
}

func TestNewSessionDuplicate(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-dup-session"
	KillSession(name)

	err := NewSession(name)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer KillSession(name)

	// Creating a session with the same name should fail
	err = NewSession(name)
	if err == nil {
		t.Error("expected error when creating duplicate session")
	}
}

func TestNewSessionWithCommandDuplicate(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-dup-cmd-session"
	KillSession(name)

	err := NewSessionWithCommand(name, "sleep 60")
	if err != nil {
		t.Fatalf("NewSessionWithCommand: %v", err)
	}
	defer KillSession(name)

	err = NewSessionWithCommand(name, "sleep 60")
	if err == nil {
		t.Error("expected error when creating duplicate session")
	}
}

func TestSplitWindowAndListPanes(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := "ae-cli-unit-test-split-list"
	KillSession(name)

	err := NewSession(name)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer KillSession(name)

	// Use sleep so the pane stays alive long enough to be listed
	paneID, err := SplitWindow(name, "test-tool", "sleep", []string{"10"})
	if err != nil {
		t.Fatalf("SplitWindow: %v", err)
	}
	if paneID == "" {
		t.Error("expected non-empty pane ID")
	}

	panes, err := ListPanes(name)
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	// Should have at least 2 panes (initial + split)
	if len(panes) < 2 {
		t.Errorf("expected at least 2 panes, got %d", len(panes))
	}
}

func TestSplitWindowWithEnvRemovesAnthropicAPIKeyInPane(t *testing.T) {
	if !HasTmux() {
		t.Skip("tmux not installed")
	}

	name := fmt.Sprintf("ae-cli-unit-test-split-env-%d", time.Now().UnixNano())
	KillSession(name)
	if err := NewSession(name); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer KillSession(name)

	// Seed stale session-level auth to prove pane launch sanitization works.
	if err := exec.Command("tmux", "set-environment", "-t", name, "ANTHROPIC_API_KEY", "stale-upstream-key").Run(); err != nil {
		t.Fatalf("seed stale tmux env: %v", err)
	}

	cmd := "env | sort; sleep 2"
	paneID, err := SplitWindowWithEnv(name, "env-check", "sh", []string{"-lc", cmd}, map[string]string{
		"ANTHROPIC_BASE_URL":   "http://127.0.0.1:43123/anthropic",
		"ANTHROPIC_AUTH_TOKEN": "proxy-token",
	}, []string{"ANTHROPIC_API_KEY"})
	if err != nil {
		t.Fatalf("SplitWindowWithEnv: %v", err)
	}

	content, readErr := waitForPaneContent(paneID, func(content []byte) bool {
		got := string(content)
		return strings.Contains(got, "ANTHROPIC_AUTH_TOKEN=proxy-token")
	})
	if readErr != nil {
		t.Fatalf("read pane env output: %v", readErr)
	}
	got := string(content)
	if strings.Contains(got, "ANTHROPIC_API_KEY=") {
		t.Fatalf("expected pane env to remove ANTHROPIC_API_KEY, got:\n%s", got)
	}
	if !strings.Contains(got, "ANTHROPIC_AUTH_TOKEN=proxy-token") {
		t.Fatalf("expected pane env to include proxy auth token, got:\n%s", got)
	}
}

func waitForPaneContent(paneID string, matcher func([]byte) bool) ([]byte, error) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-", "-E", "-", "-t", paneID).Output()
		if err == nil && matcher(out) {
			return out, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for matching pane content: %s", paneID)
}

func waitForFileContent(path string, matcher func([]byte) bool) ([]byte, error) {
	var content []byte
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, err = os.ReadFile(path)
		if err == nil && matcher(content) {
			return content, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	return content, fmt.Errorf("timed out waiting for matching file content: %s", path)
}

func TestWaitForFileContentRetriesUntilMatcherPasses(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "pane-env.txt")

	go func() {
		if err := os.WriteFile(outPath, []byte(""), 0o600); err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(outPath, []byte("ANTHROPIC_AUTH_TOKEN=proxy-token\n"), 0o600)
	}()

	content, err := waitForFileContent(outPath, func(content []byte) bool {
		return strings.Contains(string(content), "ANTHROPIC_AUTH_TOKEN=proxy-token")
	})
	if err != nil {
		t.Fatalf("waitForFileContent: %v", err)
	}
	if got := string(content); !strings.Contains(got, "ANTHROPIC_AUTH_TOKEN=proxy-token") {
		t.Fatalf("expected proxy token in file content, got %q", got)
	}
}

func TestIsInsideSession(t *testing.T) {
	// When not inside any tmux session ($TMUX is empty),
	// IsInsideSession should return false for any session name.
	origTmux := os.Getenv("TMUX")
	os.Unsetenv("TMUX")
	defer func() {
		if origTmux != "" {
			os.Setenv("TMUX", origTmux)
		} else {
			os.Unsetenv("TMUX")
		}
	}()

	if IsInsideSession("ae-anything") {
		t.Error("expected false when not inside any tmux session")
	}
}

func TestIsInsideSessionFuncOverride(t *testing.T) {
	// Override IsInsideSessionFunc to simulate being inside a session
	orig := IsInsideSessionFunc
	defer func() { IsInsideSessionFunc = orig }()

	IsInsideSessionFunc = func(name string) bool { return name == "ae-target" }

	if !IsInsideSession("ae-target") {
		t.Error("expected true when IsInsideSessionFunc returns true for matching name")
	}
	if IsInsideSession("ae-other") {
		t.Error("expected false for non-matching session name")
	}
}

func TestPaneStruct(t *testing.T) {
	p := Pane{
		ID:     "%1",
		Tool:   "claude",
		Active: true,
	}
	if p.ID != "%1" {
		t.Errorf("ID = %q, want %%1", p.ID)
	}
	if p.Tool != "claude" {
		t.Errorf("Tool = %q, want claude", p.Tool)
	}
	if !p.Active {
		t.Error("Active should be true")
	}
}
