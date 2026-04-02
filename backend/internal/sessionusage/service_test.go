package sessionusage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionusageevent"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func createOwnedSession(t *testing.T) (*ent.Client, context.Context, uuid.UUID, int, int) {
	t.Helper()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
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

func validUsageReq(sessionID uuid.UUID, eventID string) CreateUsageEventRequest {
	return CreateUsageEventRequest{
		EventID:      eventID,
		SessionID:    sessionID.String(),
		WorkspaceID:  "ws-1",
		RequestID:    "req-1",
		ProviderName: "sub2api",
		Model:        "gpt-5.4",
		StartedAt:    time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		FinishedAt:   time.Date(2026, 4, 2, 10, 0, 3, 0, time.UTC),
		InputTokens:  100,
		OutputTokens: 40,
		TotalTokens:  140,
		Status:       "completed",
	}
}

func TestCreate_CreatesUsageEvent(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	err := svc.Create(ctx, ownerID, validUsageReq(sessionID, "usage-evt-1"))
	if err != nil {
		t.Fatalf("create usage event: %v", err)
	}

	ev := client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-evt-1")).
		OnlyX(ctx)
	if ev.WorkspaceID != "ws-1" {
		t.Fatalf("workspace_id = %q, want ws-1", ev.WorkspaceID)
	}
}

func TestCreate_IsIdempotentByEventID(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)
	req := validUsageReq(sessionID, "usage-evt-dup-1")

	if err := svc.Create(ctx, ownerID, req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := svc.Create(ctx, ownerID, req); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}

	count := client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-evt-dup-1")).
		CountX(ctx)
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}

func TestCreate_ReturnsErrorForInvalidSessionID(t *testing.T) {
	t.Parallel()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	svc := NewService(client)

	err := svc.Create(context.Background(), 1, CreateUsageEventRequest{
		EventID:      "usage-evt-2",
		SessionID:    "not-a-uuid",
		WorkspaceID:  "ws-1",
		RequestID:    "req-1",
		ProviderName: "sub2api",
		Model:        "gpt-5.4",
		StartedAt:    time.Now(),
		FinishedAt:   time.Now(),
		Status:       "completed",
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

	err := svc.Create(ctx, otherID, validUsageReq(sessionID, "usage-evt-cross-user"))
	if !errors.Is(err, ErrSessionForbidden) {
		t.Fatalf("error = %v, want ErrSessionForbidden", err)
	}
}

func TestCreate_RejectsMissingSession(t *testing.T) {
	t.Parallel()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	svc := NewService(client)

	err := svc.Create(context.Background(), 1, validUsageReq(uuid.New(), "usage-evt-missing-session"))
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestCreate_RejectsInvalidPayload(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	req := validUsageReq(sessionID, "usage-evt-invalid")
	req.EventID = "   "
	err := svc.Create(ctx, ownerID, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("event_id whitespace error = %v, want ErrInvalidRequest", err)
	}

	req = validUsageReq(sessionID, "usage-evt-invalid-2")
	req.InputTokens = -1
	err = svc.Create(ctx, ownerID, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative token error = %v, want ErrInvalidRequest", err)
	}

	req = validUsageReq(sessionID, "usage-evt-invalid-3")
	req.FinishedAt = req.StartedAt.Add(-1 * time.Second)
	err = svc.Create(ctx, ownerID, req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("time order error = %v, want ErrInvalidRequest", err)
	}
}

func TestCreate_LinksUsageEventToSessionEdge(t *testing.T) {
	t.Parallel()
	client, ctx, sessionID, ownerID, _ := createOwnedSession(t)
	svc := NewService(client)

	err := svc.Create(ctx, ownerID, validUsageReq(sessionID, "usage-evt-edge-1"))
	if err != nil {
		t.Fatalf("create usage event: %v", err)
	}

	s := client.Session.Query().
		Where(session.IDEQ(sessionID)).
		WithSessionUsageEvents().
		OnlyX(ctx)
	if len(s.Edges.SessionUsageEvents) != 1 {
		t.Fatalf("session_usage_events = %d, want 1", len(s.Edges.SessionUsageEvents))
	}
}
