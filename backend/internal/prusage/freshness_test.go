package prusage

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
)

func TestEvaluateLoadedPRFreshness(t *testing.T) {
	refreshedAt := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)
	olderObserved := refreshedAt.Add(-time.Minute)
	newerObserved := refreshedAt.Add(time.Minute)
	checkedAt := time.Date(2026, time.July, 15, 18, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	checkpointID := func(id int) *int { return &id }
	snapshot := func(id, sortOrder int, sha string, checkpointID *int) *ent.PRCommitUsageSnapshot {
		return &ent.PRCommitUsageSnapshot{
			ID:                 id,
			CommitSha:          sha,
			CommitCheckpointID: checkpointID,
			SortOrder:          sortOrder,
		}
	}
	commit := func(sha string, status UsageStatus, reason string, checkpointFound, usageEventFound bool) CommitFreshness {
		return CommitFreshness{
			CommitSHA:       sha,
			Status:          status,
			Reason:          reason,
			CheckpointFound: checkpointFound,
			UsageEventFound: usageEventFound,
		}
	}

	tests := []struct {
		name              string
		usageRefreshedAt  *time.Time
		snapshots         []*ent.PRCommitUsageSnapshot
		pendingUnbound    bool
		usageByCheckpoint map[int]checkpointUsageFact
		wantStatus        UsageStatus
		wantReason        string
		wantCommits       []CommitFreshness
	}{
		{
			name:             "first commit no checkpoint wins over later no usage events",
			usageRefreshedAt: &refreshedAt,
			snapshots: []*ent.PRCommitUsageSnapshot{
				snapshot(30, 2, "later-no-usage", checkpointID(303)),
				snapshot(20, 1, "middle-fresh", checkpointID(202)),
				snapshot(10, 1, "first-no-checkpoint", nil),
			},
			usageByCheckpoint: map[int]checkpointUsageFact{
				202: {Count: 1, LatestObserved: &olderObserved},
				303: {Count: 0},
			},
			wantStatus: UsageStatusNoCheckpoint,
			wantReason: "No checkpoint matched this PR commit.",
			wantCommits: []CommitFreshness{
				commit("first-no-checkpoint", UsageStatusNoCheckpoint, "No checkpoint matched this PR commit.", false, false),
				commit("middle-fresh", UsageStatusFresh, "Usage events were included.", true, true),
				commit("later-no-usage", UsageStatusNoUsageEvents, "Checkpoint exists but no usage events are bound to it.", true, false),
			},
		},
		{
			name:             "first commit no usage events wins over later stale snapshot",
			usageRefreshedAt: &refreshedAt,
			snapshots: []*ent.PRCommitUsageSnapshot{
				snapshot(20, 1, "later-stale", checkpointID(202)),
				snapshot(10, 1, "first-no-usage", checkpointID(101)),
			},
			usageByCheckpoint: map[int]checkpointUsageFact{
				101: {Count: 0},
				202: {Count: 1, LatestObserved: &newerObserved},
			},
			wantStatus: UsageStatusNoUsageEvents,
			wantReason: "Checkpoint exists but no usage events are bound to it.",
			wantCommits: []CommitFreshness{
				commit("first-no-usage", UsageStatusNoUsageEvents, "Checkpoint exists but no usage events are bound to it.", true, false),
				commit("later-stale", UsageStatusStaleSnapshot, "Usage events newer than the PR snapshot are bound to this checkpoint.", true, true),
			},
		},
		{
			name:             "first commit stale snapshot wins over later no checkpoint",
			usageRefreshedAt: &refreshedAt,
			snapshots: []*ent.PRCommitUsageSnapshot{
				snapshot(20, 1, "later-no-checkpoint", nil),
				snapshot(10, 1, "first-stale", checkpointID(101)),
			},
			usageByCheckpoint: map[int]checkpointUsageFact{
				101: {Count: 1, LatestObserved: &newerObserved},
			},
			wantStatus: UsageStatusStaleSnapshot,
			wantReason: "Usage events newer than the PR snapshot are bound to this checkpoint.",
			wantCommits: []CommitFreshness{
				commit("first-stale", UsageStatusStaleSnapshot, "Usage events newer than the PR snapshot are bound to this checkpoint.", true, true),
				commit("later-no-checkpoint", UsageStatusNoCheckpoint, "No checkpoint matched this PR commit.", false, false),
			},
		},
		{
			name:             "all commits with included usage are fresh",
			usageRefreshedAt: &refreshedAt,
			snapshots: []*ent.PRCommitUsageSnapshot{
				snapshot(30, 2, "third-fresh", checkpointID(303)),
				snapshot(20, 1, "second-fresh", checkpointID(202)),
				snapshot(10, 1, "first-fresh", checkpointID(101)),
			},
			usageByCheckpoint: map[int]checkpointUsageFact{
				101: {Count: 1, LatestObserved: &olderObserved},
				202: {Count: 1, LatestObserved: &olderObserved},
				303: {Count: 1, LatestObserved: &olderObserved},
			},
			wantStatus: UsageStatusFresh,
			wantReason: "Usage snapshot is current.",
			wantCommits: []CommitFreshness{
				commit("first-fresh", UsageStatusFresh, "Usage events were included.", true, true),
				commit("second-fresh", UsageStatusFresh, "Usage events were included.", true, true),
				commit("third-fresh", UsageStatusFresh, "Usage events were included.", true, true),
			},
		},
		{
			name:             "pending repo evidence wins when there are no snapshots",
			usageRefreshedAt: &refreshedAt,
			pendingUnbound:   true,
			wantStatus:       UsageStatusPendingUpload,
			wantReason:       "Unbound usage events exist for this repo and may still be waiting for checkpoint binding.",
		},
		{
			name:       "never refreshed without snapshots has no checkpoint",
			wantStatus: UsageStatusNoCheckpoint,
			wantReason: "No PR commit snapshot has been generated yet.",
		},
		{
			name:             "refreshed without snapshot rows has no checkpoint",
			usageRefreshedAt: &refreshedAt,
			wantStatus:       UsageStatusNoCheckpoint,
			wantReason:       "Snapshot refresh ran but no PR commit rows were recorded.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputIDs := make([]int, len(tt.snapshots))
			for i, item := range tt.snapshots {
				inputIDs[i] = item.ID
			}

			got := evaluateLoadedPRFreshness(
				&ent.PrRecord{UsageRefreshedAt: tt.usageRefreshedAt},
				tt.snapshots,
				tt.pendingUnbound,
				tt.usageByCheckpoint,
				checkedAt,
			)

			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if !got.CheckedAt.Equal(checkedAt) || got.CheckedAt.Location() != time.UTC {
				t.Fatalf("checked_at = %v (%v), want same instant in UTC", got.CheckedAt, got.CheckedAt.Location())
			}
			if !reflect.DeepEqual(got.Commits, tt.wantCommits) {
				t.Fatalf("commits = %+v, want %+v", got.Commits, tt.wantCommits)
			}

			gotInputIDs := make([]int, len(tt.snapshots))
			for i, item := range tt.snapshots {
				gotInputIDs[i] = item.ID
			}
			if !reflect.DeepEqual(gotInputIDs, inputIDs) {
				t.Fatalf("input snapshot order = %v, want unchanged %v", gotInputIDs, inputIDs)
			}
		})
	}
}

