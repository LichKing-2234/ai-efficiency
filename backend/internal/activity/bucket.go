package activity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionallocationrevision"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/attributionledger"
)

func (s *Service) Bucket(ctx context.Context, actorUserID int, bucketID string) (*BucketDetail, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	actor, err := s.client.User.Query().Where(user.IDEQ(actorUserID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity bucket actor: %w", err)
	}
	bucket, err := s.client.AttributionUsageBucket.Query().Where(attributionusagebucket.BucketIDEQ(bucketID)).WithReportingInstallation().Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity bucket: %w", err)
	}
	if bucket.UserID != actorUserID && actor.Role != user.RoleAdmin {
		return nil, ErrForbidden
	}
	result := &BucketDetail{
		ContractVersion: MetricContractVersion,
		BucketID:        bucket.BucketID,
		OwnerUserID:     bucket.UserID,
		Tool:            bucket.Tool,
		Model:           bucket.Model,
		ObservedStart:   bucket.ObservedStartAt.UTC(),
		ObservedEnd:     bucket.ObservedEndAt.UTC(),
		Tokens: TokenBreakdown{
			FreshInput: bucket.FreshInputTokens, CacheRead: bucket.CacheReadTokens, CacheWrite: bucket.CacheWriteTokens,
			Output: bucket.OutputTokens, Reasoning: bucket.ReasoningTokens, ProviderTotal: bucket.ProviderTotalTokens, Processed: bucket.ProcessedTotalTokens,
		},
		TokenQuality:         string(bucket.TokenQuality),
		CoverageGapCount:     bucket.CoverageGapCount,
		ExtractorVersion:     bucket.ExtractorVersion,
		NormalizationVersion: bucket.NormalizationVersion,
		CorrelationQuality:   string(bucket.RequestCorrelationQuality),
		RequestIDs:           RequestIDDetail{State: "unlinked", Count: bucket.RequestIDCoverageCount, Evidence: []RequestIDEvidence{}},
	}
	revision, err := s.client.AttributionAllocationRevision.Query().Where(
		attributionallocationrevision.UsageBucketIDEQ(bucket.ID), latestAllocationRevisionPredicate(),
	).Only(ctx)
	if err == nil {
		result.Revision = AllocationRevisionDetail{
			RevisionID: revision.RevisionID, Sequence: revision.Sequence, Reason: revision.Reason,
			EvidenceVersion: revision.EvidenceVersion, RestatedAt: revision.RestatedAt.UTC(), Allocations: revision.Allocations,
		}
	} else if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("load activity bucket revision: %w", err)
	} else {
		result.Revision.Allocations = []map[string]any{}
	}
	if bucket.RequestIDCoverageCount <= 0 || bucket.RequestCorrelationQuality == attributionusagebucket.RequestCorrelationQualityUnlinked {
		return result, nil
	}
	if s.correlation == nil || bucket.Edges.ReportingInstallation == nil {
		result.RequestIDs.State = "unavailable"
		return result, nil
	}
	slices, err := decodeSessionSlices(bucket.SessionSlices)
	if err != nil {
		result.RequestIDs.State = "unavailable"
		return result, nil
	}
	evidence, err := s.correlation.Lookup(ctx, bucket.Edges.ReportingInstallation.InstallationID, slices)
	if err != nil {
		result.RequestIDs.State = "unavailable"
		return result, nil
	}
	if len(evidence) == 0 {
		result.RequestIDs.State = "expired"
		return result, nil
	}
	result.RequestIDs.State = "retained"
	result.RequestIDs.Count = len(evidence)
	for _, item := range evidence {
		result.RequestIDs.Evidence = append(result.RequestIDs.Evidence, RequestIDEvidence{
			RequestID: item.RequestID, ObservedAt: item.ObservedAt.UTC(), Transport: item.Transport,
			StatusCode: item.StatusCode, ErrorCategory: item.ErrorCategory, Failed: item.Failed,
		})
	}
	return result, nil
}

func decodeSessionSlices(values []map[string]any) ([]attributionledger.SessionSlice, error) {
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var slices []attributionledger.SessionSlice
	if err := json.Unmarshal(payload, &slices); err != nil {
		return nil, err
	}
	return slices, nil
}
