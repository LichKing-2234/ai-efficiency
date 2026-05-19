package efficiency

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
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

// LabelPR analyzes a PR record and applies AI labels based on checkpoint-bound tool usage.
func (l *Labeler) LabelPR(ctx context.Context, prRecordID int) (*LabelResult, error) {
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

	windowStart := pr.CreatedAt.Add(-7 * 24 * time.Hour)

	checkpoints, err := l.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.RepoConfigIDEQ(rc.ID),
			commitcheckpoint.BranchSnapshotEQ(pr.SourceBranch),
			commitcheckpoint.CapturedAtGTE(windowStart),
			commitcheckpoint.CapturedAtLTE(pr.CreatedAt),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("find matching checkpoints: %w", err)
	}

	result := &LabelResult{
		PRRecordID: prRecordID,
	}

	if len(checkpoints) == 0 {
		return l.markNoAIDetected(ctx, result)
	}

	checkpointIDs := make([]int, 0, len(checkpoints))
	for _, cp := range checkpoints {
		checkpointIDs = append(checkpointIDs, cp.ID)
	}

	events, err := l.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.HasCommitCheckpointWith(commitcheckpoint.IDIn(checkpointIDs...))).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("find matching tool usage events: %w", err)
	}
	if len(events) == 0 {
		return l.markNoAIDetected(ctx, result)
	}

	sessionSet := map[string]struct{}{}
	hasDirectUsage := false
	for _, event := range events {
		if toolSessionID := strings.TrimSpace(event.ToolSessionID); toolSessionID != "" {
			sessionSet[toolSessionID] = struct{}{}
		}
		if event.InputTokens > 0 || event.OutputTokens > 0 || event.CachedInputTokens > 0 || event.ReasoningTokens > 0 || event.CreditUsage > 0 || event.RequestCount > 0 {
			hasDirectUsage = true
		}
	}

	sessionIDs := make([]string, 0, len(sessionSet))
	for toolSessionID := range sessionSet {
		sessionIDs = append(sessionIDs, toolSessionID)
	}
	slices.Sort(sessionIDs)

	result.SessionIDs = sessionIDs
	result.AILabel = "ai_via_sub2api"
	result.TokenCost = 0

	if hasDirectUsage {
		result.AIRatio = 1.0
	} else {
		result.AIRatio = 0.5
	}

	update := l.entClient.PrRecord.UpdateOneID(prRecordID).
		SetAiLabel(prrecord.AiLabelAiViaSub2api).
		SetSessionIds(sessionIDs).
		SetAiRatio(result.AIRatio).
		SetTokenCost(0)
	if err := update.Exec(ctx); err != nil {
		return nil, fmt.Errorf("update PR record: %w", err)
	}

	l.logger.Info("PR labeled",
		zap.Int("pr_id", prRecordID),
		zap.String("label", result.AILabel),
		zap.Int("tool_sessions", len(sessionIDs)),
		zap.Int("checkpoints", len(checkpointIDs)),
	)

	return result, nil
}

func (l *Labeler) markNoAIDetected(ctx context.Context, result *LabelResult) (*LabelResult, error) {
	result.AILabel = "no_ai_detected"
	result.AIRatio = 0
	result.TokenCost = 0
	result.SessionIDs = []string{}
	if err := l.entClient.PrRecord.UpdateOneID(result.PRRecordID).
		SetAiLabel(prrecord.AiLabelNoAiDetected).
		SetAiRatio(0).
		SetTokenCost(0).
		SetSessionIds([]string{}).
		Exec(ctx); err != nil {
		return nil, fmt.Errorf("update PR label: %w", err)
	}
	return result, nil
}
