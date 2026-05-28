package prsync

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// SyncResult holds the summary of a sync operation.
type SyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Total   int `json:"total"`
}

type UpsertState string

const (
	UpsertCreated   UpsertState = "created"
	UpsertChanged   UpsertState = "changed"
	UpsertUnchanged UpsertState = "unchanged"
)

type SyncProgress struct {
	Phase             string
	CurrentPage       int
	PageSize          int
	FetchedPRs        int
	TotalPRs          int
	ProcessedPRs      int
	CreatedPRs        int
	ChangedPRs        int
	UnchangedPRs      int
	UpsertFailedPRs   int
	LabeledPRs        int
	LabelFailedPRs    int
	UsageTotalPRs     int
	UsageRefreshedPRs int
	UsageSkippedPRs   int
	UsageFailedPRs    int
}

type ProgressSink interface {
	UpdateProgress(ctx context.Context, jobID int, progress SyncProgress) error
	FailJob(ctx context.Context, jobID int, phase string, err error) error
	CompleteJob(ctx context.Context, jobID int, result SyncResult) error
}

// Service handles PR synchronization from SCM providers.
type Service struct {
	entClient      *ent.Client
	labeler        *efficiency.Labeler
	usageRefresher usageRefresher
	logger         *zap.Logger
}

// NewService creates a new PR sync service.
func NewService(entClient *ent.Client, labeler *efficiency.Labeler, logger *zap.Logger, usageRefreshers ...usageRefresher) *Service {
	var refresher usageRefresher
	if len(usageRefreshers) > 0 {
		refresher = usageRefreshers[0]
	}
	return &Service{
		entClient:      entClient,
		labeler:        labeler,
		usageRefresher: refresher,
		logger:         logger,
	}
}

func (s *Service) StartSyncJob(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error) {
	if s == nil || s.entClient == nil {
		return nil, false, fmt.Errorf("start PR sync job: ent client is required")
	}
	if scmProvider == nil {
		return nil, false, fmt.Errorf("start PR sync job: scm provider is required")
	}
	if rc == nil {
		return nil, false, fmt.Errorf("start PR sync job: repo config is required")
	}

	existing, err := s.entClient.PRSyncJob.Query().
		Where(
			prsyncjob.RepoConfigIDEQ(rc.ID),
			prsyncjob.StatusIn(prsyncjob.StatusQueued, prsyncjob.StatusRunning),
		).
		Order(ent.Desc(prsyncjob.FieldCreatedAt)).
		First(ctx)
	if err == nil {
		return existing, true, nil
	}
	if !ent.IsNotFound(err) {
		return nil, false, fmt.Errorf("query running PR sync job: %w", err)
	}

	job, err := s.entClient.PRSyncJob.Create().
		SetRepoConfigID(rc.ID).
		SetStatus(prsyncjob.StatusQueued).
		SetPhase(prsyncjob.PhaseQueued).
		SetPageSize(100).
		Save(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("create PR sync job: %w", err)
	}
	return job, false, nil
}

func (s *Service) UpdateProgress(ctx context.Context, jobID int, p SyncProgress) error {
	update := s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusRunning).
		SetPhase(prsyncjob.Phase(p.Phase)).
		SetCurrentPage(p.CurrentPage).
		SetPageSize(p.PageSize).
		SetFetchedPrs(p.FetchedPRs).
		SetTotalPrs(p.TotalPRs).
		SetProcessedPrs(p.ProcessedPRs).
		SetCreatedPrs(p.CreatedPRs).
		SetChangedPrs(p.ChangedPRs).
		SetUnchangedPrs(p.UnchangedPRs).
		SetUpsertFailedPrs(p.UpsertFailedPRs).
		SetLabeledPrs(p.LabeledPRs).
		SetLabelFailedPrs(p.LabelFailedPRs).
		SetUsageTotalPrs(p.UsageTotalPRs).
		SetUsageRefreshedPrs(p.UsageRefreshedPRs).
		SetUsageSkippedPrs(p.UsageSkippedPRs).
		SetUsageFailedPrs(p.UsageFailedPRs)
	if p.Phase == string(prsyncjob.PhaseFetchingPrs) || p.Phase == string(prsyncjob.PhaseUpsertingPrs) {
		update.SetStartedAt(time.Now().UTC())
	}
	return update.Exec(ctx)
}

