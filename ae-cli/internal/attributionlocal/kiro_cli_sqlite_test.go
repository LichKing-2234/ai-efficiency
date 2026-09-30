package attributionlocal

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

// buildKiroCLISQLiteTurnFixture writes a conversations_v2 row whose shape
// mirrors a real Kiro CLI turn: history[] carries every request the turn made,
// usage_info[] carries one credit record per request, and requests[] is short by
// the final answer step.
func buildKiroCLISQLiteTurnFixture(t *testing.T, workspaceRoot, value string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "kiro-cli")
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
	if _, err := db.Exec(`INSERT INTO conversations_v2 (key, conversation_id, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, workspaceRoot, "conv-1", value, 1779285314178, 1779285314178); err != nil {
		t.Fatalf("insert conversations_v2: %v", err)
	}
	return path
}

// Real Kiro CLI turns drop the final answer step from user_turn_metadata.requests
// while history[] and user_turn_metadata.usage_info[] both keep it, so the
// request count must come from the history backbone, not from requests[].
func TestParseKiroCLISQLite_CountsRequestsFromHistoryNotRequestsArray(t *testing.T) {
	t.Parallel()

	value := `{"conversation_id":"conv-1",` +
		`"history":[` +
		`{"user":{"content":{"Prompt":{"prompt":"test"}}},"assistant":{"ToolUse":{"message_id":"msg-1","tool_uses":[{"id":"tool-1","name":"fs_write"}]}},"request_metadata":{"request_id":"req-1","message_id":"msg-1","chat_conversation_type":"ToolUse","context_usage_percentage":1.1,"request_start_timestamp_ms":1779285309036,"stream_end_timestamp_ms":1779285310000}},` +
		`{"user":{"content":{"ToolUseResults":{}}},"assistant":{"ToolUse":{"message_id":"msg-2","tool_uses":[{"id":"tool-2","name":"fs_read"}]}},"request_metadata":{"request_id":"req-2","message_id":"msg-2","chat_conversation_type":"ToolUse","context_usage_percentage":1.2,"request_start_timestamp_ms":1779285310100,"stream_end_timestamp_ms":1779285312000}},` +
		`{"user":{"content":{"ToolUseResults":{}}},"assistant":{"Response":{"message_id":"msg-3","content":"done"}},"request_metadata":{"request_id":"req-3","message_id":"msg-3","chat_conversation_type":"NotToolUse","context_usage_percentage":1.3,"request_start_timestamp_ms":1779285312100,"stream_end_timestamp_ms":1779285314178}}` +
		`],` +
		`"user_turn_metadata":{"continuation_id":"cont-1",` +
		`"requests":[{"request_id":"req-1"},{"request_id":"req-2"}],` +
		`"usage_info":[{"value":0.03,"unit":"credit"},{"value":0.02,"unit":"credit"},{"value":0.01,"unit":"credit"}]}}`

	dbPath := buildKiroCLISQLiteTurnFixture(t, "/tmp/repo", value)

	events, err := ParseKiroCLISQLite(dbPath, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroCLISQLite: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.RequestCount != 3 {
		t.Fatalf("request count = %d, want 3 (history/usage_info length, not requests length)", ev.RequestCount)
	}
	if math.Abs(ev.CreditUsage-0.06) > 1e-12 {
		t.Fatalf("credit usage = %v, want 0.06", ev.CreditUsage)
	}
	if ev.ToolEventID != "msg-3" {
		t.Fatalf("tool event id = %q, want msg-3", ev.ToolEventID)
	}
}

// History entries without any request evidence must not inflate the count, and a
// row that carries no request evidence at all still reports the history length
// rather than a fabricated number.
func TestParseKiroCLISQLite_SkipsHistoryEntriesWithoutRequestEvidence(t *testing.T) {
	t.Parallel()

	value := `{"conversation_id":"conv-1",` +
		`"history":[` +
		`{"user":{"content":{"Prompt":{"prompt":"test"}}},"assistant":{"ToolUse":{"message_id":"msg-1"}},"request_metadata":{"request_id":"req-1","message_id":"msg-1","request_start_timestamp_ms":1779285309036,"stream_end_timestamp_ms":1779285310000}},` +
		`{"user":{"content":{"CancelledToolUses":{}}},"assistant":{}},` +
		`{"user":{"content":{"ToolUseResults":{}}},"assistant":{"Response":{"message_id":"msg-2","content":"done"}},"request_metadata":{"request_id":"req-2","message_id":"msg-2","request_start_timestamp_ms":1779285312100,"stream_end_timestamp_ms":1779285314178}}` +
		`],` +
		`"user_turn_metadata":{"continuation_id":"cont-1","requests":[],"usage_info":[{"value":0.05,"unit":"credit"},{"value":0.05,"unit":"credit"}]}}`

	dbPath := buildKiroCLISQLiteTurnFixture(t, "/tmp/repo", value)

	events, err := ParseKiroCLISQLite(dbPath, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroCLISQLite: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].RequestCount != 2 {
		t.Fatalf("request count = %d, want 2", events[0].RequestCount)
	}
}
