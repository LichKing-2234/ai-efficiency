package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeHookEventCodexUserPromptSubmit(t *testing.T) {
	capturedAt := time.Date(2026, 4, 3, 15, 4, 5, 0, time.UTC)
	event, err := NormalizeHookEvent("codex", map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-session-1",
		"prompt":          "explain this diff",
	}, HookForwardOptions{
		WorkspaceID: "ws-codex-1",
		CapturedAt:  capturedAt,
	})
	if err != nil {
		t.Fatalf("NormalizeHookEvent: %v", err)
	}

	if event.EventType != "user_prompt_submit" {
		t.Fatalf("event_type = %q, want %q", event.EventType, "user_prompt_submit")
	}
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", event.Payload)
	}
	if payload["source"] != "codex_hook" {
		t.Fatalf("source = %#v, want %q", payload["source"], "codex_hook")
	}
	if payload["workspace_id"] != "ws-codex-1" {
		t.Fatalf("workspace_id = %#v, want %q", payload["workspace_id"], "ws-codex-1")
	}
	if payload["captured_at"] != "2026-04-03T15:04:05Z" {
		t.Fatalf("captured_at = %#v, want %q", payload["captured_at"], "2026-04-03T15:04:05Z")
	}
	if payload["prompt"] != "explain this diff" {
		t.Fatalf("prompt = %#v, want %q", payload["prompt"], "explain this diff")
	}
}

func TestForwardHookEventPostsNormalizedPayloadToLocalProxy(t *testing.T) {
	capturedAt := time.Date(2026, 4, 3, 16, 0, 0, 0, time.UTC)
	var got EventEnvelope
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session-events" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/v1/session-events")
		}
		if gotAuth := strings.TrimSpace(r.Header.Get("Authorization")); gotAuth != "Bearer proxy-token" {
			t.Fatalf("auth = %q, want %q", gotAuth, "Bearer proxy-token")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode event envelope: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer proxyServer.Close()

	err := ForwardHookEvent(context.Background(), strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"}}`), HookForwardRequest{
		Tool:            "claude",
		LocalProxyURL:   proxyServer.URL,
		LocalProxyToken: "proxy-token",
		WorkspaceID:     "ws-claude-1",
		CapturedAt:      capturedAt,
	})
	if err != nil {
		t.Fatalf("ForwardHookEvent: %v", err)
	}

	if got.EventType != "pre_tool_use" {
		t.Fatalf("event_type = %q, want %q", got.EventType, "pre_tool_use")
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", got.Payload)
	}
	if payload["source"] != "claude_hook" {
		t.Fatalf("source = %#v, want %q", payload["source"], "claude_hook")
	}
	if payload["workspace_id"] != "ws-claude-1" {
		t.Fatalf("workspace_id = %#v, want %q", payload["workspace_id"], "ws-claude-1")
	}
	toolInput, ok := payload["tool_input"].(map[string]any)
	if !ok {
		t.Fatalf("tool_input type = %T, want map[string]any", payload["tool_input"])
	}
	if toolInput["command"] != "git status" {
		t.Fatalf("tool_input.command = %#v, want %q", toolInput["command"], "git status")
	}
}

func TestForwardHookEventIsFailOpenWhenProxyDeliveryFails(t *testing.T) {
	err := ForwardHookEvent(context.Background(), strings.NewReader(`{"hook_event_name":"Stop"}`), HookForwardRequest{
		Tool:            "codex",
		LocalProxyURL:   "http://127.0.0.1:1",
		LocalProxyToken: "proxy-token",
		WorkspaceID:     "ws-codex-2",
		CapturedAt:      time.Date(2026, 4, 3, 16, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ForwardHookEvent should fail open, got err=%v", err)
	}
}

func TestForwardHookEventSpoolsWhenProxyDeliveryFailsAndSessionIsKnown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := ForwardHookEvent(context.Background(), strings.NewReader(`{"hook_event_name":"Stop"}`), HookForwardRequest{
		Tool:            "codex",
		LocalProxyURL:   "http://127.0.0.1:1",
		LocalProxyToken: "proxy-token",
		SessionID:       "sess-proxy-fallback",
		WorkspaceID:     "ws-codex-3",
		CapturedAt:      time.Date(2026, 4, 3, 16, 6, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ForwardHookEvent should fail open, got err=%v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".ae-cli", "runtime", "sess-proxy-fallback", "queue", "proxy-session-events.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile spool: %v", err)
	}
	if !strings.Contains(string(data), `"event_type":"stop"`) {
		t.Fatalf("expected stop event spooled, got %s", string(data))
	}
}
