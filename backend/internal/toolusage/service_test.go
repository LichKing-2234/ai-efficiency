package toolusage

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type bindingFixture struct {
	WorkspaceID        string
	CheckpointID       int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

func createToolUsageBindingFixture(t *testing.T) (*ent.Client, bindingFixture) {
	t.Helper()

	ctx := context.Background()
	client := testdb.Open(t)

	scm := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)

	repoCfg := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName("repo").
		SetFullName("org/repo").
		SetCloneURL("https://github.com/org/repo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	prev := time.Unix(150, 0).UTC()
	commitAt := time.Unix(200, 0).UTC()

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repoCfg.ID).
		SetCommitSha("abc123").
		SetParentShas([]string{"parent-1"}).
		SetBindingSource("unbound").
		SetCapturedAt(commitAt).
		SaveX(ctx)

	insert := func(dedupeKey string, endAt time.Time, bound bool) {
		create := client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID("ws-1").
			SetRepoConfigID(repoCfg.ID).
			SetUserID(user.ID).
			SetToolSessionID("codex-sess-1").
			SetToolEventID(dedupeKey).
			SetDedupeKey(dedupeKey).
			SetUsageUnit("token").
			SetInputTokens(10).
			SetOutputTokens(5).
			SetObservedStartAt(endAt.Add(-1 * time.Second)).
			SetObservedEndAt(endAt)
		if bound {
			create.SetCommitCheckpointID(checkpoint.ID)
		}
		create.SaveX(ctx)
	}

	insert("evt-window-1", prev.Add(2*time.Second), false)
	insert("evt-window-2", commitAt.Add(-10*time.Second), false)
	insert("evt-before-window", prev.Add(-10*time.Second), false)
	insert("evt-already-bound", commitAt.Add(-4*time.Second), true)

	return client, bindingFixture{
		WorkspaceID:        "ws-1",
		CheckpointID:       checkpoint.ID,
		CommitCapturedAt:   commitAt,
		PreviousCapturedAt: prev,
	}
}

func TestCreateUsageEvent_DedupesByDedupeKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client)

	scm := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)

	repoCfg := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName("repo").
		SetFullName("org/repo").
		SetCloneURL("https://github.com/org/repo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	req := CreateUsageEventRequest{
		Tool:            "codex",
		WorkspaceID:     "ws-1",
		RepoConfigID:    repoCfg.ID,
		UserID:          user.ID,
		ToolSessionID:   "codex-sess-1",
		ToolEventID:     "resp-1",
		DedupeKey:       "codex:codex-sess-1:resp-1",
		UsageUnit:       "token",
		InputTokens:     10,
		OutputTokens:    5,
		ObservedStartAt: time.Unix(120, 0).UTC(),
		ObservedEndAt:   time.Unix(121, 0).UTC(),
	}

	if err := svc.CreateUsageEvent(ctx, req); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := svc.CreateUsageEvent(ctx, req); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	count := client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ(req.DedupeKey)).
		CountX(ctx)
	if count != 1 {
		t.Fatalf("tool_usage_events count = %d, want 1", count)
	}
}

func TestBindUsageEventsToCheckpoint_BindsOnlyUnboundWindowMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, seed := createToolUsageBindingFixture(t)
	svc := NewService(client)

	bound, err := svc.BindUsageEventsToCheckpoint(ctx, BindUsageEventsRequest{
		WorkspaceID:        seed.WorkspaceID,
		CommitCheckpointID: seed.CheckpointID,
		CommitCapturedAt:   seed.CommitCapturedAt,
		PreviousCapturedAt: seed.PreviousCapturedAt,
	})
	if err != nil {
		t.Fatalf("BindUsageEventsToCheckpoint: %v", err)
	}
	if bound != 2 {
		t.Fatalf("bound count = %d, want 2", bound)
	}

	rows := client.ToolUsageEvent.Query().
		Where(toolusageevent.WorkspaceIDEQ(seed.WorkspaceID)).
		AllX(ctx)

	boundCount := 0
	for _, row := range rows {
		if row.CommitCheckpointID != nil && *row.CommitCheckpointID == seed.CheckpointID {
			boundCount++
		}
	}
	if boundCount != 3 {
		t.Fatalf("bound row count = %d, want 3 including pre-bound row", boundCount)
	}
}
