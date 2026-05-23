package attributionlocal

import (
	"context"
	"errors"
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

func TestFindCodexWorkspaceSessionIDsHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", `{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := findCodexWorkspaceSessionIDsContext(ctx, path, "/tmp/repo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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

func TestParseCodexJSONL_EmitsDistinctEventsWithoutResponseID(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "codex.jsonl", strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/tmp/repo"}}`,
		`{"type":"event_msg","timestamp":"2026-05-20T13:09:02.118Z","payload":{"type":"token_count","info":null}}`,
		`{"type":"event_msg","timestamp":"2026-05-20T13:09:10.936Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":24342,"cached_input_tokens":2432,"output_tokens":410,"reasoning_output_tokens":211,"total_tokens":24752}}}}`,
		`{"type":"event_msg","timestamp":"2026-05-20T13:09:20.111Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":30798,"cached_input_tokens":23936,"output_tokens":390,"reasoning_output_tokens":172,"total_tokens":31188}}}}`,
		`{"type":"event_msg","timestamp":"2026-05-20T13:09:37.056Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":37059,"cached_input_tokens":30592,"output_tokens":750,"reasoning_output_tokens":516,"total_tokens":37809}}}}`,
	}, "\n"))

	events, err := ParseCodexJSONLFallback(path, "/tmp/repo")
	if err != nil {
		t.Fatalf("ParseCodexJSONLFallback: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	for idx, ev := range events {
		if ev.ToolEventID == "" {
			t.Fatalf("events[%d] missing tool event id: %+v", idx, ev)
		}
		if ev.DedupeKey == "" {
			t.Fatalf("events[%d] missing dedupe key: %+v", idx, ev)
		}
	}
	if events[0].ToolEventID == events[1].ToolEventID || events[1].ToolEventID == events[2].ToolEventID || events[0].ToolEventID == events[2].ToolEventID {
		t.Fatalf("tool event ids should differ: %+v", events)
	}
	if events[0].DedupeKey == events[1].DedupeKey || events[1].DedupeKey == events[2].DedupeKey || events[0].DedupeKey == events[2].DedupeKey {
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
