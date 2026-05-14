package attributionlocal

import (
	"strings"
	"testing"
)

func TestParseClaudeJSONL_PrefersEndTurnRecord(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "claude.jsonl", strings.Join([]string{
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":1}},"stop_reason":null}`,
		`{"type":"assistant","cwd":"/tmp/repo","sessionId":"claude-1","message":{"id":"msg-1","usage":{"input_tokens":5,"output_tokens":3}},"stop_reason":"end_turn"}`,
	}, "\n"))

	events, err := ParseClaudeJSONL(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseClaudeJSONL: %v", err)
	}
	if len(events) != 1 || events[0].OutputTokens != 3 {
		t.Fatalf("events = %+v", events)
	}
}
