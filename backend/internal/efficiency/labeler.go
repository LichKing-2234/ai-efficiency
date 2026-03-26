package efficiency

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// Labeler handles PR auto-labeling based on session-token association.
type Labeler struct {
	entClient     *ent.Client
	relayProvider relay.Provider // nil if relay not configured
	logger        *zap.Logger
}

// NewLabeler creates a new Labeler.
func NewLabeler(entClient *ent.Client, relayProvider relay.Provider, logger *zap.Logger) *Labeler {
	return &Labeler{
		entClient:     entClient,
		relayProvider: relayProvider,
		logger:        logger,
	}
}

// LabelResult holds the result of a labeling operation.
type LabelResult struct {
	PRRecordID int      `json:"pr_record_id"`
	AILabel    string   `json:"ai_label"`
	AIRatio    float64  `json:"ai_ratio"`
	TokenCost  float64  `json:"token_cost"`
	SessionIDs []string `json:"session_ids"`
}

// LabelPR analyzes a PR record and applies AI labels based on session matching.
func (l *Labeler) LabelPR(ctx context.Context, prRecordID int) (*LabelResult, error) {
	// Get PR record with repo config
	pr, err := l.entClient.PrRecord.Query().
		Where(prrecord.IDEQ(prRecordID)).
		WithRepoConfig().
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("get PR record: %w", err)
	}

	rc := pr.Edges.RepoConfig
	if rc == nil {
		return nil, fmt.Errorf("PR record has no repo config")
	}

	// Find matching sessions: same repo + overlapping branch + time window
	// Look for sessions on the source branch that were active before the PR was created
	timeWindow := pr.CreatedAt.Add(-7 * 24 * time.Hour) // 7 days before PR

	sessions, err := l.entClient.Session.Query().
		Where(
			session.HasRepoConfigWith(repoconfig.IDEQ(rc.ID)),
			session.BranchEQ(pr.SourceBranch),
			session.CreatedAtGTE(timeWindow),
			session.CreatedAtLTE(pr.CreatedAt),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("find matching sessions: %w", err)
	}

	result := &LabelResult{
		PRRecordID: prRecordID,
	}

	if len(sessions) == 0 {
		// No AI sessions found
		result.AILabel = "no_ai_detected"
		if err := l.entClient.PrRecord.UpdateOneID(prRecordID).
			SetAiLabel(prrecord.AiLabelNoAiDetected).
			Exec(ctx); err != nil {
			return nil, fmt.Errorf("update PR label: %w", err)
		}
		return result, nil
	}

	// Collect session IDs and calculate token cost from relay provider
	var sessionIDs []string
	var totalTokenCost float64
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.ID.String())

		// Query relay provider for usage stats if the session has a relay user ID and provider is available
		if l.relayProvider != nil && s.RelayUserID != nil {
			endTime := s.CreatedAt.Add(24 * time.Hour) // default window
			if s.EndedAt != nil {
				endTime = *s.EndedAt
			}
			stats, err := l.relayProvider.GetUsageStats(ctx, int64(*s.RelayUserID), s.StartedAt, endTime)
			if err != nil {
				l.logger.Warn("failed to query relay usage stats",
					zap.String("session_id", s.ID.String()),
					zap.Error(err),
				)
			} else if stats != nil {
				totalTokenCost += stats.TotalCost
			}
		}
	}

	result.SessionIDs = sessionIDs
	result.AILabel = "ai_via_sub2api"
	result.TokenCost = totalTokenCost

	// Calculate AI ratio: proportion of AI-assisted sessions relative to PR activity
	// 1.0 = fully AI-assisted, 0.0 = no AI involvement
	if totalTokenCost > 0 {
		// If we have token cost data, use it as a signal
		result.AIRatio = 1.0
	} else if len(sessions) > 0 {
		// Sessions exist but no token cost — partial AI involvement
		result.AIRatio = 0.5
	}
	if result.AIRatio > 1 {
		result.AIRatio = 1
	}

	// Update PR record
	update := l.entClient.PrRecord.UpdateOneID(prRecordID).
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetSessionIds(sessionIDs).
		SetAiRatio(result.AIRatio)
	if totalTokenCost > 0 {
		update.SetTokenCost(totalTokenCost)
	}
	if err := update.Exec(ctx); err != nil {
		return nil, fmt.Errorf("update PR record: %w", err)
	}

	l.logger.Info("PR labeled",
		zap.Int("pr_id", prRecordID),
		zap.String("label", result.AILabel),
		zap.Int("sessions", len(sessions)),
		zap.Float64("token_cost", totalTokenCost),
	)

	return result, nil
}
