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

func TestParseCodexSQLite_ExtractsWebsocketResponseCompletedUsage(t *testing.T) {
	dbPath := buildCodexSQLiteFixture(t, []string{
		`session_loop{thread_id=019e6374-fdf3-7ff2-96bf-81a5fbccd716}:responses_websocket.stream_request{}: websocket event: {"type":"response.completed","response":{"id":"resp-json","completed_at":1779855390,"usage":{"input_tokens":205901,"input_tokens_details":{"cached_tokens":205184},"output_tokens":632,"output_tokens_details":{"reasoning_tokens":244},"total_tokens":206533}}} event.timestamp=2026-05-27T04:16:30Z`,
	})

	parser := NewCodexSQLiteParser()
	events, _, err := parser.Parse(dbPath, CodexSQLiteWatermark{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	got := events[0]
	if got.ToolSessionID != "019e6374-fdf3-7ff2-96bf-81a5fbccd716" || got.ToolEventID != "resp-json" {
		t.Fatalf("event identity = %s/%s, want codex thread id/resp-json", got.ToolSessionID, got.ToolEventID)
	}
	if got.InputTokens != 205901 || got.OutputTokens != 632 || got.CachedInputTokens != 205184 || got.ReasoningTokens != 244 {
		t.Fatalf("event usage = %+v", got)
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
