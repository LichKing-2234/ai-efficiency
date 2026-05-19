package attributionlocal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

func TestScanner_ScanWorkspaceReadsMatchingCodexJSONL(t *testing.T) {
	fixture := buildAttributionFixture(t)
	scanner := NewScanner()

	first, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan events = %d, want 1", len(first))
	}
	if first[0].DedupeKey != "codex-jsonl:sess-1:resp-1" {
		t.Fatalf("dedupe key = %q, want %q", first[0].DedupeKey, "codex-jsonl:sess-1:resp-1")
	}
}

func TestScanner_IgnoresGlobalCodexSQLiteTransportLogs(t *testing.T) {
	fixture := buildSQLiteOnlyAttributionFixture(t)
	scanner := NewScanner()

	first, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(first) != 0 {
		t.Fatal("expected first scan events")
	}
}

func TestFindCodexJSONLFiles_IgnoresWorkspaceScopedCodexHome(t *testing.T) {
	workspaceRoot := t.TempDir()
	homeDir := t.TempDir()

	workspaceCodex := filepath.Join(workspaceRoot, ".ae", "codex-home", "sessions", "workspace.jsonl")
	if err := os.MkdirAll(filepath.Dir(workspaceCodex), 0o700); err != nil {
		t.Fatalf("mkdir workspace codex dir: %v", err)
	}
	if err := os.WriteFile(workspaceCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write workspace codex file: %v", err)
	}

	globalCodex := filepath.Join(homeDir, ".codex", "sessions", "global.jsonl")
	if err := os.MkdirAll(filepath.Dir(globalCodex), 0o700); err != nil {
		t.Fatalf("mkdir global codex dir: %v", err)
	}
	if err := os.WriteFile(globalCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write global codex file: %v", err)
	}

	paths := findCodexJSONLFiles(workspaceRoot, homeDir)
	if len(paths) != 1 || paths[0] != globalCodex {
		t.Fatalf("paths = %v, want only %s", paths, globalCodex)
	}
}

func TestMustWorkspaceID_UsesGitdirFileForLinkedWorktreeLayout(t *testing.T) {
	workspaceRoot := t.TempDir()
	gitDir := filepath.Join(t.TempDir(), "gitdir")
	gitCommonDir := filepath.Join(t.TempDir(), "git-common")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("mkdir gitDir: %v", err)
	}
	if err := os.MkdirAll(gitCommonDir, 0o700); err != nil {
		t.Fatalf("mkdir gitCommonDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte(gitCommonDir+"\n"), 0o600); err != nil {
		t.Fatalf("write commondir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o600); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got, err := mustWorkspaceID(workspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}

	want, err := session.DeriveWorkspaceID(workspaceRoot, workspaceRoot, gitDir, gitCommonDir)
	if err != nil {
		t.Fatalf("DeriveWorkspaceID: %v", err)
	}
	if got != want {
		t.Fatalf("workspaceID = %q, want %q", got, want)
	}
}
