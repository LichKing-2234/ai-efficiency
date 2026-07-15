package prusage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
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
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate PR freshness: load PR: %w", err)
	}
	repo, err := pr.Edges.RepoConfigOrErr()
	if err != nil {
		return nil, fmt.Errorf("evaluate PR freshness: load repo config: %w", err)
	}

	page, err := s.EvaluatePRFreshnessPage(ctx, repo.ID, []*ent.PrRecord{pr})
	if err != nil {
		return nil, fmt.Errorf("evaluate PR freshness: evaluate page: %w", err)
	}
	freshness, ok := page[pr.ID]
	if !ok {
		return nil, fmt.Errorf("evaluate PR freshness: evaluated page omitted selected PR")
	}
	return freshness, nil
}

func (s *Service) EvaluatePRFreshnessPage(
	ctx context.Context,
	repoConfigID int,
	prs []*ent.PrRecord,
) (map[int]*PRFreshness, error) {
	const op = "evaluate PR freshness page"
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("%s: ent client is required", op)
	}
	if repoConfigID <= 0 {
		return nil, fmt.Errorf("%s: repo config ID must be positive", op)
	}

	uniquePRs := make([]*ent.PrRecord, 0, len(prs))
	prIDs := make([]int, 0, len(prs))
	seenPRIDs := make(map[int]struct{}, len(prs))
	for i, pr := range prs {
		if pr == nil {
			return nil, fmt.Errorf("%s: PR at index %d is nil", op, i)
		}
		if _, ok := seenPRIDs[pr.ID]; ok {
			continue
		}
		seenPRIDs[pr.ID] = struct{}{}
		uniquePRs = append(uniquePRs, pr)
		prIDs = append(prIDs, pr.ID)
	}
	result := make(map[int]*PRFreshness, len(uniquePRs))
	if len(uniquePRs) == 0 {
		return result, nil
	}

	snapshots, err := s.entClient.PRCommitUsageSnapshot.Query().
		Where(prcommitusagesnapshot.PrRecordIDIn(prIDs...)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: load snapshots: %w", op, err)
	}
	pendingCount, err := s.entClient.ToolUsageEvent.Query().
		Where(
			toolusageevent.RepoConfigIDEQ(repoConfigID),
			toolusageevent.CommitCheckpointIDIsNil(),
		).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: count pending usage events: %w", op, err)
	}

	snapshotsByPR := make(map[int][]*ent.PRCommitUsageSnapshot, len(uniquePRs))
	checkpointIDSet := make(map[int]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotsByPR[snapshot.PrRecordID] = append(snapshotsByPR[snapshot.PrRecordID], snapshot)
		if snapshot.CommitCheckpointID != nil {
			checkpointIDSet[*snapshot.CommitCheckpointID] = struct{}{}
		}
	}

	usageByCheckpoint := make(map[int]checkpointUsageFact, len(checkpointIDSet))
	if len(checkpointIDSet) > 0 {
		checkpointIDs := make([]int, 0, len(checkpointIDSet))
		for checkpointID := range checkpointIDSet {
			checkpointIDs = append(checkpointIDs, checkpointID)
		}
		sort.Ints(checkpointIDs)

		var aggregates []struct {
			CommitCheckpointID int       `json:"commit_checkpoint_id"`
			Count              int       `json:"event_count"`
			LatestObserved     time.Time `json:"latest_observed"`
		}
		if err := s.entClient.ToolUsageEvent.Query().
			Where(toolusageevent.CommitCheckpointIDIn(checkpointIDs...)).
			GroupBy(toolusageevent.FieldCommitCheckpointID).
			Aggregate(
				ent.As(ent.Count(), "event_count"),
				ent.As(ent.Max(toolusageevent.FieldObservedEndAt), "latest_observed"),
			).
			Scan(ctx, &aggregates); err != nil {
			return nil, fmt.Errorf("%s: load checkpoint usage facts: %w", op, err)
		}
		for _, aggregate := range aggregates {
			latestObserved := aggregate.LatestObserved
			usageByCheckpoint[aggregate.CommitCheckpointID] = checkpointUsageFact{
				Count:          aggregate.Count,
				LatestObserved: &latestObserved,
			}
		}
	}

	checkedAt := time.Now().UTC()
	for _, pr := range uniquePRs {
		result[pr.ID] = evaluateLoadedPRFreshness(
			pr,
			snapshotsByPR[pr.ID],
			pendingCount > 0,
			usageByCheckpoint,
			checkedAt,
		)
	}
	return result, nil
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
