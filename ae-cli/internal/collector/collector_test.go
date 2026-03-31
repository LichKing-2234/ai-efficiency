package collector

import "testing"

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
