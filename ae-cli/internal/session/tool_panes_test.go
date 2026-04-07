package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisterToolPaneAssignsMonotonicInstanceNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := RegisterToolPane("sess-1", "claude", "%11", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane first: %v", err)
	}
	second, err := RegisterToolPane("sess-1", "claude", "%12", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane second: %v", err)
	}
	if first.InstanceNo != 1 || second.InstanceNo != 2 {
		t.Fatalf("instance numbers = %d, %d, want 1, 2", first.InstanceNo, second.InstanceNo)
	}
	if got := FormatToolPaneLabel(*second); got != "claude#2" {
		t.Fatalf("label = %q, want %q", got, "claude#2")
	}
}

func TestFindToolPaneReturnsRequestedInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "codex", "%21", "run"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "codex", "%22", "run"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	inst, err := FindToolPane("sess-1", "codex", 2)
	if err != nil {
		t.Fatalf("FindToolPane: %v", err)
	}
	if inst.PaneID != "%22" {
		t.Fatalf("PaneID = %q, want %q", inst.PaneID, "%22")
	}
}

func TestRemoveToolPaneByPaneIDDeletesOnlyMatchingPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "claude", "%31", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "claude", "%32", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	if err := RemoveToolPaneByPaneID("sess-1", "%31"); err != nil {
		t.Fatalf("RemoveToolPaneByPaneID: %v", err)
	}

	items, err := ListToolPanes("sess-1")
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 1 || items[0].PaneID != "%32" {
		t.Fatalf("remaining panes = %#v, want only %%32", items)
	}
}

func TestPruneToolPanesRemovesDeadEntriesWithoutReusingNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "claude", "%41", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "claude", "%42", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	items, err := PruneToolPanes("sess-1", func(paneID string) bool {
		return paneID == "%42"
	})
	if err != nil {
		t.Fatalf("PruneToolPanes: %v", err)
	}
	if len(items) != 1 || items[0].PaneID != "%42" {
		t.Fatalf("items after prune = %#v, want only %%42", items)
	}

	next, err := RegisterToolPane("sess-1", "claude", "%43", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane after prune: %v", err)
	}
	if next.InstanceNo != 3 {
		t.Fatalf("InstanceNo = %d, want 3", next.InstanceNo)
	}
}

func TestReadToolPaneRegistryMissingFileReturnsEmptyRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reg, err := ReadToolPaneRegistry("sess-missing")
	if err != nil {
		t.Fatalf("ReadToolPaneRegistry: %v", err)
	}
	if len(reg.Instances) != 0 {
		t.Fatalf("Instances len = %d, want 0", len(reg.Instances))
	}
	if _, err := os.Stat(toolPaneRegistryPath("sess-missing")); !os.IsNotExist(err) {
		t.Fatalf("expected no registry file yet, stat err=%v", err)
	}
}

func TestRegisterToolPaneRejectsBlankToolName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := RegisterToolPane("sess-blank", "   ", "%100", "shell"); err == nil {
		t.Fatalf("expected error when tool name is blank")
	}
}

func TestRegisterToolPaneRejectsBlankPaneID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := RegisterToolPane("sess-blank", "claude", "   ", "shell"); err == nil {
		t.Fatalf("expected error when pane id is blank")
	}
}

func TestFindToolPaneRejectsBlankToolName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := FindToolPane("sess-1", "   ", 1); err == nil {
		t.Fatalf("expected error when tool name is blank")
	}
}

func TestWriteToolPaneRegistryRejectsBlankSessionID(t *testing.T) {
	if err := WriteToolPaneRegistry("", &ToolPaneRegistry{}); err == nil {
		t.Fatalf("expected error when session_id is blank")
	}
}

func TestRegisterToolPaneBlocksWhileLockHeld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	session := "sess-blocking"
	hold := make(chan struct{})
	release := make(chan struct{})
	var holdErr error
	go func() {
		holdErr = withToolPaneLock(session, func() error {
			close(hold)
			<-release
			return nil
		})
	}()
	<-hold
	regDone := make(chan struct{})
	var regErr error
	go func() {
		_, regErr = RegisterToolPane(session, "claude", "%hb", "shell")
		close(regDone)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-regDone:
		t.Fatalf("register should stay blocked while lock held")
	default:
	}
	close(release)
	select {
	case <-regDone:
		if regErr != nil {
			t.Fatalf("register failed after release: %v", regErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("register did not finish after release")
	}
	if holdErr != nil {
		t.Fatalf("hold lock func error: %v", holdErr)
	}
}

func TestRegisterToolPaneDoesNotStealLockWithoutHeartbeatOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	session := "sess-no-steal"
	hold := make(chan struct{})
	release := make(chan struct{})
	var holdErr error
	go func() {
		holdErr = withToolPaneLock(session, func() error {
			close(hold)
			_ = os.Remove(filepath.Join(toolPaneLockPath(session), "heartbeat"))
			<-release
			return nil
		})
	}()
	<-hold

	regDone := make(chan struct{})
	var regErr error
	go func() {
		_, regErr = RegisterToolPane(session, "claude", "%steal", "shell")
		close(regDone)
	}()

	time.Sleep(100 * time.Millisecond)
	select {
	case <-regDone:
		t.Fatalf("register should remain blocked even when heartbeat path is missing")
	default:
	}

	close(release)
	select {
	case <-regDone:
		if regErr != nil {
			t.Fatalf("register failed after release: %v", regErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("register did not finish after release")
	}
	if holdErr != nil {
		t.Fatalf("hold lock func error: %v", holdErr)
	}
}
