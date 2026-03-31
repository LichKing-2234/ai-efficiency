package attribution

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prattributionrun"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/scm"
)

type Service struct {
	entClient     *ent.Client
	relayProvider relay.Provider
}

type SettleResult struct {
	PRRecordID            int                    `json:"pr_record_id"`
	AttributionRunID      int                    `json:"attribution_run_id"`
	ResultClassification  string                 `json:"result_classification"`
	AttributionStatus     string                 `json:"attribution_status"`
	AttributionConfidence string                 `json:"attribution_confidence"`
	PrimaryTokenCount     int64                  `json:"primary_token_count"`
	PrimaryTokenCost      float64                `json:"primary_token_cost"`
	MatchedCommitSHAs     []string               `json:"matched_commit_shas"`
	MatchedSessionIDs     []string               `json:"matched_session_ids"`
	PrimaryUsageSummary   map[string]interface{} `json:"primary_usage_summary"`
	MetadataSummary       map[string]interface{} `json:"metadata_summary"`
	ValidationSummary     map[string]interface{} `json:"validation_summary"`
}

func NewService(entClient *ent.Client, relayProvider relay.Provider) *Service {
	return &Service{
		entClient:     entClient,
		relayProvider: relayProvider,
	}
}

func (s *Service) Settle(ctx context.Context, provider scm.SCMProvider, pr *ent.PrRecord, triggeredBy string) (*SettleResult, error) {
	if s.entClient == nil {
		return nil, fmt.Errorf("settle pr: ent client is required")
	}
	if s.relayProvider == nil {
		return nil, fmt.Errorf("settle pr: relay provider is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("settle pr: scm provider is required")
	}
	if pr == nil {
		return nil, fmt.Errorf("settle pr: pr record is required")
	}

	triggeredBy = strings.TrimSpace(triggeredBy)
	if triggeredBy == "" {
		triggeredBy = "system"
	}

	rc, err := pr.QueryRepoConfig().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle pr: load repo config: %w", err)
	}

	prCommitSHAs, err := provider.ListPRCommits(ctx, rc.FullName, pr.ScmPrID)
	if err != nil {
		return nil, fmt.Errorf("settle pr: list pr commits: %w", err)
	}

	if len(prCommitSHAs) == 0 {
		return s.persistAmbiguous(ctx, pr, triggeredBy, "no_pr_commits", nil)
	}

	matchedCheckpoints, err := s.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.RepoConfigIDEQ(rc.ID),
			commitcheckpoint.CommitShaIn(prCommitSHAs...),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle pr: query matched checkpoints: %w", err)
	}
	if len(matchedCheckpoints) == 0 {
		return s.persistAmbiguous(ctx, pr, triggeredBy, "no_matching_checkpoints", nil)
	}

	commitSet := map[string]struct{}{}
	matchedSessionSet := map[string]struct{}{}
	for _, cp := range matchedCheckpoints {
		commitSet[cp.CommitSha] = struct{}{}
		if cp.BindingSource == commitcheckpoint.BindingSourceUnbound || cp.SessionID == nil {
			return s.persistAmbiguous(ctx, pr, triggeredBy, "unbound_checkpoint", commitSet)
		}
	}

	intervals := make([]map[string]interface{}, 0, len(matchedCheckpoints))
	accountTokenTotals := map[string]int64{}
	accountCostTotals := map[string]float64{}
	var totalTokens int64
	var totalCost float64
	var totalUsageLogs int64

	for _, cp := range matchedCheckpoints {
		sessionID := *cp.SessionID
		matchedSessionSet[sessionID.String()] = struct{}{}

		sess, err := s.entClient.Session.Get(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("settle pr: load session %s: %w", sessionID, err)
		}
		if sess.RelayAPIKeyID == nil {
			return s.persistAmbiguous(ctx, pr, triggeredBy, "session_missing_api_key", commitSet)
		}

		from := sess.StartedAt
		prevCP, err := s.entClient.CommitCheckpoint.Query().
			Where(
				commitcheckpoint.SessionIDEQ(sessionID),
				commitcheckpoint.CapturedAtLT(cp.CapturedAt),
			).
			Order(ent.Desc(commitcheckpoint.FieldCapturedAt)).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("settle pr: load previous checkpoint: %w", err)
		}
		if prevCP != nil {
			from = prevCP.CapturedAt
		}

		to := cp.CapturedAt
		logs, err := s.relayProvider.ListUsageLogsByAPIKeyExact(ctx, int64(*sess.RelayAPIKeyID), from, to)
		if err != nil {
			return nil, fmt.Errorf("settle pr: list usage logs: %w", err)
		}

		var intervalTokens int64
		var intervalCost float64
		for _, log := range logs {
			intervalTokens += log.TotalTokens
			intervalCost += log.TotalCost
			totalUsageLogs++

			accountID := strings.TrimSpace(log.AccountID)
			if accountID == "" {
				accountID = "unknown"
			}
			accountTokenTotals[accountID] += log.TotalTokens
			accountCostTotals[accountID] += log.TotalCost
		}

		totalTokens += intervalTokens
		totalCost += intervalCost

		intervals = append(intervals, map[string]interface{}{
			"session_id":      sessionID.String(),
			"checkpoint_id":   cp.ID,
			"commit_sha":      cp.CommitSha,
			"from":            from,
			"to":              to,
			"usage_log_count": len(logs),
			"total_tokens":    intervalTokens,
			"total_cost":      intervalCost,
		})
	}

	matchedCommits := make([]string, 0, len(commitSet))
	for _, sha := range prCommitSHAs {
		if _, ok := commitSet[sha]; ok {
			matchedCommits = append(matchedCommits, sha)
		}
	}
	if len(matchedCommits) == 0 {
		return s.persistAmbiguous(ctx, pr, triggeredBy, "no_matching_checkpoints", nil)
	}

	matchedSessions := make([]string, 0, len(matchedSessionSet))
	for sessionID := range matchedSessionSet {
		matchedSessions = append(matchedSessions, sessionID)
	}
	slices.Sort(matchedSessions)

	primarySummary := map[string]interface{}{
		"total_tokens":    totalTokens,
		"total_cost":      totalCost,
		"interval_count":  len(intervals),
		"usage_log_count": totalUsageLogs,
	}
	metadataSummary := map[string]interface{}{
		"matched_commit_count":  len(matchedCommits),
		"matched_session_count": len(matchedSessions),
		"intervals":             intervals,
		"account_token_totals":  accountTokenTotals,
		"account_cost_totals":   accountCostTotals,
	}
	validationSummary := map[string]interface{}{
		"result":           "consistent",
		"confidence":       "high",
		"reason":           "all_matched_checkpoints_bound",
		"matched_commits":  len(matchedCommits),
		"matched_sessions": len(matchedSessions),
	}

	return s.persistResult(
		ctx,
		pr,
		triggeredBy,
		prattributionrun.ResultClassificationClear,
		prrecord.AttributionStatusClear,
		prrecord.AttributionConfidenceHigh,
		totalTokens,
		totalCost,
		matchedCommits,
		matchedSessions,
		primarySummary,
		metadataSummary,
		validationSummary,
	)
}

