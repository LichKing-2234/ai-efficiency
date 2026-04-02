package sessionevent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/sessionevent"
	_ "github.com/mattn/go-sqlite3"
)

func TestCreate_CreatesSessionEvent(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	svc := NewService(client)

	err := svc.Create(context.Background(), CreateSessionEventRequest{
		EventID:     "event-1",
		SessionID:   "11111111-1111-1111-1111-111111111111",
		WorkspaceID: "ws-1",
		EventType:   "user_prompt_submit",
		Source:      "codex_hook",
		CapturedAt:  time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		RawPayload: map[string]any{
			"prompt": "explain the diff",
		},
	})
	if err != nil {
		t.Fatalf("create session event: %v", err)
	}

	ev := client.SessionEvent.Query().
		Where(sessionevent.EventIDEQ("event-1")).
		OnlyX(context.Background())

	if ev.EventType != "user_prompt_submit" {
		t.Fatalf("event_type = %q, want %q", ev.EventType, "user_prompt_submit")
	}
	if ev.Source != "codex_hook" {
		t.Fatalf("source = %q, want %q", ev.Source, "codex_hook")
	}
}

func TestCreate_ReturnsErrorForInvalidSessionID(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	svc := NewService(client)

	err := svc.Create(context.Background(), CreateSessionEventRequest{
		EventID:     "event-2",
		SessionID:   "not-a-uuid",
		WorkspaceID: "ws-1",
		EventType:   "user_prompt_submit",
		Source:      "codex_hook",
		CapturedAt:  time.Now(),
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse session_id") {
		t.Fatalf("error = %q, want parse session_id", err.Error())
	}
}
