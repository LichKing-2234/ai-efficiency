package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

	codexDir := filepath.Join(tmpHome, ".codex")
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

func TestBuildSnapshotFindsOlderValidFileAfterNewerInvalidDefaults(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	workspaceRoot := "/tmp/repo"
	codexDir := filepath.Join(tmpHome, ".codex")
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
