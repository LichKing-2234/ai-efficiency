package collector

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func TestBuildSnapshotAggregatesCodexClaudeAndKiro(t *testing.T) {
	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:    []string{"testdata/codex-session.jsonl"},
		ClaudeFiles:   []string{"testdata/claude-session.jsonl"},
		KiroFiles:     []string{"testdata/kiro-session.json"},
		WorkspaceRoot: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex.SourceSessionID != "codex-sess-1" || snapshot.Codex.TotalTokens != 1450 {
		t.Fatalf("Codex snapshot = %+v", snapshot.Codex)
	}
	if snapshot.Claude.InputTokens != 1100 || snapshot.Claude.CachedInputTokens != 90 {
		t.Fatalf("Claude snapshot = %+v", snapshot.Claude)
	}
	if snapshot.Kiro.ConversationID != "conv-kiro-1" || snapshot.Kiro.ContextUsagePct != 47.5 {
		t.Fatalf("Kiro snapshot = %+v", snapshot.Kiro)
	}
}

func TestBuildSnapshotPrefersMostRecentValidFilePerTool(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	dir := t.TempDir()

	codexOld := filepath.Join(dir, "codex-old.jsonl")
	codexNew := filepath.Join(dir, "codex-new.jsonl")
	if err := os.WriteFile(codexOld, []byte(`{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-old","cwd":"`+workspaceRoot+`"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":9000,"cached_input_tokens":0,"output_tokens":1000,"reasoning_output_tokens":0,"total_tokens":10000}}}}`), 0o600); err != nil {
		t.Fatalf("write codex old: %v", err)
	}
	if err := os.WriteFile(codexNew, []byte(`{"timestamp":"2026-03-28T09:00:00Z","type":"session_meta","payload":{"id":"codex-new","cwd":"`+workspaceRoot+`"}}
{"timestamp":"2026-03-28T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":400,"cached_input_tokens":50,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":500}}}}`), 0o600); err != nil {
		t.Fatalf("write codex new: %v", err)
	}

	claudeOld := filepath.Join(dir, "claude-old.jsonl")
	claudeNew := filepath.Join(dir, "claude-new.jsonl")
	if err := os.WriteFile(claudeOld, []byte(`{"type":"assistant","cwd":"`+workspaceRoot+`","sessionId":"claude-old","message":{"usage":{"input_tokens":900,"output_tokens":100,"cache_creation_input_tokens":50,"cache_read_input_tokens":50}}}`), 0o600); err != nil {
		t.Fatalf("write claude old: %v", err)
	}
	if err := os.WriteFile(claudeNew, []byte(`{"type":"assistant","cwd":"`+workspaceRoot+`","sessionId":"claude-new","message":{"usage":{"input_tokens":20,"output_tokens":5,"cache_creation_input_tokens":3,"cache_read_input_tokens":2}}}`), 0o600); err != nil {
		t.Fatalf("write claude new: %v", err)
	}

	kiroOld := filepath.Join(dir, "kiro-old.json")
	kiroNew := filepath.Join(dir, "kiro-new.json")
	if err := os.WriteFile(kiroOld, []byte(`{"session_id":"kiro-old","cwd":"`+workspaceRoot+`","session_state":{"rts_model_state":{"conversation_id":"conv-old","context_usage_percentage":11.5}}}`), 0o600); err != nil {
		t.Fatalf("write kiro old: %v", err)
	}
	if err := os.WriteFile(kiroNew, []byte(`{"session_id":"kiro-new","cwd":"`+workspaceRoot+`","session_state":{"rts_model_state":{"conversation_id":"conv-new","context_usage_percentage":22.5}}}`), 0o600); err != nil {
		t.Fatalf("write kiro new: %v", err)
	}

	oldTime := time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(1 * time.Hour)
	for _, path := range []string{codexOld, claudeOld, kiroOld} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("chtimes old %s: %v", path, err)
		}
	}
	for _, path := range []string{codexNew, claudeNew, kiroNew} {
		if err := os.Chtimes(path, newTime, newTime); err != nil {
			t.Fatalf("chtimes new %s: %v", path, err)
		}
	}

	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:    []string{codexOld, codexNew},
		ClaudeFiles:   []string{claudeOld, claudeNew},
		KiroFiles:     []string{kiroNew, kiroOld},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex == nil || snapshot.Codex.SourceSessionID != "codex-new" || snapshot.Codex.TotalTokens != 500 {
		t.Fatalf("Codex snapshot = %+v, want latest file", snapshot.Codex)
	}
	if snapshot.Claude == nil || snapshot.Claude.SourceSessionID != "claude-new" || snapshot.Claude.InputTokens != 20 || snapshot.Claude.CachedInputTokens != 5 {
		t.Fatalf("Claude snapshot = %+v, want latest file", snapshot.Claude)
	}
	if snapshot.Kiro == nil || snapshot.Kiro.ConversationID != "conv-new" || snapshot.Kiro.ContextUsagePct != 22.5 {
		t.Fatalf("Kiro snapshot = %+v, want latest file", snapshot.Kiro)
	}
}