func (s *Service) FailJob(ctx context.Context, jobID int, phase string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusFailed).
		SetPhase(prsyncjob.PhaseFailed).
		SetNillableLastError(&msg).
		SetCompletedAt(time.Now().UTC()).
		Exec(ctx)
}

func (s *Service) CompleteJob(ctx context.Context, jobID int, result SyncResult) error {
	return s.entClient.PRSyncJob.UpdateOneID(jobID).
		SetStatus(prsyncjob.StatusCompleted).
		SetPhase(prsyncjob.PhaseCompleted).
		SetTotalPrs(result.Total).
		SetCompletedAt(time.Now().UTC()).
		Exec(ctx)
}

// Sync fetches all PRs from the SCM provider and upserts them into pr_records.
func (s *Service) Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	return s.SyncWithProgress(ctx, scmProvider, rc, 0, nil)
}

func (s *Service) RunSyncJob(ctx context.Context, jobID int, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*SyncResult, error) {
	result, err := s.SyncWithProgress(ctx, scmProvider, rc, jobID, s)
	if err != nil {
		_ = s.FailJob(context.Background(), jobID, "failed", err)
		return nil, err
	}
	_ = s.CompleteJob(context.Background(), jobID, *result)
	return result, nil
}

func (s *Service) GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error) {
	if s == nil || s.entClient == nil {
		return nil, fmt.Errorf("get PR sync job: ent client is required")
	}
	job, err := s.entClient.PRSyncJob.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get PR sync job %d: %w", id, err)
	}
	return job, nil
}

func (s *Service) SyncWithProgress(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig, jobID int, sink ProgressSink) (*SyncResult, error) {
	progress := SyncProgress{Phase: string(prsyncjob.PhaseFetchingPrs), PageSize: 100}
	allPRs, err := s.fetchAllPRsWithProgress(ctx, scmProvider, rc.FullName, jobID, sink, &progress)
	if err != nil {
		return nil, fmt.Errorf("fetch PRs from SCM: %w", err)
	}

	result := &SyncResult{Total: len(allPRs)}
	var labelIDs []int
	activeCutoff := time.Now().AddDate(0, -3, 0)
	var activePRIDs []int
	activePRIDSet := map[int]struct{}{}

	progress.Phase = string(prsyncjob.PhaseUpsertingPrs)
	progress.TotalPRs = len(allPRs)
	for _, pr := range allPRs {
		recordID, state, err := s.upsertPR(ctx, rc.ID, pr)
		if err != nil {
			progress.UpsertFailedPRs++
			progress.ProcessedPRs++
			s.updateProgress(ctx, jobID, sink, progress)
			s.logger.Warn("failed to upsert PR", zap.Int("scm_pr_id", pr.ID), zap.Error(err))
			continue
		}
		if state == UpsertCreated {
			result.Created++
			progress.CreatedPRs++
		} else {
			result.Updated++
			if state == UpsertChanged {
				progress.ChangedPRs++
			} else {
				progress.UnchangedPRs++
			}
		}
		progress.ProcessedPRs++
		labelIDs = append(labelIDs, recordID)
		if state == UpsertCreated || state == UpsertChanged || mapPRStatus(pr.State) == prrecord.StatusOpen || (!pr.CreatedAt.IsZero() && !pr.CreatedAt.Before(activeCutoff)) {
			if _, ok := activePRIDSet[recordID]; !ok {
				activePRIDs = append(activePRIDs, recordID)
				activePRIDSet[recordID] = struct{}{}
			}
		}
		s.updateProgress(ctx, jobID, sink, progress)
	}

	// Run labeler on all synced PRs
	progress.Phase = string(prsyncjob.PhaseLabeling)
	if s.labeler != nil {
		for _, id := range labelIDs {
			if _, err := s.labeler.LabelPR(ctx, id); err != nil {
				progress.LabelFailedPRs++
				s.logger.Warn("failed to label PR", zap.Int("pr_record_id", id), zap.Error(err))
			} else {
				progress.LabeledPRs++
			}
			s.updateProgress(ctx, jobID, sink, progress)
		}
	}

	progress.Phase = string(prsyncjob.PhaseRefreshingUsage)
	progress.UsageTotalPRs = len(activePRIDs)
	if s.usageRefresher != nil {
		for _, prID := range activePRIDs {
			pr, err := s.entClient.PrRecord.Get(ctx, prID)
			if err != nil {
				progress.UsageFailedPRs++
				s.updateProgress(ctx, jobID, sink, progress)
				s.logger.Warn("failed to load PR for usage refresh", zap.Int("pr_record_id", prID), zap.Error(err))
				continue
			}
			if _, err := s.usageRefresher.RefreshPR(ctx, scmProvider, pr); err != nil {
				progress.UsageFailedPRs++
				s.logger.Warn("failed to refresh PR usage", zap.Int("pr_record_id", prID), zap.Error(err))
			} else {
				progress.UsageRefreshedPRs++
			}
			s.updateProgress(ctx, jobID, sink, progress)
		}
	} else {
		progress.UsageSkippedPRs = len(activePRIDs)
		s.updateProgress(ctx, jobID, sink, progress)
	}

	s.logger.Info("PR sync completed",
		zap.String("repo", rc.FullName),
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("total", result.Total),
	)

	return result, nil
}

