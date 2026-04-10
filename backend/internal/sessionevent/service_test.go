package sessionevent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionevent"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
)

func createOwnedSession(t *testing.T) (*ent.Client, context.Context, uuid.UUID, int, int) {
	t.Helper()
	client := testdb.Open(t)
	ctx := context.Background()

	owner := client.User.Create().
		SetUsername("owner").
		SetEmail("owner@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)
	other := client.User.Create().
		SetUsername("other").
		SetEmail("other@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)

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
		SetUserID(owner.ID).
		SaveX(ctx)

	return client, ctx, sess.ID, owner.ID, other.ID
}

func validSessionEventReq(sessionID uuid.UUID, eventID string) CreateSessionEventRequest {
	return CreateSessionEventRequest{
		EventID:     eventID,
		SessionID:   sessionID.String(),
		WorkspaceID: "ws-1",
		EventType:   "user_prompt_submit",
		Source:      "codex_hook",
		CapturedAt:  time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
	}
}

func TestCreate_CreatesSessionEvent(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	req := validSessionEventReq(sessionID, "event-1")
	req.RawPayload = map[string]any{"prompt": "explain the diff"}
	err := svc.Create(ctx, ownerID, req)
	if err != nil {
		t.Fatalf("create session event: %v", err)
	}

	ev := client.SessionEvent.Query().
		Where(sessionevent.EventIDEQ("event-1")).
		OnlyX(ctx)
	if ev.EventType != "user_prompt_submit" {
		t.Fatalf("event_type = %q, want user_prompt_submit", ev.EventType)
	}
}

func TestCreate_IsIdempotentByEventID(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)
	req := validSessionEventReq(sessionID, "event-dup-1")

	if err := svc.Create(ctx, ownerID, req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := svc.Create(ctx, ownerID, req); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}

	count := client.SessionEvent.Query().
		Where(sessionevent.EventIDEQ("event-dup-1")).
		CountX(ctx)
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}

func TestCreate_ReturnsErrorForInvalidSessionID(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	svc := NewService(client)

	err := svc.Create(context.Background(), 1, CreateSessionEventRequest{
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

func TestCreate_RejectsCrossUserSession(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, _, otherID := createOwnedSession(t)
	svc := NewService(client)

	err := svc.Create(ctx, otherID, validSessionEventReq(sessionID, "event-cross-user"))
	if !errors.Is(err, ErrSessionForbidden) {
		t.Fatalf("error = %v, want ErrSessionForbidden", err)
	}
}

func TestCreate_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	svc := NewService(client)

	err := svc.Create(context.Background(), 1, validSessionEventReq(uuid.New(), "event-missing-session"))
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestCreate_RejectsInvalidPayload(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	req := validSessionEventReq(sessionID, "event-invalid")
	req.EventType = " "
	err := svc.Create(ctx, ownerID, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("event_type whitespace error = %v, want ErrInvalidRequest", err)
	}

	req = validSessionEventReq(sessionID, "event-invalid-2")
	req.Source = " "
	err = svc.Create(ctx, ownerID, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("source whitespace error = %v, want ErrInvalidRequest", err)
	}
}

func TestCreate_LinksSessionEventToSessionEdge(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	err := svc.Create(ctx, ownerID, validSessionEventReq(sessionID, "event-edge-1"))
	if err != nil {
		t.Fatalf("create session event: %v", err)
	}

	s := client.Session.Query().
		Where(session.IDEQ(sessionID)).
		WithSessionEvents().
		OnlyX(ctx)
	if len(s.Edges.SessionEvents) != 1 {
		t.Fatalf("session_events = %d, want 1", len(s.Edges.SessionEvents))
	}
}
