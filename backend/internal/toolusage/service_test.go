package toolusage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestCreateUsageEvent_DedupesByDedupeKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client)

	scope := seedToolUsageScope(t, client)
	client.CommitCheckpoint.Create().
		SetEventID("cp-seed-scope").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("seed-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource("unbound").
		SetCapturedAt(time.Unix(110, 0).UTC()).
		SaveX(ctx)

	req := CreateUsageEventRequest{
		Tool:            "codex",
		WorkspaceID:     scope.WorkspaceID,
		ToolSessionID:   "codex-sess-1",
		ToolEventID:     "resp-1",
		DedupeKey:       "codex:codex-sess-1:resp-1",
		UsageUnit:       "token",
		InputTokens:     10,
		OutputTokens:    5,
		ObservedStartAt: time.Unix(120, 0).UTC(),
		ObservedEndAt:   time.Unix(121, 0).UTC(),
	}

	if err := svc.CreateUsageEvent(ctx, scope.UserID, req); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := svc.CreateUsageEvent(ctx, scope.UserID, req); err != nil {
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
	client := testdb.Open(t)
	seed := createToolUsageBindingFixture(t, client)
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

func TestBindUsageEventsToCheckpoint_DoesNotCrossRepoScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	seed := createToolUsageBindingFixture(t, client)
	svc := NewService(client)

	otherScope := seedToolUsageScope(t, client)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(seed.WorkspaceID).
		SetRepoConfigID(otherScope.RepoConfigID).
		SetUserID(otherScope.UserID).
		SetToolSessionID("other-sess").
		SetToolEventID("other-resp").
		SetDedupeKey("other:cross-repo").
		SetUsageUnit("token").
		SetObservedStartAt(seed.PreviousCapturedAt.Add(3 * time.Second)).
		SetObservedEndAt(seed.PreviousCapturedAt.Add(4 * time.Second)).
		SaveX(ctx)

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
}

func TestCreateUsageEvent_RejectsCrossUserScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client)

	scope := seedToolUsageScope(t, client)
	client.CommitCheckpoint.Create().
		SetEventID("cp-cross-user-scope").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("seed-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource("unbound").
		SetCapturedAt(time.Unix(110, 0).UTC()).
		SaveX(ctx)

	req := CreateUsageEventRequest{
		Tool:            "codex",
		WorkspaceID:     scope.WorkspaceID,
		ToolSessionID:   "codex-sess-1",
		ToolEventID:     "resp-cross-user-1",
		DedupeKey:       "codex:codex-sess-1:resp-cross-user-1",
		UsageUnit:       "token",
		InputTokens:     10,
		OutputTokens:    5,
		ObservedStartAt: time.Unix(120, 0).UTC(),
		ObservedEndAt:   time.Unix(121, 0).UTC(),
	}

	err := svc.CreateUsageEvent(ctx, scope.UserID+999, req)
	if err == nil {
		t.Fatal("expected cross-user scope validation to fail")
	}
	if !errors.Is(err, ErrUsageEventForbidden) {
		t.Fatalf("err = %v, want %v", err, ErrUsageEventForbidden)
	}
}