func TestEvaluatePRFreshnessNoSnapshots(t *testing.T) {
	tests := []struct {
		name       string
		refreshed  bool
		pending    bool
		wantStatus UsageStatus
		wantReason string
	}{
		{
			name:       "pending repo evidence takes precedence",
			refreshed:  true,
			pending:    true,
			wantStatus: UsageStatusPendingUpload,
			wantReason: "Unbound usage events exist for this repo and may still be waiting for checkpoint binding.",
		},
		{
			name:       "never refreshed",
			wantStatus: UsageStatusNoCheckpoint,
			wantReason: "No PR commit snapshot has been generated yet.",
		},
		{
			name:       "refreshed without rows",
			refreshed:  true,
			wantStatus: UsageStatusNoCheckpoint,
			wantReason: "Snapshot refresh ran but no PR commit rows were recorded.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, repo, pr, userID := newTestRepoPR(t)
			defer client.Close()
			ctx := context.Background()
			refreshedAt := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)

			if tt.refreshed {
				client.PrRecord.UpdateOneID(pr.ID).
					SetUsageRefreshedAt(refreshedAt).
					ExecX(ctx)
			}
			if tt.pending {
				client.ToolUsageEvent.Create().
					SetTool("codex").
					SetWorkspaceID("ws-no-snapshots").
					SetRepoConfigID(repo.ID).
					SetUserID(userID).
					SetToolSessionID("session-no-snapshots").
					SetUsageUnit("token").
					SetInputTokens(42).
					SetOutputTokens(7).
					SetObservedStartAt(refreshedAt.Add(-2 * time.Minute)).
					SetObservedEndAt(refreshedAt.Add(-time.Minute)).
					SetDedupeKey("pending-no-snapshots").
					SaveX(ctx)
			}

			got, err := NewService(client).EvaluatePRFreshness(ctx, pr.ID)
			if err != nil {
				t.Fatalf("EvaluatePRFreshness error: %v", err)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.CheckedAt.Location() != time.UTC {
				t.Fatalf("checked_at location = %v, want UTC", got.CheckedAt.Location())
			}
			if len(got.Commits) != 0 {
				t.Fatalf("commits = %+v, want empty", got.Commits)
			}
		})
	}
}

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