func (s *Service) fetchAllPRsWithProgress(ctx context.Context, provider scm.SCMProvider, repoFullName string, jobID int, sink ProgressSink, progress *SyncProgress) ([]*scm.PR, error) {
	var all []*scm.PR
	page := 1
	pageSize := 100
	if progress != nil && progress.PageSize > 0 {
		pageSize = progress.PageSize
	}

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
		if progress != nil {
			progress.Phase = string(prsyncjob.PhaseFetchingPrs)
			progress.CurrentPage = page
			progress.PageSize = pageSize
			progress.FetchedPRs = len(all)
			progress.TotalPRs = len(all)
			s.updateProgress(ctx, jobID, sink, *progress)
		}
		if len(prs) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

func (s *Service) updateProgress(ctx context.Context, jobID int, sink ProgressSink, progress SyncProgress) {
	if sink == nil || jobID <= 0 {
		return
	}
	if err := sink.UpdateProgress(ctx, jobID, progress); err != nil {
		s.logger.Warn("failed to update PR sync progress", zap.Int("job_id", jobID), zap.Error(err))
	}
}

type usageRefresher interface {
	RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error)
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

func (s *Service) upsertPR(ctx context.Context, repoConfigID int, pr *scm.PR) (int, UpsertState, error) {
	existing, err := s.entClient.PrRecord.Query().
		Where(
			prrecord.ScmPrIDEQ(pr.ID),
			prrecord.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID)),
		).
		Only(ctx)

	if err != nil && !ent.IsNotFound(err) {
		return 0, "", fmt.Errorf("query existing PR: %w", err)
	}

	status := mapPRStatus(pr.State)

	if existing != nil {
		if !prChanged(existing, pr) {
			return existing.ID, UpsertUnchanged, nil
		}
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
			return 0, "", fmt.Errorf("update PR: %w", err)
		}
		return existing.ID, UpsertChanged, nil
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
		return 0, "", fmt.Errorf("create PR: %w", err)
	}
	return record.ID, UpsertCreated, nil
}

func prChanged(existing *ent.PrRecord, pr *scm.PR) bool {
	if existing.Title != pr.Title ||
		existing.Author != pr.Author ||
		existing.SourceBranch != pr.SourceBranch ||
		existing.TargetBranch != pr.TargetBranch ||
		existing.Status != mapPRStatus(pr.State) ||
		existing.ScmPrURL != pr.URL ||
		existing.LinesAdded != pr.LinesAdded ||
		existing.LinesDeleted != pr.LinesDeleted {
		return true
	}
	if !pr.CreatedAt.IsZero() && !sameDatabaseTimestamp(existing.CreatedAt, pr.CreatedAt) {
		return true
	}
	if !pr.MergedAt.IsZero() {
		if existing.MergedAt == nil || !sameDatabaseTimestamp(*existing.MergedAt, pr.MergedAt) {
			return true
		}
	}
	if len(pr.Labels) > 0 && !slices.Equal(existing.Labels, pr.Labels) {
		return true
	}
	return false
}

func sameDatabaseTimestamp(a, b time.Time) bool {
	return a.Round(time.Microsecond).Equal(b.Round(time.Microsecond))
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