func TestBuildSnapshotSkipsBrokenFilesAndKeepsOtherTools(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	dir := t.TempDir()

	codexBad := filepath.Join(dir, "codex-bad.jsonl")
	if err := os.WriteFile(codexBad, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("write codex bad: %v", err)
	}
	claudeGood := filepath.Join(dir, "claude-good.jsonl")
	if err := os.WriteFile(claudeGood, []byte(`{"type":"assistant","cwd":"`+workspaceRoot+`","sessionId":"claude-good","message":{"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":5}}}`), 0o600); err != nil {
		t.Fatalf("write claude good: %v", err)
	}
	kiroGood := filepath.Join(dir, "kiro-good.json")
	if err := os.WriteFile(kiroGood, []byte(`{"session_id":"kiro-good","cwd":"`+workspaceRoot+`","session_state":{"rts_model_state":{"conversation_id":"conv-good","context_usage_percentage":33.3}}}`), 0o600); err != nil {
		t.Fatalf("write kiro good: %v", err)
	}

	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:    []string{codexBad},
		ClaudeFiles:   []string{claudeGood},
		KiroFiles:     []string{kiroGood},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex != nil {
		t.Fatalf("expected broken codex file to be skipped, got %+v", snapshot.Codex)
	}
	if snapshot.Claude == nil || snapshot.Claude.SourceSessionID != "claude-good" {
		t.Fatalf("Claude snapshot = %+v", snapshot.Claude)
	}
	if snapshot.Kiro == nil || snapshot.Kiro.ConversationID != "conv-good" {
		t.Fatalf("Kiro snapshot = %+v", snapshot.Kiro)
	}
}

