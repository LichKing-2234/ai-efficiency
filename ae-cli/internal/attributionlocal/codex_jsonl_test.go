package attributionlocal

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseCodexJSONL_PrefersLastTokenUsage(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","timestamp":"2026-05-19T10:00:00Z","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120},"last_token_usage":{"input_tokens":7,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":9}}}}`,
	}, "\n"))

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 1 || events[0].InputTokens != 7 || events[0].OutputTokens != 2 {
		t.Fatalf("events = %+v", events)
	}
	want := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	if !events[0].ObservedStartAt.Equal(want) || !events[0].ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want %s", events[0].ObservedStartAt, events[0].ObservedEndAt, want)
	}
}

func TestParseCodexJSONL_EmitsMultipleEventsPerFile(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","response_id":"resp-1","info":{"last_token_usage":{"input_tokens":7,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":9}}}}`,
		`{"type":"event_msg","payload":{"type":"token_count","response_id":"resp-2","info":{"last_token_usage":{"input_tokens":9,"cached_input_tokens":2,"output_tokens":3,"reasoning_output_tokens":1,"total_tokens":12}}}}`,
	}, "\n"))

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].DedupeKey == events[1].DedupeKey {
		t.Fatalf("dedupe keys should differ: %+v", events)
	}
}

func TestParseCodexJSONL_PreservesTimestampForLegacyReplayBackfill(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","timestamp":"2026-05-19T10:00:00Z","payload":{"type":"token_count","response_id":"resp-1","info":{"last_token_usage":{"input_tokens":7,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":1,"total_tokens":9}}}}`,
	}, "\n"))
	fileTime := time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, fileTime, fileTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}

	legacy := events[0]
	legacy.ObservedStartAt = time.Time{}
	legacy.ObservedEndAt = time.Time{}
	legacy = normalizeObservedWindow(legacy)

	want := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	if !legacy.ObservedStartAt.Equal(want) || !legacy.ObservedEndAt.Equal(want) {
		t.Fatalf("observed timestamps = %s / %s, want row timestamp %s", legacy.ObservedStartAt, legacy.ObservedEndAt, want)
	}
}
