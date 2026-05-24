package attributionlocal

import "testing"

func TestParseCodexSQLite_ExtractsResponseCompletedUsage(t *testing.T) {
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

func TestParseCodexSQLite_FirstScanUsesRecentLookbackWindow(t *testing.T) {
	oldLookback := codexSQLiteInitialLookbackRows
	codexSQLiteInitialLookbackRows = 2
	t.Cleanup(func() { codexSQLiteInitialLookbackRows = oldLookback })

	dbPath := buildCodexSQLiteFixture(t, []string{
		`event.name="codex.sse_event" event.kind=response.completed conversation.id=old response.id=old input_token_count=1`,
		`event.name="codex.sse_event" event.kind=response.completed conversation.id=older response.id=older input_token_count=2`,
		`event.name="codex.sse_event" event.kind=response.completed conversation.id=recent response.id=recent input_token_count=3`,
	})

	parser := NewCodexSQLiteParser()
	events, watermark, err := parser.Parse(dbPath, CodexSQLiteWatermark{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].ToolSessionID == "old" || events[1].ToolSessionID == "old" {
		t.Fatalf("events = %+v, want initial scan to skip rows outside lookback", events)
	}
	if watermark.LastLogID != 3 {
		t.Fatalf("watermark.LastLogID = %d, want 3", watermark.LastLogID)
	}
}
