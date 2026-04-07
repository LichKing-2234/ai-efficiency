package session

import (
	"os"
	"testing"
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