func TestCreateUsageEvent_AutoBindsToLatestCheckpointWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client)

	scope := seedToolUsageScope(t, client)
	prev := client.CommitCheckpoint.Create().
		SetEventID("cp-auto-bind-prev").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("base-sha").
		SetParentShas([]string{"base-parent"}).
		SetBindingSource("unbound").
		SetCapturedAt(time.Unix(110, 0).UTC()).
		SaveX(ctx)
	current := client.CommitCheckpoint.Create().
		SetEventID("cp-auto-bind-current").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("head-sha").
		SetParentShas([]string{prev.CommitSha}).
		SetBindingSource("unbound").
		SetCapturedAt(time.Unix(140, 0).UTC()).
		SaveX(ctx)

	req := CreateUsageEventRequest{
		Tool:              "codex",
		WorkspaceID:       scope.WorkspaceID,
		ToolSessionID:     "codex-sess-auto-bind",
		ToolEventID:       "resp-auto-bind",
		DedupeKey:         "codex:codex-sess-auto-bind:resp-auto-bind",
		UsageUnit:         "token",
		InputTokens:       21,
		OutputTokens:      8,
		CachedInputTokens: 3,
		ObservedStartAt:   time.Unix(120, 0).UTC(),
		ObservedEndAt:     time.Unix(121, 0).UTC(),
	}

	if err := svc.CreateUsageEvent(ctx, scope.UserID, req); err != nil {
		t.Fatalf("CreateUsageEvent: %v", err)
	}

	row := client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ(req.DedupeKey)).
		OnlyX(ctx)
	if row.CommitCheckpointID == nil || *row.CommitCheckpointID != current.ID {
		t.Fatalf("commit_checkpoint_id = %v, want %d", row.CommitCheckpointID, current.ID)
	}
}

func TestCreateUsageEventWithRepoConfigIDDoesNotNeedCheckpointScope(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	ctx := context.Background()
	defer client.Close()

	repo := client.RepoConfig.Create().
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://repo-host.example.com/org/demo.git").
		SetDefaultBranch("main").
		SetStatus("active").
		SaveX(ctx)
	userID := client.User.Create().
		SetUsername("tool-user").
		SetEmail("tool-user@example.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	err := NewService(client).CreateUsageEvent(ctx, userID, CreateUsageEventRequest{
		RepoConfigID:     repo.ID,
		Tool:             "codex",
		WorkspaceID:      "ws-direct",
		ToolSessionID:    "codex-sess",
		DedupeKey:        "codex:direct",
		UsageUnit:        "token",
		ObservedStartAt:  time.Unix(100, 0).UTC(),
		ObservedEndAt:    time.Unix(101, 0).UTC(),
		InputTokens:      10,
		OutputTokens:     5,
		RawSourcePath:    "/tmp/should-stay-supported-for-legacy",
		RawSourceLocator: "line:1",
		RawPayload:       map[string]any{"legacy": true},
	})
	if err != nil {
		t.Fatalf("CreateUsageEvent: %v", err)
	}
	row := client.ToolUsageEvent.Query().Where(toolusageevent.DedupeKeyEQ("codex:direct")).OnlyX(ctx)
	if row.RepoConfigID != repo.ID || row.UserID != userID {
		t.Fatalf("scope = repo %d user %d, want repo %d user %d", row.RepoConfigID, row.UserID, repo.ID, userID)
	}
}

func TestCreateUsageEventWithRepoConfigIDIgnoresConflictingWorkspaceScope(t *testing.T) {
	t.Parallel()
	client := testdb.Open(t)
	ctx := context.Background()
	defer client.Close()

	scopeA := seedToolUsageScope(t, client)
	scopeB := seedToolUsageScope(t, client)
	userID := scopeB.UserID

	err := NewService(client).CreateUsageEvent(ctx, userID, CreateUsageEventRequest{
		RepoConfigID:    scopeB.RepoConfigID,
		Tool:            "claude",
		WorkspaceID:     scopeA.WorkspaceID,
		ToolSessionID:   "claude-sess",
		DedupeKey:       "claude:override",
		UsageUnit:       "token",
		ObservedStartAt: time.Unix(200, 0).UTC(),
		ObservedEndAt:   time.Unix(201, 0).UTC(),
		InputTokens:     8,
		OutputTokens:    3,
	})
	if err != nil {
		t.Fatalf("CreateUsageEvent: %v", err)
	}
	row := client.ToolUsageEvent.Query().Where(toolusageevent.DedupeKeyEQ("claude:override")).OnlyX(ctx)
	if row.RepoConfigID != scopeB.RepoConfigID || row.UserID != scopeB.UserID {
		t.Fatalf("scope = repo %d user %d, want repo %d user %d", row.RepoConfigID, row.UserID, scopeB.RepoConfigID, scopeB.UserID)
	}
}
