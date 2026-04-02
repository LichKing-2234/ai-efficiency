package sessionusage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionusageevent"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestCreate_CreatesUsageEvent(t *testing.T) {
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

	err := svc.Create(ctx, CreateUsageEventRequest{
		EventID:      "usage-evt-1",
		SessionID:    sess.ID.String(),
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
		RawMetadata: map[string]any{
			"billing_mode": "postpaid",
		},
	})
	if err != nil {
		t.Fatalf("create usage event: %v", err)
	}

	ev := client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-evt-1")).
		OnlyX(ctx)

	if ev.WorkspaceID != "ws-1" {
		t.Fatalf("workspace_id = %q, want %q", ev.WorkspaceID, "ws-1")
	}
	if ev.TotalTokens != 140 {
		t.Fatalf("total_tokens = %d, want 140", ev.TotalTokens)
	}
}

func TestCreate_ReturnsErrorForInvalidSessionID(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	svc := NewService(client)

	err := svc.Create(context.Background(), CreateUsageEventRequest{
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

func TestCreate_LinksUsageEventToSessionEdge(t *testing.T) {
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
	err := svc.Create(ctx, CreateUsageEventRequest{
		EventID:      "usage-evt-edge-1",
		SessionID:    sess.ID.String(),
		WorkspaceID:  "ws-1",
		RequestID:    "req-1",
		ProviderName: "sub2api",
		Model:        "gpt-5.4",
		StartedAt:    time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
		FinishedAt:   time.Date(2026, 4, 2, 10, 0, 3, 0, time.UTC),
		Status:       "completed",
	})
	if err != nil {
		t.Fatalf("create usage event: %v", err)
	}

	s := client.Session.Query().
		Where(session.IDEQ(sess.ID)).
		WithSessionUsageEvents().
		OnlyX(ctx)
	if len(s.Edges.SessionUsageEvents) != 1 {
		t.Fatalf("session_usage_events = %d, want 1", len(s.Edges.SessionUsageEvents))
	}
}
