package attributionlocal

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestParseKiroJSON_UsesCreditAndConversationID(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "kiro.json", `{"session_id":"root-1","cwd":"/tmp/repo","session_state":{"conversation_metadata":{"user_turn_metadatas":[{"total_request_count":2,"context_usage_percentage":4.2,"metering_usage":[{"value":0.1,"unit":"credit"},{"value":0.2,"unit":"credit"}]}]},"rts_model_state":{"conversation_id":"conv-1"}}}`)
	want := time.Date(2026, 5, 19, 11, 22, 33, 0, time.UTC)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	events, err := ParseKiroJSON(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroJSON: %v", err)
	}
	if len(events) != 1 || events[0].ToolSessionID != "conv-1" || math.Abs(events[0].CreditUsage-0.3) > 1e-9 {
		t.Fatalf("events = %+v", events)
	}
	if !events[0].ObservedStartAt.Equal(want) || !events[0].ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want %s", events[0].ObservedStartAt, events[0].ObservedEndAt, want)
	}
}

func TestParseKiroCLISQLite_UsesCreditAndMessageID(t *testing.T) {
	t.Parallel()

	dbPath := buildKiroCLISQLiteFixture(t, "/tmp/repo")

	events, err := ParseKiroCLISQLite(dbPath, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseKiroCLISQLite: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.ToolSessionID != "conv-1" {
		t.Fatalf("tool session id = %q, want conv-1", ev.ToolSessionID)
	}
	if ev.ToolEventID != "msg-1" {
		t.Fatalf("tool event id = %q, want msg-1", ev.ToolEventID)
	}
	if ev.DedupeKey != "kiro-cli:conv-1:msg-1" {
		t.Fatalf("dedupe key = %q, want %q", ev.DedupeKey, "kiro-cli:conv-1:msg-1")
	}
	if math.Abs(ev.CreditUsage-0.10903188126036485) > 1e-12 {
		t.Fatalf("credit usage = %v", ev.CreditUsage)
	}
	if ev.RequestCount != 1 {
		t.Fatalf("request count = %d, want 1", ev.RequestCount)
	}
	if math.Abs(ev.ContextUsagePct-3.2832) > 1e-9 {
		t.Fatalf("context usage pct = %v", ev.ContextUsagePct)
	}
	wantStart := time.UnixMilli(1779285309036).UTC()
	wantEnd := time.UnixMilli(1779285314178).UTC()
	if !ev.ObservedStartAt.Equal(wantStart) || !ev.ObservedEndAt.Equal(wantEnd) {
		t.Fatalf("observed timestamps = %s / %s, want %s / %s", ev.ObservedStartAt, ev.ObservedEndAt, wantStart, wantEnd)
	}
}
