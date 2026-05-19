package attributionlocal

import (
	"strings"
	"testing"
	"time"
)

func TestParseClaudeJSONL_PrefersEndTurnRecord(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "claude.jsonl", strings.Join([]string{
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":1}},"stop_reason":null,"timestamp":"2026-05-19T10:00:00Z"}`,
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":3}},"stop_reason":"end_turn","timestamp":"2026-05-19T10:00:01Z"}`,
	}, "\n"))

	events, err := ParseClaudeJSONL(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseClaudeJSONL: %v", err)
	}
	if len(events) != 1 || events[0].OutputTokens != 3 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].ObservedStartAt.IsZero() || events[0].ObservedEndAt.IsZero() {
		t.Fatalf("expected observed timestamps, got %+v", events[0])
	}
	want := time.Date(2026, 5, 19, 10, 0, 1, 0, time.UTC)
	if !events[0].ObservedStartAt.Equal(want) || !events[0].ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want %s", events[0].ObservedStartAt, events[0].ObservedEndAt, want)
	}
}
