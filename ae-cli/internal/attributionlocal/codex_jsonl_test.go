package attributionlocal

import (
	"strings"
	"testing"
)

func TestParseCodexJSONL_PrefersLastTokenUsage(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120},"last_token_usage":{"input_tokens":7,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":9}}}}`,
	}, "\n"))

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 1 || events[0].InputTokens != 7 || events[0].OutputTokens != 2 {
		t.Fatalf("events = %+v", events)
	}
}
