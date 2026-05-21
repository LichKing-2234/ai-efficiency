package prusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/internal/scm"
)

type Service struct {
	entClient *ent.Client
}

type Summary struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	RequestCount      int
	CommitCount       int
}

type CommitSnapshot struct {
	CommitSHA          string
	CommitCheckpointID *int
	CapturedAt         *time.Time
	InputTokens        int64
	OutputTokens       int64
	CachedInputTokens  int64
	ReasoningTokens    int64
	CreditUsage        float64
	RequestCount       int
	SortOrder          int
}

type Result struct {
	PRRecordID   int
	Summary      Summary
	Commits      []CommitSnapshot
	RefreshedAt  time.Time
	SnapshotHash string
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*Result, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("refresh PR usage: ent client is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("refresh PR usage: scm provider is required")
	}
	if pr == nil {
		return nil, fmt.Errorf("refresh PR usage: pr record is required")
	}

	rc, err := pr.QueryRepoConfig().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh PR usage: load repo config: %w", err)
	}

	prCommitSHAs, err := provider.ListPRCommits(ctx, rc.FullName, pr.ScmPrID)
	if err != nil {
		return nil, fmt.Errorf("refresh PR usage: list pr commits: %w", err)
	}

	commitSnapshots := make([]CommitSnapshot, 0, len(prCommitSHAs))
	var summary Summary
	for idx, prCommitSHA := range prCommitSHAs {
		snapshot, err := s.buildCommitSnapshot(ctx, rc.ID, prCommitSHA, idx)
		if err != nil {
			return nil, err
		}
		commitSnapshots = append(commitSnapshots, snapshot)
		summary.InputTokens += snapshot.InputTokens
		summary.OutputTokens += snapshot.OutputTokens
		summary.CachedInputTokens += snapshot.CachedInputTokens
		summary.ReasoningTokens += snapshot.ReasoningTokens
		summary.CreditUsage += snapshot.CreditUsage
		summary.RequestCount += snapshot.RequestCount
	}
	summary.CommitCount = len(commitSnapshots)

	refreshedAt := time.Now().UTC()
	snapshotHash := hashCommitSet(prCommitSHAs)

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh PR usage: start tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.PRCommitUsageSnapshot.Delete().
		Where(prcommitusagesnapshot.HasPrRecordWith()).
		Where(prcommitusagesnapshot.PrRecordIDEQ(pr.ID)).
		Exec(ctx); err != nil {
		return nil, fmt.Errorf("refresh PR usage: delete old commit snapshots: %w", err)
	}

	for _, snapshot := range commitSnapshots {
		create := tx.PRCommitUsageSnapshot.Create().
			SetPrRecordID(pr.ID).
			SetCommitSha(snapshot.CommitSHA).
			SetInputTokens(snapshot.InputTokens).
			SetOutputTokens(snapshot.OutputTokens).
			SetCachedInputTokens(snapshot.CachedInputTokens).
			SetReasoningTokens(snapshot.ReasoningTokens).
			SetCreditUsage(snapshot.CreditUsage).
			SetRequestCount(snapshot.RequestCount).
			SetSortOrder(snapshot.SortOrder)
		if snapshot.CommitCheckpointID != nil {
			create.SetCommitCheckpointID(*snapshot.CommitCheckpointID)
		}
		if snapshot.CapturedAt != nil {
			create.SetCapturedAt(*snapshot.CapturedAt)
		}
		if _, err := create.Save(ctx); err != nil {
			return nil, fmt.Errorf("refresh PR usage: create commit snapshot: %w", err)
		}
	}

	if _, err := tx.PrRecord.UpdateOneID(pr.ID).
		SetUsageInputTokens(summary.InputTokens).
		SetUsageOutputTokens(summary.OutputTokens).
		SetUsageCachedInputTokens(summary.CachedInputTokens).
		SetUsageReasoningTokens(summary.ReasoningTokens).
		SetUsageCreditUsage(summary.CreditUsage).
		SetUsageRequestCount(summary.RequestCount).
		SetUsageCommitCount(summary.CommitCount).
		SetUsageRefreshedAt(refreshedAt).
		SetUsageCommitSnapshotHash(snapshotHash).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("refresh PR usage: update pr record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("refresh PR usage: commit tx: %w", err)
	}

	return &Result{
		PRRecordID:   pr.ID,
		Summary:      summary,
		Commits:      commitSnapshots,
		RefreshedAt:  refreshedAt,
		SnapshotHash: snapshotHash,
	}, nil
}

