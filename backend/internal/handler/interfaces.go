package handler

import (
	"context"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/scm"
)

// repoSCMProvider abstracts repo.Service.GetSCMProvider for testability.
type repoSCMProvider interface {
	GetSCMProvider(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error)
}

// prSyncer abstracts prsync.Service for testability.
type prSyncer interface {
	Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	StartSyncJob(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*ent.PRSyncJob, bool, error)
	RunSyncJob(ctx context.Context, jobID int, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
	GetSyncJob(ctx context.Context, id int) (*ent.PRSyncJob, error)
	GetLatestSyncJobForRepo(ctx context.Context, repoID int) (*ent.PRSyncJob, error)
}

type prAttributionSettler interface {
	Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error)
}

type prUsageRefresher interface {
	RefreshPR(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord) (*prusage.Result, error)
}

type prUsageFreshnessEvaluator interface {
	EvaluatePRFreshness(ctx context.Context, prID int) (*prusage.PRFreshness, error)
}