func TestBuildSnapshotReadsKiroCLISQLite(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	dbPath := buildKiroCLISQLiteSnapshotFixture(t, workspaceRoot)

	snapshot, err := BuildSnapshot(Paths{
		KiroFiles:     []string{dbPath},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot == nil || snapshot.Kiro == nil {
		t.Fatalf("expected kiro snapshot, got %+v", snapshot)
	}
	if snapshot.Kiro.ConversationID != "conv-kiro-cli-1" {
		t.Fatalf("conversation id = %q, want conv-kiro-cli-1", snapshot.Kiro.ConversationID)
	}
	if math.Abs(snapshot.Kiro.CreditUsage-0.10903188126036485) > 1e-12 {
		t.Fatalf("credit usage = %v", snapshot.Kiro.CreditUsage)
	}
	if math.Abs(snapshot.Kiro.ContextUsagePct-3.2832) > 1e-9 {
		t.Fatalf("context usage = %v", snapshot.Kiro.ContextUsagePct)
	}
}

func TestBuildSnapshotReadsKiroIDEExecution(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	sessionIndexPath, execPath := buildKiroIDESnapshotFixture(t, workspaceRoot)

	snapshot, err := BuildSnapshot(Paths{
		KiroFiles:     []string{sessionIndexPath, execPath},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot == nil || snapshot.Kiro == nil {
		t.Fatalf("expected kiro snapshot, got %+v", snapshot)
	}
	if snapshot.Kiro.ConversationID != "chat-sess-1" {
		t.Fatalf("conversation id = %q, want chat-sess-1", snapshot.Kiro.ConversationID)
	}
	if math.Abs(snapshot.Kiro.CreditUsage-(0.007750866932006633+0.05991760597014926)) > 1e-12 {
		t.Fatalf("credit usage = %v", snapshot.Kiro.CreditUsage)
	}
	if math.Abs(snapshot.Kiro.ContextUsagePct-23.259000778198242) > 1e-9 {
		t.Fatalf("context usage = %v", snapshot.Kiro.ContextUsagePct)
	}
}

func TestDefaultPathsMergesEnvOverridesWithDefaults(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	overrideCodex := filepath.Join(tmpHome, "override-codex.jsonl")
	if err := os.WriteFile(overrideCodex, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write override codex: %v", err)
	}
	t.Setenv("AE_CODEX_SESSION_FILES", overrideCodex)

	claudeDefault := filepath.Join(tmpHome, ".claude", "claude-default.jsonl")
	if err := os.MkdirAll(filepath.Dir(claudeDefault), 0o700); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	if err := os.WriteFile(claudeDefault, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write claude default: %v", err)
	}

	kiroDefault := filepath.Join(tmpHome, ".kiro", "kiro-default.json")
	if err := os.MkdirAll(filepath.Dir(kiroDefault), 0o700); err != nil {
		t.Fatalf("mkdir kiro dir: %v", err)
	}
	if err := os.WriteFile(kiroDefault, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write kiro default: %v", err)
	}

	paths := DefaultPaths("/tmp/repo")
	if len(paths.CodexFiles) != 1 || paths.CodexFiles[0] != overrideCodex {
		t.Fatalf("CodexFiles = %v, want [%s]", paths.CodexFiles, overrideCodex)
	}
	if len(paths.ClaudeFiles) == 0 || paths.ClaudeFiles[0] != claudeDefault {
		t.Fatalf("ClaudeFiles = %v, want default claude path", paths.ClaudeFiles)
	}
	if len(paths.KiroFiles) == 0 || paths.KiroFiles[0] != kiroDefault {
		t.Fatalf("KiroFiles = %v, want default kiro path", paths.KiroFiles)
	}
}

func TestBuildSnapshotToleratesDirtyJSONLTrailingLines(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	dir := t.TempDir()

	codex := filepath.Join(dir, "codex-dirty.jsonl")
	if err := os.WriteFile(codex, []byte(`{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-dirty","cwd":"`+workspaceRoot+`"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":135}}}}
{broken`), 0o600); err != nil {
		t.Fatalf("write codex dirty: %v", err)
	}

	claude := filepath.Join(dir, "claude-dirty.jsonl")
	if err := os.WriteFile(claude, []byte(`{"type":"assistant","cwd":"`+workspaceRoot+`","sessionId":"claude-dirty","message":{"usage":{"input_tokens":50,"output_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":5}}}
{broken`), 0o600); err != nil {
		t.Fatalf("write claude dirty: %v", err)
	}

	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:    []string{codex},
		ClaudeFiles:   []string{claude},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex == nil || snapshot.Codex.SourceSessionID != "codex-dirty" {
		t.Fatalf("Codex snapshot = %+v", snapshot.Codex)
	}
	if snapshot.Claude == nil || snapshot.Claude.SourceSessionID != "claude-dirty" {
		t.Fatalf("Claude snapshot = %+v", snapshot.Claude)
	}
}

func TestDefaultPathsOrdersNewestDefaultFilesFirst(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	codexDir := filepath.Join(tmpHome, ".codex", "sessions")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	base := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		path := filepath.Join(codexDir, fmt.Sprintf("codex-%02d.jsonl", i))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	paths := DefaultPaths("/tmp/repo")
	if len(paths.CodexFiles) != 12 {
		t.Fatalf("CodexFiles len = %d, want 12", len(paths.CodexFiles))
	}
	if got := filepath.Base(paths.CodexFiles[0]); got != "codex-11.jsonl" {
		t.Fatalf("first CodexFiles entry = %s, want newest file", got)
	}
}

func TestDefaultPathsUsesGlobalCodexSessionsOnly(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	workspaceRoot := t.TempDir()
	globalCodexDir := filepath.Join(tmpHome, ".codex", "sessions", "2026", "04", "15")
	if err := os.MkdirAll(globalCodexDir, 0o700); err != nil {
		t.Fatalf("mkdir global codex dir: %v", err)
	}
	globalCodex := filepath.Join(globalCodexDir, "global-codex.jsonl")
	if err := os.WriteFile(globalCodex, []byte(`{"timestamp":"2026-04-15T09:00:00Z","type":"session_meta","payload":{"id":"codex-global","cwd":"`+workspaceRoot+`"}}
{"timestamp":"2026-04-15T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":2,"reasoning_output_tokens":0,"total_tokens":3}}}}`), 0o600); err != nil {
		t.Fatalf("write global codex file: %v", err)
	}
	for _, ignored := range []string{
		filepath.Join(tmpHome, ".codex", "debug.jsonl"),
		filepath.Join(tmpHome, ".codex", "archived_sessions", "old.jsonl"),
	} {
		if err := os.MkdirAll(filepath.Dir(ignored), 0o700); err != nil {
			t.Fatalf("mkdir ignored codex dir: %v", err)
		}
		if err := os.WriteFile(ignored, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write ignored codex file: %v", err)
		}
	}

	paths := DefaultPaths(workspaceRoot)
	if len(paths.CodexFiles) != 1 || paths.CodexFiles[0] != globalCodex {
		t.Fatalf("CodexFiles = %v, want only global file %s", paths.CodexFiles, globalCodex)
	}

	snapshot, err := BuildSnapshot(paths)
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot == nil || snapshot.Codex == nil {
		t.Fatalf("expected codex snapshot from global file, got %+v", snapshot)
	}
	if snapshot.Codex.SourceSessionID != "codex-global" || snapshot.Codex.TotalTokens != 3 {
		t.Fatalf("Codex snapshot = %+v, want global token details", snapshot.Codex)
	}
}

func TestBuildSnapshotFindsOlderValidFileAfterNewerInvalidDefaults(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	workspaceRoot := "/tmp/repo"
	codexDir := filepath.Join(tmpHome, ".codex", "sessions")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	base := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		path := filepath.Join(codexDir, fmt.Sprintf("codex-bad-%02d.jsonl", i))
		if err := os.WriteFile(path, []byte("{broken\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		ts := base.Add(time.Duration(i+1) * time.Minute)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	valid := filepath.Join(codexDir, "codex-valid.jsonl")
	if err := os.WriteFile(valid, []byte(`{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-valid","cwd":"`+workspaceRoot+`"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":200,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":235}}}}`), 0o600); err != nil {
		t.Fatalf("write valid codex: %v", err)
	}
	if err := os.Chtimes(valid, base, base); err != nil {
		t.Fatalf("chtimes valid: %v", err)
	}

	paths := DefaultPaths(workspaceRoot)
	snapshot, err := BuildSnapshot(paths)
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex == nil || snapshot.Codex.SourceSessionID != "codex-valid" {
		t.Fatalf("Codex snapshot = %+v, want older valid file", snapshot.Codex)
	}
}

func TestBuildSnapshotReturnsNilWhenNoToolDataFound(t *testing.T) {
	snapshot, err := BuildSnapshot(Paths{
		WorkspaceRoot: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot != nil {
		t.Fatalf("snapshot = %+v, want nil when no tool data found", snapshot)
	}
}

func TestBuildSnapshotReadsLargeCodexAndClaudeJSONLLines(t *testing.T) {
	workspaceRoot := "/tmp/repo"
	dir := t.TempDir()
	blob := strings.Repeat("x", 5*1024*1024)

	codex := filepath.Join(dir, "codex-large.jsonl")
	if err := os.WriteFile(codex, []byte(`{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-large","cwd":"`+workspaceRoot+`","note":"`+blob+`"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":0,"total_tokens":13}},"note":"`+blob+`"}}`), 0o600); err != nil {
		t.Fatalf("write large codex: %v", err)
	}

	claude := filepath.Join(dir, "claude-large.jsonl")
	if err := os.WriteFile(claude, []byte(`{"type":"assistant","cwd":"`+workspaceRoot+`","sessionId":"claude-large","message":{"usage":{"input_tokens":20,"output_tokens":3,"cache_creation_input_tokens":1,"cache_read_input_tokens":1},"note":"`+blob+`"}}`), 0o600); err != nil {
		t.Fatalf("write large claude: %v", err)
	}

	snapshot, err := BuildSnapshot(Paths{
		CodexFiles:    []string{codex},
		ClaudeFiles:   []string{claude},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if snapshot.Codex == nil || snapshot.Codex.SourceSessionID != "codex-large" {
		t.Fatalf("Codex snapshot = %+v", snapshot.Codex)
	}
	if snapshot.Claude == nil || snapshot.Claude.SourceSessionID != "claude-large" {
		t.Fatalf("Claude snapshot = %+v", snapshot.Claude)
	}
}

func buildKiroCLISQLiteSnapshotFixture(t *testing.T, workspaceRoot string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "Library", "Application Support", "kiro-cli")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir kiro-cli dir: %v", err)
	}
	path := filepath.Join(base, "data.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations_v2 (key TEXT NOT NULL, conversation_id TEXT NOT NULL, value TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (key, conversation_id))`); err != nil {
		t.Fatalf("create conversations_v2: %v", err)
	}
	value := `{"conversation_id":"conv-kiro-cli-1","history":[{"assistant":{"Response":{"message_id":"msg-kiro-cli-1","content":"reply"}},"request_metadata":{"context_usage_percentage":3.2832,"message_id":"msg-kiro-cli-1","request_start_timestamp_ms":1779285309036,"stream_end_timestamp_ms":1779285314178}}],"user_turn_metadata":{"usage_info":[{"value":0.10903188126036485,"unit":"credit","unit_plural":"credits"}]}}`
	if _, err := db.Exec(`INSERT INTO conversations_v2 (key, conversation_id, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, workspaceRoot, "conv-kiro-cli-1", value, 1779285314178, 1779285314178); err != nil {
		t.Fatalf("insert conversations_v2: %v", err)
	}
	return path
}

func buildKiroIDESnapshotFixture(t *testing.T, workspaceRoot string) (string, string) {
	t.Helper()

	base := filepath.Join(t.TempDir(), "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent")
	sessionDir := filepath.Join(base, "workspace-sessions", "L3RtcC9yZXBv")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("mkdir kiro ide session dir: %v", err)
	}
	sessionIndexPath := filepath.Join(sessionDir, "sessions.json")
	sessionIndex := fmt.Sprintf(`[{"sessionId":"chat-sess-1","title":"hi","dateCreated":"1779284885045","workspaceDirectory":%q}]`, workspaceRoot)
	if err := os.WriteFile(sessionIndexPath, []byte(sessionIndex), 0o600); err != nil {
		t.Fatalf("write kiro ide session index: %v", err)
	}

	execDir := filepath.Join(base, "8794d1d6b05461c486ae3c70a25dbd02", "414d1636299d2b9e4ce7e17fb11f63e9")
	if err := os.MkdirAll(execDir, 0o700); err != nil {
		t.Fatalf("mkdir kiro ide exec dir: %v", err)
	}
	execPath := filepath.Join(execDir, "71d22ce2a62c4cdc077c824e07bd8650")
	body := fmt.Sprintf(`{
  "executionId": "exec-1",
  "workflowType": "chat-agent",
  "status": "succeed",
  "startTime": 1779288013500,
  "chatSessionID": "",
  "chatSessionId": "chat-sess-1",
  "endTime": 1779288019211,
  "usageSummary": [
    {"usage": 0.007750866932006633, "unit": "credit", "unitPlural": "credits"},
    {"usage": 0.05991760597014926, "unit": "credit", "unitPlural": "credits"}
  ],
  "contextUsagePercentage": 23.259000778198242,
  "workspaceContext": {"cwd": %q}
}`, workspaceRoot)
	if err := os.WriteFile(execPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write kiro ide execution: %v", err)
	}
	return sessionIndexPath, execPath
}

func TestDefaultPathsResolvesKiroCLIDatabaseAcrossPlatforms(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	for _, key := range []string{"KIRO_CLI_DB", "KIRO_CLI_DATA_DIR", "XDG_DATA_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(key, "")
	}

	dbDir := filepath.Join(t.TempDir(), "kiro-cli")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		t.Fatalf("mkdir kiro-cli dir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "data.sqlite3")
	if err := os.WriteFile(dbPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write kiro-cli db: %v", err)
	}
	t.Setenv("KIRO_CLI_DATA_DIR", dbDir)

	paths := DefaultPaths("/tmp/repo")
	found := false
	for _, path := range paths.KiroFiles {
		if path == dbPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("KiroFiles = %v, want it to include %s", paths.KiroFiles, dbPath)
	}
}