func (s *Service) buildCommitSnapshot(ctx context.Context, repoConfigID int, prCommitSHA string, sortOrder int) (CommitSnapshot, error) {
	snapshot := CommitSnapshot{
		CommitSHA: prCommitSHA,
		SortOrder: sortOrder,
	}

	candidates, err := s.expandCommitCandidates(ctx, repoConfigID, []string{prCommitSHA})
	if err != nil {
		return snapshot, fmt.Errorf("refresh PR usage: expand commit candidates: %w", err)
	}
	if len(candidates) == 0 {
		return snapshot, nil
	}

	checkpoints, err := s.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.RepoConfigIDEQ(repoConfigID),
			commitcheckpoint.CommitShaIn(candidates...),
		).
		Order(ent.Asc(commitcheckpoint.FieldCapturedAt)).
		All(ctx)
	if err != nil {
		return snapshot, fmt.Errorf("refresh PR usage: query checkpoints: %w", err)
	}
	if len(checkpoints) == 0 {
		return snapshot, nil
	}

	for _, cp := range checkpoints {
		input, output, cache, reasoning, credit, requestCount, err := s.loadCheckpointUsage(ctx, cp.ID)
		if err != nil {
			return snapshot, err
		}
		snapshot.InputTokens += input
		snapshot.OutputTokens += output
		snapshot.CachedInputTokens += cache
		snapshot.ReasoningTokens += reasoning
		snapshot.CreditUsage += credit
		snapshot.RequestCount += requestCount
		snapshot.CommitCheckpointID = &cp.ID
		capturedAt := cp.CapturedAt
		snapshot.CapturedAt = &capturedAt
	}

	return snapshot, nil
}

func (s *Service) loadCheckpointUsage(ctx context.Context, checkpointID int) (int64, int64, int64, int64, float64, int, error) {
	items, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.CommitCheckpointIDEQ(checkpointID)).
		All(ctx)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("refresh PR usage: query tool usage events: %w", err)
	}

	var input, output, cache, reasoning int64
	var credit float64
	var requestCount int
	for _, item := range items {
		input += item.InputTokens
		output += item.OutputTokens
		cache += item.CachedInputTokens
		reasoning += item.ReasoningTokens
		credit += item.CreditUsage
		requestCount += item.RequestCount
	}
	return input, output, cache, reasoning, credit, requestCount, nil
}

func (s *Service) expandCommitCandidates(ctx context.Context, repoConfigID int, currentSHAs []string) ([]string, error) {
	seen := make(map[string]struct{}, len(currentSHAs))
	ordered := make([]string, 0, len(currentSHAs))
	queue := append([]string(nil), currentSHAs...)

	for len(queue) > 0 {
		batch := make([]string, 0, len(queue))
		next := make([]string, 0)
		for _, sha := range queue {
			sha = strings.TrimSpace(sha)
			if sha == "" {
				continue
			}
			if _, ok := seen[sha]; ok {
				continue
			}
			seen[sha] = struct{}{}
			ordered = append(ordered, sha)
			batch = append(batch, sha)
		}
		if len(batch) == 0 {
			break
		}

		rewrites, err := s.entClient.CommitRewrite.Query().
			Where(
				commitrewrite.RepoConfigIDEQ(repoConfigID),
				commitrewrite.NewCommitShaIn(batch...),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, rw := range rewrites {
			oldSHA := strings.TrimSpace(rw.OldCommitSha)
			if oldSHA == "" {
				continue
			}
			if _, ok := seen[oldSHA]; ok {
				continue
			}
			next = append(next, oldSHA)
		}
		queue = next
	}

	return ordered, nil
}

func hashCommitSet(commits []string) string {
	normalized := append([]string(nil), commits...)
	for idx := range normalized {
		normalized[idx] = strings.TrimSpace(normalized[idx])
	}
	slices.Sort(normalized)
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x1f")))
	return hex.EncodeToString(sum[:])
}
