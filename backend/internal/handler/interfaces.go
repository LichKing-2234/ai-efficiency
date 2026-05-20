package handler

import (
	"context"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/scm"
)

// repoSCMProvider abstracts repo.Service.GetSCMProvider for testability.
type repoSCMProvider interface {
	GetSCMProvider(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error)
}

// prSyncer abstracts prsync.Service for testability.
type prSyncer interface {
	Sync(ctx context.Context, scmProvider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
}

type prAttributionSettler interface {
	Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*attribution.SettleResult, error)
}
