package prusage

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
)

func TestEvaluateCommitFreshnessNoCheckpoint(t *testing.T) {
	client, _, pr, _ := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusNoCheckpoint {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusNoCheckpoint)
	}
}

func TestEvaluateCommitFreshnessNoUsageEvents(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	cp := client.CommitCheckpoint.Create().
		SetEventID("fresh-cp").
		SetUserID(userID).
		SetWorkspaceID("ws-fresh").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("abc123").
		SetParentShas([]string{"base"}).
		SetCapturedAt(time.Now().UTC()).
		SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("abc123").
		SetCommitCheckpointID(cp.ID).
		SetSortOrder(0).
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusNoUsageEvents {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusNoUsageEvents)
	}
	if len(status.Commits) != 1 || status.Commits[0].Status != UsageStatusNoUsageEvents {
		t.Fatalf("commits = %+v, want no_usage_events", status.Commits)
	}
}

func TestEvaluateCommitFreshnessPendingUploadWhenUnboundUsageExists(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-pending").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("session-pending").
		SetUsageUnit("token").
		SetInputTokens(42).
		SetOutputTokens(7).
		SetObservedStartAt(now.Add(-2 * time.Minute)).
		SetObservedEndAt(now.Add(-1 * time.Minute)).
		SetDedupeKey("pending-upload-1").
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusPendingUpload {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusPendingUpload)
	}
}

func TestEvaluateCommitFreshnessStaleSnapshotWhenNewUsageArrives(t *testing.T) {
	client, repo, pr, userID := newTestRepoPR(t)
	defer client.Close()
	ctx := context.Background()
	refreshedAt := time.Now().Add(-30 * time.Minute).UTC()
	cp := client.CommitCheckpoint.Create().
		SetEventID("stale-cp").
		SetUserID(userID).
		SetWorkspaceID("ws-stale").
		SetRepoConfigID(repo.ID).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SetCommitSha("stale-sha").
		SetParentShas([]string{"base"}).
		SetCapturedAt(refreshedAt.Add(-5 * time.Minute)).
		SaveX(ctx)
	client.PrRecord.UpdateOneID(pr.ID).
		SetUsageRefreshedAt(refreshedAt).
		ExecX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("stale-sha").
		SetCommitCheckpointID(cp.ID).
		SetInputTokens(1).
		SetSortOrder(0).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-stale").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("session-stale").
		SetUsageUnit("token").
		SetInputTokens(9).
		SetObservedStartAt(refreshedAt.Add(5 * time.Minute)).
		SetObservedEndAt(refreshedAt.Add(6 * time.Minute)).
		SetDedupeKey("stale-usage-1").
		SetCommitCheckpointID(cp.ID).
		SaveX(ctx)

	status, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
	if err != nil {
		t.Fatalf("EvaluatePRFreshness error: %v", err)
	}
	if status.Status != UsageStatusStaleSnapshot {
		t.Fatalf("status = %s, want %s", status.Status, UsageStatusStaleSnapshot)
	}
}
