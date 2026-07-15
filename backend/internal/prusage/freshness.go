package prusage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type UsageStatus string

const (
	UsageStatusFresh         UsageStatus = "fresh"
	UsageStatusPendingUpload UsageStatus = "pending_upload"
	UsageStatusNoCheckpoint  UsageStatus = "no_checkpoint"
	UsageStatusNoUsageEvents UsageStatus = "no_usage_events"
	UsageStatusUnbound       UsageStatus = "unbound"
	UsageStatusStaleSnapshot UsageStatus = "stale_snapshot"
	UsageStatusRefreshFailed UsageStatus = "refresh_failed"
	UsageStatusUnknown       UsageStatus = "unknown"
)

type CommitFreshness struct {
	CommitSHA       string      `json:"commit_sha"`
	Status          UsageStatus `json:"usage_status"`
	Reason          string      `json:"usage_status_reason"`
	CheckpointFound bool        `json:"checkpoint_found"`
	UsageEventFound bool        `json:"usage_event_found"`
}

type PRFreshness struct {
	Status    UsageStatus       `json:"usage_status"`
	Reason    string            `json:"usage_status_reason"`
	CheckedAt time.Time         `json:"usage_status_checked_at"`
	Commits   []CommitFreshness `json:"commits"`
}

type checkpointUsageFact struct {
	Count          int
	LatestObserved *time.Time
}

func (s *Service) EvaluatePRFreshness(ctx context.Context, prID int) (*PRFreshness, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("evaluate PR freshness: ent client is required")
	}
	pr, err := s.entClient.PrRecord.Query().
		Where(prrecord.IDEQ(prID)).
		WithRepoConfig().
		WithPrCommitUsageSnapshots().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate PR freshness: load PR: %w", err)
	}

	checkedAt := time.Now().UTC()
	snapshots := pr.Edges.PrCommitUsageSnapshots
	if len(snapshots) == 0 {
		rc, err := pr.Edges.RepoConfigOrErr()
		if err != nil {
			return nil, fmt.Errorf("evaluate PR freshness: load repo config: %w", err)
		}
		pendingCount, err := s.entClient.ToolUsageEvent.Query().
			Where(
				toolusageevent.RepoConfigIDEQ(rc.ID),
				toolusageevent.CommitCheckpointIDIsNil(),
			).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate PR freshness: count pending usage events: %w", err)
		}
		return evaluateLoadedPRFreshness(pr, snapshots, pendingCount > 0, nil, checkedAt), nil
	}

	usageByCheckpoint := make(map[int]checkpointUsageFact, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.CommitCheckpointID == nil {
			continue
		}

		checkpointID := *snapshot.CommitCheckpointID
		count, err := s.entClient.ToolUsageEvent.Query().
			Where(toolusageevent.CommitCheckpointIDEQ(checkpointID)).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate PR freshness: count usage events: %w", err)
		}
		fact := checkpointUsageFact{Count: count}
		if count > 0 && pr.UsageRefreshedAt != nil {
			newerCount, err := s.entClient.ToolUsageEvent.Query().
				Where(
					toolusageevent.CommitCheckpointIDEQ(checkpointID),
					toolusageevent.ObservedEndAtGT(*pr.UsageRefreshedAt),
				).
				Count(ctx)
			if err != nil {
				return nil, fmt.Errorf("evaluate PR freshness: count newer usage events: %w", err)
			}
			if newerCount > 0 {
				newerObserved := pr.UsageRefreshedAt.Add(time.Nanosecond)
				fact.LatestObserved = &newerObserved
			}
		}
		usageByCheckpoint[checkpointID] = fact
	}

	return evaluateLoadedPRFreshness(pr, snapshots, false, usageByCheckpoint, checkedAt), nil
}

func evaluateLoadedPRFreshness(
	pr *ent.PrRecord,
	snapshots []*ent.PRCommitUsageSnapshot,
	pendingUnbound bool,
	usageByCheckpoint map[int]checkpointUsageFact,
	checkedAt time.Time,
) *PRFreshness {
	checkedAt = checkedAt.UTC()
	if len(snapshots) == 0 {
		if pendingUnbound {
			return &PRFreshness{
				Status:    UsageStatusPendingUpload,
				Reason:    "Unbound usage events exist for this repo and may still be waiting for checkpoint binding.",
				CheckedAt: checkedAt,
			}
		}
		if pr.UsageRefreshedAt == nil {
			return &PRFreshness{
				Status:    UsageStatusNoCheckpoint,
				Reason:    "No PR commit snapshot has been generated yet.",
				CheckedAt: checkedAt,
			}
		}
		return &PRFreshness{
			Status:    UsageStatusNoCheckpoint,
			Reason:    "Snapshot refresh ran but no PR commit rows were recorded.",
			CheckedAt: checkedAt,
		}
	}

	orderedSnapshots := append([]*ent.PRCommitUsageSnapshot(nil), snapshots...)
	sort.Slice(orderedSnapshots, func(i, j int) bool {
		if orderedSnapshots[i].SortOrder != orderedSnapshots[j].SortOrder {
			return orderedSnapshots[i].SortOrder < orderedSnapshots[j].SortOrder
		}
		return orderedSnapshots[i].ID < orderedSnapshots[j].ID
	})

	commits := make([]CommitFreshness, 0, len(orderedSnapshots))
	overall := UsageStatusFresh
	reason := "Usage snapshot is current."
	for _, snapshot := range orderedSnapshots {
		item := CommitFreshness{
			CommitSHA:       snapshot.CommitSha,
			Status:          UsageStatusFresh,
			Reason:          "Usage events were included.",
			CheckpointFound: true,
			UsageEventFound: true,
		}
		if snapshot.CommitCheckpointID == nil {
			item.Status = UsageStatusNoCheckpoint
			item.Reason = "No checkpoint matched this PR commit."
			item.CheckpointFound = false
			item.UsageEventFound = false
		} else {
			fact := usageByCheckpoint[*snapshot.CommitCheckpointID]
			if fact.Count == 0 {
				item.Status = UsageStatusNoUsageEvents
				item.Reason = "Checkpoint exists but no usage events are bound to it."
				item.UsageEventFound = false
			} else if pr.UsageRefreshedAt != nil &&
				fact.LatestObserved != nil &&
				fact.LatestObserved.After(*pr.UsageRefreshedAt) {
				item.Status = UsageStatusStaleSnapshot
				item.Reason = "Usage events newer than the PR snapshot are bound to this checkpoint."
			}
		}
		commits = append(commits, item)
		if item.Status != UsageStatusFresh && overall == UsageStatusFresh {
			overall = item.Status
			reason = item.Reason
		}
	}

	return &PRFreshness{
		Status:    overall,
		Reason:    reason,
		CheckedAt: checkedAt,
		Commits:   commits,
	}
}
