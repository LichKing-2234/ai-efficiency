package attributionlocal

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/session"
	_ "github.com/glebarez/go-sqlite"
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

func TestScanner_UsesCodexSQLiteBeforeJSONLFallback(t *testing.T) {
	fixture := buildSQLiteOnlyAttributionFixture(t)
	scanner := NewScanner()

	first, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan events = %d, want 1", len(first))
	}
	if first[0].DedupeKey != "codex:conv-1:resp-1" {
		t.Fatalf("dedupe key = %q, want %q", first[0].DedupeKey, "codex:conv-1:resp-1")
	}
}

func TestScanner_MatchesCodexSessionFromLinkedWorktreeCommonDir(t *testing.T) {
	mainRoot := fixtureRepoRoot(t)
	linkedRoot := filepath.Join(t.TempDir(), "linked")
	cmd := exec.Command("git", "worktree", "add", "-b", "linked-test", linkedRoot)
	cmd.Dir = mainRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	codexSessions := filepath.Join(home, ".codex", "sessions", "2026", "05", "27")
	if err := os.MkdirAll(codexSessions, 0o700); err != nil {
		t.Fatalf("mkdir codex sessions: %v", err)
	}
	codexJSONL := filepath.Join(codexSessions, "sess-1.jsonl")
	codexBody := `{"type":"session_meta","payload":{"id":"conv-1","cwd":"` + mainRoot + `"}}`
	if err := os.WriteFile(codexJSONL, []byte(codexBody), 0o600); err != nil {
		t.Fatalf("write codex session metadata: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	dbPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`); err != nil {
		t.Fatalf("create logs table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, `event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-05-27T03:56:32Z conversation.id=conv-1 response.id=resp-1`); err != nil {
		t.Fatalf("insert logs row: %v", err)
	}

	events, _, err := NewScanner().ScanWorkspace(linkedRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan linked worktree: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	wantWorkspaceID, err := mustWorkspaceID(linkedRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID(linkedRoot): %v", err)
	}
	if events[0].WorkspaceID != wantWorkspaceID {
		t.Fatalf("workspace_id = %q, want linked worktree %q", events[0].WorkspaceID, wantWorkspaceID)
	}
}

func TestScanner_SecondScanWithStateReturnsNoDuplicateSQLiteEvents(t *testing.T) {
	fixture := buildSQLiteOnlyAttributionFixture(t)
	scanner := NewScanner()

	first, state, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	second, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, state)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan events = %d, want 1", len(first))
	}
	if len(second) != 0 {
		t.Fatalf("second scan events = %d, want 0", len(second))
	}
}

func TestScanner_ScanWorkspaceReadsMatchingKiroCLISQLite(t *testing.T) {
	fixture := buildKiroCLISQLiteAttributionFixture(t)
	scanner := NewScanner()

	first, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan events = %d, want 1", len(first))
	}
	if first[0].DedupeKey != "kiro-cli:conv-1:msg-1" {
		t.Fatalf("dedupe key = %q, want %q", first[0].DedupeKey, "kiro-cli:conv-1:msg-1")
	}
}

func TestScanner_ScanWorkspaceReadsMatchingKiroIDEExecution(t *testing.T) {
	fixture := buildKiroIDEAttributionFixture(t)
	scanner := NewScanner()

	first, _, err := scanner.ScanWorkspace(fixture.WorkspaceRoot, ScanState{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan events = %d, want 1", len(first))
	}
	if first[0].DedupeKey != "kiro-ide:chat-sess-1:exec-1" {
		t.Fatalf("dedupe key = %q, want %q", first[0].DedupeKey, "kiro-ide:chat-sess-1:exec-1")
	}
}

func TestFindCodexJSONLFiles_UsesActiveAndArchivedCodexHome(t *testing.T) {
	homeDir := t.TempDir()

	globalCodex := filepath.Join(homeDir, ".codex", "sessions", "global.jsonl")
	if err := os.MkdirAll(filepath.Dir(globalCodex), 0o700); err != nil {
		t.Fatalf("mkdir global codex dir: %v", err)
	}
	if err := os.WriteFile(globalCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write global codex file: %v", err)
	}
	archivedCodex := filepath.Join(homeDir, ".codex", "archived_sessions", "old.jsonl")
	if err := os.MkdirAll(filepath.Dir(archivedCodex), 0o700); err != nil {
		t.Fatalf("mkdir archived codex dir: %v", err)
	}
	if err := os.WriteFile(archivedCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write archived codex file: %v", err)
	}
	topLevelCodex := filepath.Join(homeDir, ".codex", "debug.jsonl")
	if err := os.WriteFile(topLevelCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write top-level codex file: %v", err)
	}

	paths := findCodexJSONLFiles(t.TempDir(), homeDir)
	if len(paths) != 2 || paths[0] != archivedCodex || paths[1] != globalCodex {
		t.Fatalf("paths = %v, want active and archived sessions", paths)
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