func (s *Service) persistAmbiguous(ctx context.Context, pr *ent.PrRecord, triggeredBy, reason string, commitSet map[string]struct{}) (*SettleResult, error) {
	matchedCommits := make([]string, 0, len(commitSet))
	for sha := range commitSet {
		matchedCommits = append(matchedCommits, sha)
	}
	slices.Sort(matchedCommits)

	primarySummary := map[string]interface{}{
		"total_tokens": int64(0),
		"total_cost":   float64(0),
	}
	metadataSummary := map[string]interface{}{
		"reason": reason,
	}
	validationSummary := map[string]interface{}{
		"result":     "mismatch",
		"confidence": "low",
		"reason":     reason,
	}

	return s.persistResult(
		ctx,
		pr,
		triggeredBy,
		prattributionrun.ResultClassificationAmbiguous,
		prrecord.AttributionStatusAmbiguous,
		prrecord.AttributionConfidenceLow,
		0,
		0,
		matchedCommits,
		[]string{},
		primarySummary,
		metadataSummary,
		validationSummary,
	)
}

func (s *Service) persistResult(
	ctx context.Context,
	pr *ent.PrRecord,
	triggeredBy string,
	classification prattributionrun.ResultClassification,
	attributionStatus prrecord.AttributionStatus,
	confidence prrecord.AttributionConfidence,
	tokenCount int64,
	tokenCost float64,
	matchedCommits []string,
	matchedSessions []string,
	primarySummary map[string]interface{},
	metadataSummary map[string]interface{},
	validationSummary map[string]interface{},
) (*SettleResult, error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle pr: start transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	run, err := tx.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode(prattributionrun.TriggerModeManual).
		SetTriggeredBy(triggeredBy).
		SetStatus(prattributionrun.StatusCompleted).
		SetResultClassification(classification).
		SetMatchedCommitShas(matchedCommits).
		SetMatchedSessionIds(matchedSessions).
		SetPrimaryUsageSummary(primarySummary).
		SetMetadataSummary(metadataSummary).
		SetValidationSummary(validationSummary).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle pr: create attribution run: %w", err)
	}

	now := time.Now()
	_, err = tx.PrRecord.UpdateOneID(pr.ID).
		SetAttributionStatus(attributionStatus).
		SetAttributionConfidence(confidence).
		SetPrimaryTokenCount(tokenCount).
		SetPrimaryTokenCost(tokenCost).
		SetMetadataSummary(metadataSummary).
		SetLastAttributedAt(now).
		SetLastAttributionRunID(run.ID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("settle pr: update pr record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("settle pr: commit transaction: %w", err)
	}

	return &SettleResult{
		PRRecordID:            pr.ID,
		AttributionRunID:      run.ID,
		ResultClassification:  string(classification),
		AttributionStatus:     string(attributionStatus),
		AttributionConfidence: string(confidence),
		PrimaryTokenCount:     tokenCount,
		PrimaryTokenCost:      tokenCost,
		MatchedCommitSHAs:     matchedCommits,
		MatchedSessionIDs:     matchedSessions,
		PrimaryUsageSummary:   primarySummary,
		MetadataSummary:       metadataSummary,
		ValidationSummary:     validationSummary,
	}, nil
}
