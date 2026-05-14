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
	ctx = context.Background()
	client.Session.Create().
		SetRepoConfigID(scope.RepoConfigID).
		SetBranch("main").
		SetUserID(scope.UserID).
		SetStartedAt(time.Unix(100, 0).UTC()).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-seed-scope").
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
	client.Session.Create().
		SetRepoConfigID(scope.RepoConfigID).
		SetBranch("main").
		SetUserID(scope.UserID).
		SetStartedAt(time.Unix(100, 0).UTC()).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-cross-user-scope").
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
