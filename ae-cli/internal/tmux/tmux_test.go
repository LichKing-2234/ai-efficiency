package tmux

import (
	"os"
	"testing"
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
