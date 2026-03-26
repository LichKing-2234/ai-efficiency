package prsync

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// SyncResult holds the summary of a sync operation.
type SyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Total   int `json:"total"`
}

// Service handles PR synchronization from SCM providers.
type Service struct {
	entClient  *ent.Client
	labeler    *efficiency.Labeler
	aggregator *efficiency.Aggregator
	logger     *zap.Logger
}

// NewService creates a new PR sync service.
func NewService(entClient *ent.Client, labeler *efficiency.Labeler, aggregator *efficiency.Aggregator, logger *zap.Logger) *Service {
	return &Service{
		entClient:  entClient,
		labeler:    labeler,
		aggregator: aggregator,
		logger:     logger,
	}
}

// Sync fetches all PRs from the SCM provider and upserts them into pr_records.
func (s *Service) Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	allPRs, err := s.fetchAllPRs(ctx, scmProvider, rc.FullName)
	if err != nil {
		return nil, fmt.Errorf("fetch PRs from SCM: %w", err)
	}

	result := &SyncResult{Total: len(allPRs)}
	var labelIDs []int

	for _, pr := range allPRs {
		recordID, created, err := s.upsertPR(ctx, rc.ID, pr)
		if err != nil {
			s.logger.Warn("failed to upsert PR", zap.Int("scm_pr_id", pr.ID), zap.Error(err))
			continue
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
		labelIDs = append(labelIDs, recordID)
	}

	// Run labeler on all synced PRs
	if s.labeler != nil {
		for _, id := range labelIDs {
			if _, err := s.labeler.LabelPR(ctx, id); err != nil {
				s.logger.Warn("failed to label PR", zap.Int("pr_record_id", id), zap.Error(err))
			}
		}
	}

	s.logger.Info("PR sync completed",
		zap.String("repo", rc.FullName),
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("total", result.Total),
	)

	// Auto-aggregate metrics after sync
	if s.aggregator != nil {
		for _, period := range []string{"daily", "weekly", "monthly"} {
			if err := s.aggregator.AggregateForRepo(ctx, rc.ID, period, efficiency.ComputePeriodStart(period)); err != nil {
				s.logger.Warn("post-sync aggregation failed",
					zap.Int("repo_id", rc.ID),
					zap.String("period", period),
					zap.Error(err),
				)
			}
		}
	}

	return result, nil
}

func (s *Service) fetchAllPRs(ctx context.Context, provider scm.SCMProvider, repoFullName string) ([]*scm.PR, error) {
	var all []*scm.PR
	page := 1
	pageSize := 100

	for {
		prs, err := provider.ListPRs(ctx, repoFullName, scm.PRListOpts{
			State:    "all",
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, prs...)
		if len(prs) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

func (s *Service) upsertPR(ctx context.Context, repoConfigID int, pr *scm.PR) (int, bool, error) {
	existing, err := s.entClient.PrRecord.Query().
		Where(
			prrecord.ScmPrIDEQ(pr.ID),
			prrecord.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID)),
		).
		Only(ctx)

	if err != nil && !ent.IsNotFound(err) {
		return 0, false, fmt.Errorf("query existing PR: %w", err)
	}

	status := mapPRStatus(pr.State)

	if existing != nil {
		update := s.entClient.PrRecord.UpdateOneID(existing.ID).
			SetTitle(pr.Title).
			SetAuthor(pr.Author).
			SetSourceBranch(pr.SourceBranch).
			SetTargetBranch(pr.TargetBranch).
			SetStatus(status).
			SetScmPrURL(pr.URL).
			SetLinesAdded(pr.LinesAdded).
			SetLinesDeleted(pr.LinesDeleted)

		if !pr.CreatedAt.IsZero() {
			update.SetCreatedAt(pr.CreatedAt)
		}
		if !pr.MergedAt.IsZero() {
			update.SetNillableMergedAt(&pr.MergedAt)
		}
		if len(pr.Labels) > 0 {
			update.SetLabels(pr.Labels)
		}

		if err := update.Exec(ctx); err != nil {
			return 0, false, fmt.Errorf("update PR: %w", err)
		}
		return existing.ID, false, nil
	}

	create := s.entClient.PrRecord.Create().
		SetRepoConfigID(repoConfigID).
		SetScmPrID(pr.ID).
		SetScmPrURL(pr.URL).
		SetAuthor(pr.Author).
		SetTitle(pr.Title).
		SetSourceBranch(pr.SourceBranch).
		SetTargetBranch(pr.TargetBranch).
		SetStatus(status).
		SetLinesAdded(pr.LinesAdded).
		SetLinesDeleted(pr.LinesDeleted)

	if !pr.CreatedAt.IsZero() {
		create.SetCreatedAt(pr.CreatedAt)
	}
	if !pr.MergedAt.IsZero() {
		create.SetNillableMergedAt(&pr.MergedAt)
	}
	if len(pr.Labels) > 0 {
		create.SetLabels(pr.Labels)
	}

	record, err := create.Save(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("create PR: %w", err)
	}
	return record.ID, true, nil
}

func mapPRStatus(state string) prrecord.Status {
	switch state {
	case "merged":
		return prrecord.StatusMerged
	case "closed":
		return prrecord.StatusClosed
	default:
		return prrecord.StatusOpen
	}
}
