package handler

import (
	"context"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/scm"
)

// analysisScanner abstracts analysis.Service for testability.
type analysisScanner interface {
	RunScan(ctx context.Context, repoConfigID int) (*ent.AiScanResult, error)
	ListScans(ctx context.Context, repoConfigID int, limit int) ([]*ent.AiScanResult, error)
	GetLatestScan(ctx context.Context, repoConfigID int) (*ent.AiScanResult, error)
}

// optimizerService abstracts analysis.Optimizer for testability.
type optimizerService interface {
	CreateOptimizationPR(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scanResult *ent.AiScanResult) (*analysis.OptimizeResult, error)
	PreviewOptimization(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scanResult *ent.AiScanResult) (*analysis.OptimizePreview, error)
	ConfirmOptimization(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, files map[string]string, score int) (*analysis.OptimizeResult, error)
}

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
