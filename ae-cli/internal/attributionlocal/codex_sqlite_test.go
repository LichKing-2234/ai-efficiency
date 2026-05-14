package attributionlocal

import "testing"

func TestParseCodexSQLite_ExtractsResponseCompletedUsage(t *testing.T) {
	t.Parallel()

	dbPath := buildCodexSQLiteFixture(t, []string{
		`event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-05-13T10:00:00Z conversation.id=conv-1 response.id=resp-1`,
	})

	parser := NewCodexSQLiteParser()
	events, watermark, err := parser.Parse(dbPath, CodexSQLiteWatermark{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].ToolSessionID != "conv-1" || events[0].ToolEventID != "resp-1" {
		t.Fatalf("event = %+v", events[0])
	}
	if watermark.LastLogID == 0 {
		t.Fatalf("watermark.LastLogID = %d, want > 0", watermark.LastLogID)
	}
}
