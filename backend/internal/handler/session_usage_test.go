package handler

import (
	"net/http"
	"testing"
)

func TestSessionUsageIngest_CreatesUsageEvent(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-1",
		"session_id":    "11111111-1111-1111-1111-111111111111",
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"input_tokens":  100,
		"output_tokens": 40,
		"total_tokens":  140,
		"status":        "completed",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestSessionEventIngest_CreatesPromptEvent(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-events", map[string]any{
		"event_id":     "event-1",
		"session_id":   "11111111-1111-1111-1111-111111111111",
		"workspace_id": "ws-1",
		"event_type":   "user_prompt_submit",
		"source":       "codex_hook",
		"captured_at":  "2026-04-02T10:00:00Z",
		"raw_payload":  map[string]any{"prompt": "explain the diff"},
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}
