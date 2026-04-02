package sessionevent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionevent"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestCreate_CreatesSessionEvent(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	ctx := context.Background()
	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sess := client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("main").
		SaveX(ctx)

	svc := NewService(client)

	err := svc.Create(ctx, CreateSessionEventRequest{
		EventID:     "event-1",
		SessionID:   sess.ID.String(),
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
		OnlyX(ctx)

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

func TestCreate_LinksSessionEventToSessionEdge(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	ctx := context.Background()

	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sess := client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("main").
		SaveX(ctx)

	svc := NewService(client)
	err := svc.Create(ctx, CreateSessionEventRequest{
		EventID:     "event-edge-1",
		SessionID:   sess.ID.String(),
		WorkspaceID: "ws-1",
		EventType:   "user_prompt_submit",
		Source:      "codex_hook",
		CapturedAt:  time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create session event: %v", err)
	}

	s := client.Session.Query().
		Where(session.IDEQ(sess.ID)).
		WithSessionEvents().
		OnlyX(ctx)
	if len(s.Edges.SessionEvents) != 1 {
		t.Fatalf("session_events = %d, want 1", len(s.Edges.SessionEvents))
	}
}
