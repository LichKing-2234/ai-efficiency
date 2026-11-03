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

// A session line carrying a large tool result exceeds bufio.Scanner's default
// token. The parser must keep reading past it rather than reporting an error the
// workspace scan turns into a silently skipped file.
func TestParseClaudeJSONL_ReadsPastLineLongerThanScannerDefault(t *testing.T) {
	workspace := t.TempDir()
	huge := strings.Repeat("x", 200*1024)
	path := writeFile(t, "claude-long-line.jsonl", strings.Join([]string{
		`{"type":"assistant","cwd":"` + workspace + `","sessionId":"sess-1","timestamp":"2026-08-27T10:00:00Z","message":{"id":"msg-early","usage":{"input_tokens":1,"output_tokens":2}}}`,
		`{"type":"user","cwd":"` + workspace + `","sessionId":"sess-1","message":{"content":"` + huge + `"}}`,
		`{"type":"assistant","cwd":"` + workspace + `","sessionId":"sess-1","timestamp":"2026-08-27T10:01:00Z","message":{"id":"msg-late","usage":{"input_tokens":3,"output_tokens":4}}}`,
	}, "\n")+"\n")

	events, err := ParseClaudeJSONL(path, workspace)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		seen[ev.ToolEventID] = true
	}
	if !seen["msg-early"] || !seen["msg-late"] {
		t.Fatalf("events = %+v, want both the message before and the message after the oversized line", events)
	}
}

// A final line with no trailing newline must still be read, and must not loop.
func TestParseClaudeJSONL_ReadsFinalLineWithoutTrailingNewline(t *testing.T) {
	workspace := t.TempDir()
	path := writeFile(t, "claude-no-final-newline.jsonl",
		`{"type":"assistant","cwd":"`+workspace+`","sessionId":"sess-1","timestamp":"2026-08-27T10:00:00Z","message":{"id":"msg-1","usage":{"input_tokens":1,"output_tokens":2}}}`)

	events, err := ParseClaudeJSONL(path, workspace)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 || events[0].ToolEventID != "msg-1" {
		t.Fatalf("events = %+v, want the unterminated final line", events)
	}
}
