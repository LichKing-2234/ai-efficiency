package attributionledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionallocationrevision"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

var (
	ErrImmutableBucketConflict = errors.New("usage bucket immutable content conflicts with existing bucket")
	ErrRevisionConflict        = errors.New("allocation revision conflicts with existing revision")
	ErrAllocationForbidden     = errors.New("allocation target is not backed by an authenticated-user checkpoint")
	ErrUpgradeRequired         = errors.New("ae-cli upgrade required")
)

type Service struct {
	client      *ent.Client
	correlation *CorrelationStore
	protocol    ProtocolContract
}

type repoAccumulator struct {
	row       RepoReport
	worktrees map[string]struct{}
	branches  map[string]struct{}
	commits   map[string]*CommitReport
}

func NewService(client *ent.Client, correlation *CorrelationStore, protocol ProtocolContract) *Service {
	return &Service{client: client, correlation: correlation, protocol: protocol}
}

func (s *Service) CreateBuckets(ctx context.Context, principal InstallationPrincipal, req BatchRequest) (BatchResult, error) {
	var result BatchResult
	if s != nil && s.protocol.V1WritePolicy == V1WritePolicyUpgradeNeeded {
		return result, ErrUpgradeRequired
	}
	if s == nil || s.client == nil || principal.DatabaseID <= 0 || principal.UserID <= 0 {
		return result, fmt.Errorf("create usage buckets: service and installation principal are required")
	}
	if len(req.Buckets) == 0 || len(req.Buckets) > MaxBucketBatchSize {
		return result, fmt.Errorf("create usage buckets: batch size must be between 1 and %d", MaxBucketBatchSize)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return result, fmt.Errorf("create usage buckets: start transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txService := &Service{client: tx.Client(), correlation: s.correlation}
	for index, bucket := range req.Buckets {
		result.Accepted++
		created, duplicate, err := txService.createBucket(ctx, principal, bucket)
		if err != nil {
			return result, fmt.Errorf("create usage buckets: buckets[%d]: %w", index, err)
		}
		if created {
			result.CreatedBuckets++
		}
		if duplicate {
			result.DuplicateBuckets++
		}
		createdRevision, duplicateRevision, err := txService.createRevision(ctx, principal, bucket.BucketID, bucket.InitialRevision, true)
		if err != nil {
			return result, fmt.Errorf("create usage buckets: buckets[%d].initial_revision: %w", index, err)
		}
		if createdRevision {
			result.CreatedRevisions++
		}
		if duplicateRevision {
			result.DuplicateRevisions++
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("create usage buckets: commit: %w", err)
	}
	committed = true
	return result, nil
}

func (s *Service) CreateRevision(ctx context.Context, principal InstallationPrincipal, bucketID string, req RevisionRequest) (bool, error) {
	if s != nil && s.protocol.V1WritePolicy == V1WritePolicyUpgradeNeeded {
		return false, ErrUpgradeRequired
	}
	if req.SchemaVersion != CurrentSchemaVersion {
		return false, fmt.Errorf("unsupported schema_version %d", req.SchemaVersion)
	}
	created, _, err := s.createRevision(ctx, principal, strings.TrimSpace(bucketID), req.AllocationRevision, false)
	if err != nil {
		return created, fmt.Errorf("create allocation revision: %w", err)
	}
	return created, nil
}

func (s *Service) createBucket(ctx context.Context, principal InstallationPrincipal, bucket UsageBucket) (bool, bool, error) {
	bucket = normalizeBucket(bucket)
	if err := validateBucket(bucket); err != nil {
		return false, false, fmt.Errorf("validate usage bucket: %w", err)
	}
	digest, err := immutableBucketDigest(bucket)
	if err != nil {
		return false, false, fmt.Errorf("digest usage bucket: %w", err)
	}
	existing, err := s.client.AttributionUsageBucket.Query().
		Where(attributionusagebucket.BucketIDEQ(bucket.BucketID)).
		Only(ctx)
	if err == nil {
		if existing.ReportingInstallationID != principal.DatabaseID || existing.UserID != principal.UserID || existing.ImmutableDigest != digest {
			return false, false, fmt.Errorf("validate usage bucket immutability: %w", ErrImmutableBucketConflict)
		}
		return false, true, nil
	}
	if !ent.IsNotFound(err) {
		return false, false, fmt.Errorf("query bucket: %w", err)
	}
	correlation := CorrelationSummary{Quality: CorrelationQualityUnlinked}
	if s.correlation != nil {
		correlation, _ = s.correlation.Match(ctx, principal.InstallationID, bucket.SessionSlices)
	}
	_, err = s.client.AttributionUsageBucket.Create().
		SetBucketID(bucket.BucketID).
		SetSchemaVersion(bucket.SchemaVersion).
		SetReportingInstallationID(principal.DatabaseID).
		SetUserID(principal.UserID).
		SetTool(bucket.Tool).
		SetModel(bucket.Model).
		SetChangeSetID(bucket.ChangeSetID).
		SetSessionSlices(sessionSlicesToMaps(bucket.SessionSlices)).
		SetObservedStartAt(bucket.ObservedStart.UTC()).
		SetObservedEndAt(bucket.ObservedEnd.UTC()).
		SetFreshInputTokens(bucket.Tokens.FreshInput).
		SetCacheReadTokens(bucket.Tokens.CacheRead).
		SetCacheWriteTokens(bucket.Tokens.CacheWrite).
		SetOutputTokens(bucket.Tokens.Output).
		SetReasoningTokens(bucket.Tokens.Reasoning).
		SetProviderTotalTokens(bucket.Tokens.ProviderTotal).
		SetProcessedTotalTokens(bucket.Tokens.Processed).
		SetRequestCount(bucket.RequestCount).
		SetSourceEventCount(bucket.SourceEventCount).
		SetSourceDigest(bucket.SourceDigest).
		SetImmutableDigest(digest).
		SetExtractorVersion(bucket.ExtractorVersion).
		SetNormalizationVersion(bucket.NormalizationVersion).
		SetTokenQuality(attributionusagebucket.TokenQuality(bucket.TokenQuality)).
		SetRequestCorrelationQuality(attributionusagebucket.RequestCorrelationQuality(correlation.Quality)).
		SetRequestIDCoverageCount(correlation.RequestIDCount).
		SetRequestSetDigest(correlation.RequestSetDigest).
		SetCoverageGapCount(bucket.CoverageGapCount).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return false, false, fmt.Errorf("persist usage bucket: %w", ErrImmutableBucketConflict)
		}
		return false, false, fmt.Errorf("persist bucket: %w", err)
	}
	return true, false, nil
}

func (s *Service) createRevision(ctx context.Context, principal InstallationPrincipal, bucketID string, revision AllocationRevision, requireInitial bool) (bool, bool, error) {
	revision = normalizeRevision(revision)
	bucket, err := s.client.AttributionUsageBucket.Query().
		Where(attributionusagebucket.BucketIDEQ(bucketID)).
		Only(ctx)
	if err != nil {
		return false, false, fmt.Errorf("load usage bucket for revision: %w", err)
	}
	if bucket.ReportingInstallationID != principal.DatabaseID || bucket.UserID != principal.UserID {
		return false, false, fmt.Errorf("authorize allocation revision: %w", ErrInstallationForbidden)
	}
	if requireInitial && revision.Sequence != 1 {
		return false, false, fmt.Errorf("initial revision sequence must be 1")
	}
	bucketTokens := tokensFromEntity(bucket)
	if err := validateRevision(bucketTokens, revision); err != nil {
		return false, false, fmt.Errorf("validate allocation revision: %w", err)
	}
	if err := s.validateAllocationTargets(ctx, principal.UserID, revision.Allocations); err != nil {
		return false, false, fmt.Errorf("validate allocation revision targets: %w", err)
	}
	allocationMaps, err := allocationsToMaps(revision.Allocations)
	if err != nil {
		return false, false, fmt.Errorf("encode allocation revision: %w", err)
	}
	existing, err := s.client.AttributionAllocationRevision.Query().
		Where(attributionallocationrevision.RevisionIDEQ(revision.RevisionID)).
		Only(ctx)
	if err == nil {
		if existing.UsageBucketID != bucket.ID || existing.Sequence != revision.Sequence || existing.Reason != revision.Reason || existing.EvidenceVersion != revision.EvidenceVersion || !reflect.DeepEqual(existing.Allocations, allocationMaps) {
			return false, false, fmt.Errorf("validate allocation revision immutability: %w", ErrRevisionConflict)
		}
		return false, true, nil
	}
	if !ent.IsNotFound(err) {
		return false, false, fmt.Errorf("query allocation revision: %w", err)
	}
	latest, err := s.client.AttributionAllocationRevision.Query().
		Where(attributionallocationrevision.UsageBucketIDEQ(bucket.ID)).
		Order(ent.Desc(attributionallocationrevision.FieldSequence)).
		First(ctx)
	if err == nil && revision.Sequence <= latest.Sequence {
		return false, false, fmt.Errorf("revision sequence %d must be newer than %d", revision.Sequence, latest.Sequence)
	}
	if err != nil && !ent.IsNotFound(err) {
		return false, false, fmt.Errorf("query latest allocation revision: %w", err)
	}
	create := s.client.AttributionAllocationRevision.Create().
		SetRevisionID(revision.RevisionID).
		SetUsageBucketID(bucket.ID).
		SetSequence(revision.Sequence).
		SetReason(revision.Reason).
		SetEvidenceVersion(revision.EvidenceVersion).
		SetAllocations(allocationMaps)
	if !revision.RestatedAt.IsZero() {
		create.SetRestatedAt(revision.RestatedAt.UTC())
	}
	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return false, false, fmt.Errorf("persist allocation revision: %w", ErrRevisionConflict)
		}
		return false, false, fmt.Errorf("persist allocation revision: %w", err)
	}
	return true, false, nil
}

func (s *Service) validateAllocationTargets(ctx context.Context, userID int, allocations []Allocation) error {
	for _, allocation := range allocations {
		if !isBoundStatus(allocation.Target.Status) {
			continue
		}
		exists, err := s.client.CommitCheckpoint.Query().Where(
			commitcheckpoint.UserIDEQ(userID),
			commitcheckpoint.RepoConfigIDEQ(allocation.Target.RepoConfigID),
			commitcheckpoint.WorkspaceIDEQ(allocation.Target.WorkspaceID),
			commitcheckpoint.CommitShaEQ(allocation.Target.CommitSHA),
		).Exist(ctx)
		if err != nil {
			return fmt.Errorf("validate allocation checkpoint: %w", err)
		}
		if !exists {
			exists, err = s.client.CommitRewrite.Query().Where(
				commitrewrite.UserIDEQ(userID),
				commitrewrite.RepoConfigIDEQ(allocation.Target.RepoConfigID),
				commitrewrite.WorkspaceIDEQ(allocation.Target.WorkspaceID),
				commitrewrite.NewCommitShaEQ(allocation.Target.CommitSHA),
			).Exist(ctx)
			if err != nil {
				return fmt.Errorf("validate allocation rewrite: %w", err)
			}
			if !exists {
				return ErrAllocationForbidden
			}
		}
		for _, inherited := range allocation.Target.InheritedCommits {
			if err := s.validateCommitReference(ctx, userID, inherited); err != nil {
				return fmt.Errorf("validate inherited allocation target: %w", err)
			}
		}
	}
	return nil
}

func (s *Service) validateCommitReference(ctx context.Context, userID int, reference CommitReference) error {
	exists, err := s.client.CommitCheckpoint.Query().Where(
		commitcheckpoint.UserIDEQ(userID),
		commitcheckpoint.RepoConfigIDEQ(reference.RepoConfigID),
		commitcheckpoint.WorkspaceIDEQ(reference.WorkspaceID),
		commitcheckpoint.CommitShaEQ(reference.CommitSHA),
	).Exist(ctx)
	if err != nil {
		return fmt.Errorf("validate inherited commit checkpoint: %w", err)
	}
	if exists {
		return nil
	}
	exists, err = s.client.CommitRewrite.Query().Where(
		commitrewrite.UserIDEQ(userID),
		commitrewrite.RepoConfigIDEQ(reference.RepoConfigID),
		commitrewrite.WorkspaceIDEQ(reference.WorkspaceID),
		commitrewrite.NewCommitShaEQ(reference.CommitSHA),
	).Exist(ctx)
	if err != nil {
		return fmt.Errorf("validate inherited commit rewrite: %w", err)
	}
	if !exists {
		return ErrAllocationForbidden
	}
	return nil
}

func normalizeBucket(bucket UsageBucket) UsageBucket {
	bucket.BucketID = strings.TrimSpace(bucket.BucketID)
	bucket.Tool = strings.ToLower(strings.TrimSpace(bucket.Tool))
	bucket.Model = strings.TrimSpace(bucket.Model)
	bucket.ChangeSetID = strings.TrimSpace(bucket.ChangeSetID)
	bucket.SourceDigest = strings.TrimSpace(bucket.SourceDigest)
	bucket.ExtractorVersion = strings.TrimSpace(bucket.ExtractorVersion)
	bucket.TokenQuality = TokenQuality(strings.TrimSpace(string(bucket.TokenQuality)))
	for index := range bucket.SessionSlices {
		bucket.SessionSlices[index].ConversationID = strings.TrimSpace(bucket.SessionSlices[index].ConversationID)
		bucket.SessionSlices[index].AtomSetDigest = strings.TrimSpace(bucket.SessionSlices[index].AtomSetDigest)
	}
	return bucket
}

func normalizeRevision(revision AllocationRevision) AllocationRevision {
	revision.RevisionID = strings.TrimSpace(revision.RevisionID)
	revision.Reason = strings.TrimSpace(revision.Reason)
	revision.EvidenceVersion = strings.TrimSpace(revision.EvidenceVersion)
	for index := range revision.Allocations {
		target := &revision.Allocations[index].Target
		target.Status = AllocationStatus(strings.TrimSpace(string(target.Status)))
		target.RepoKey = strings.TrimSpace(target.RepoKey)
		target.WorkspaceID = strings.TrimSpace(target.WorkspaceID)
		target.CommitSHA = strings.TrimSpace(target.CommitSHA)
		target.Branch = strings.TrimSpace(target.Branch)
		target.Lineage = strings.TrimSpace(target.Lineage)
		sort.Ints(target.AssociatedRepoConfigIDs)
		for inheritedIndex := range target.InheritedCommits {
			inherited := &target.InheritedCommits[inheritedIndex]
			inherited.RepoKey = strings.TrimSpace(inherited.RepoKey)
			inherited.WorkspaceID = strings.TrimSpace(inherited.WorkspaceID)
			inherited.CommitSHA = strings.TrimSpace(inherited.CommitSHA)
			inherited.Branch = strings.TrimSpace(inherited.Branch)
			inherited.Lineage = strings.TrimSpace(inherited.Lineage)
		}
		sort.Slice(target.InheritedCommits, func(i, j int) bool {
			return commitReferenceKey(target.InheritedCommits[i]) < commitReferenceKey(target.InheritedCommits[j])
		})
	}
	return revision
}

func validateBucket(bucket UsageBucket) error {
	if bucket.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", bucket.SchemaVersion)
	}
	if bucket.BucketID == "" || bucket.Tool != "codex" || bucket.SourceDigest == "" || bucket.ExtractorVersion == "" {
		return errors.New("bucket identity, Codex tool, source_digest, and extractor_version are required")
	}
	if bucket.ObservedStart.IsZero() || bucket.ObservedEnd.Before(bucket.ObservedStart) || len(bucket.SessionSlices) == 0 {
		return errors.New("bucket observation window and session_slices are required")
	}
	if bucket.NormalizationVersion < 1 || bucket.RequestCount < 0 || bucket.SourceEventCount < 1 || bucket.CoverageGapCount < 0 {
		return errors.New("bucket counts and normalization_version are invalid")
	}
	if bucket.TokenQuality != TokenQualityMeasured && bucket.TokenQuality != TokenQualityHistoricalAdvisory && bucket.TokenQuality != TokenQualityInvalid {
		return errors.New("invalid token_quality")
	}
	if err := validateTokens(bucket.Tokens, bucket.TokenQuality == TokenQualityInvalid); err != nil {
		return fmt.Errorf("validate bucket tokens: %w", err)
	}
	for _, slice := range bucket.SessionSlices {
		if slice.ConversationID == "" || slice.AtomSetDigest == "" || slice.TokenAtomCount < 1 || slice.ObservedStart.IsZero() || slice.ObservedEnd.Before(slice.ObservedStart) {
			return errors.New("session slice is invalid")
		}
	}
	return nil
}

func validateTokens(tokens Tokens, allowZero bool) error {
	if tokens.FreshInput < 0 || tokens.CacheRead < 0 || tokens.CacheWrite < 0 || tokens.Output < 0 || tokens.Reasoning < 0 || tokens.ProviderTotal < 0 || tokens.Processed < 0 {
		return errors.New("token values cannot be negative")
	}
	if tokens.Reasoning > tokens.Output {
		return errors.New("reasoning_tokens must be a subset of output_tokens")
	}
	processed := tokens.FreshInput + tokens.CacheRead + tokens.CacheWrite + tokens.Output
	if tokens.Processed != processed {
		return fmt.Errorf("processed_total_tokens=%d does not equal normalized components=%d", tokens.Processed, processed)
	}
	if !allowZero && tokens.Processed == 0 {
		return errors.New("measured bucket must contain processed tokens")
	}
	return nil
}

func validateRevision(bucketTokens Tokens, revision AllocationRevision) error {
	if revision.RevisionID == "" || revision.Sequence < 1 || revision.Reason == "" || revision.EvidenceVersion == "" || len(revision.Allocations) == 0 {
		return errors.New("revision identity and allocations are required")
	}
	var allocated Tokens
	for _, allocation := range revision.Allocations {
		if err := validateTokens(allocation.Tokens, bucketTokens.Processed == 0); err != nil {
			return fmt.Errorf("invalid allocation: %w", err)
		}
		if err := validateTarget(allocation.Target); err != nil {
			return fmt.Errorf("invalid allocation target: %w", err)
		}
		allocated = allocated.Add(allocation.Tokens)
	}
	if !reflect.DeepEqual(allocated, bucketTokens) {
		return fmt.Errorf("allocation does not conserve bucket tokens: got %+v want %+v", allocated, bucketTokens)
	}
	return nil
}

func validateTarget(target AllocationTarget) error {
	switch target.Status {
	case AllocationStatusBoundAuto, AllocationStatusBoundManual:
		if target.RepoConfigID <= 0 || target.RepoKey == "" || target.WorkspaceID == "" || target.CommitSHA == "" {
			return errors.New("bound allocation requires repo_config_id, repo_key, workspace_id, and commit_sha")
		}
		for _, inherited := range target.InheritedCommits {
			if err := validateCommitReference(inherited); err != nil {
				return fmt.Errorf("invalid inherited commit target: %w", err)
			}
		}
	case AllocationStatusUnbound, AllocationStatusOverhead, AllocationStatusOutOfScope, AllocationStatusMultiRepoShared:
		if target.CommitSHA != "" || len(target.InheritedCommits) > 0 {
			return errors.New("unbound allocation cannot carry commit lineage")
		}
	default:
		return fmt.Errorf("unsupported allocation status %q", target.Status)
	}
	return nil
}

func validateCommitReference(reference CommitReference) error {
	if reference.RepoConfigID <= 0 || reference.RepoKey == "" || reference.WorkspaceID == "" || reference.CommitSHA == "" || reference.Lineage == "" {
		return errors.New("inherited commit requires repo_config_id, repo_key, workspace_id, commit_sha, and lineage")
	}
	return nil
}

func commitReferenceKey(reference CommitReference) string {
	return strings.Join([]string{
		fmt.Sprintf("%d", reference.RepoConfigID), reference.RepoKey, reference.WorkspaceID,
		reference.CommitSHA, reference.Branch, reference.Lineage,
	}, "\x1f")
}

func isBoundStatus(status AllocationStatus) bool {
	return status == AllocationStatusBoundAuto || status == AllocationStatusBoundManual
}

func immutableBucketDigest(bucket UsageBucket) (string, error) {
	bucket.InitialRevision = AllocationRevision{}
	payload, err := json.Marshal(bucket)
	if err != nil {
		return "", fmt.Errorf("marshal immutable bucket: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func sessionSlicesToMaps(slices []SessionSlice) []map[string]any {
	payload, _ := json.Marshal(slices)
	var result []map[string]any
	_ = json.Unmarshal(payload, &result)
	return result
}

func allocationsToMaps(allocations []Allocation) ([]map[string]any, error) {
	payload, err := json.Marshal(allocations)
	if err != nil {
		return nil, fmt.Errorf("marshal allocations: %w", err)
	}
	var result []map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("decode allocations map: %w", err)
	}
	return result, nil
}

func allocationsFromMaps(maps []map[string]any) ([]Allocation, error) {
	payload, err := json.Marshal(maps)
	if err != nil {
		return nil, fmt.Errorf("marshal allocation maps: %w", err)
	}
	var result []Allocation
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("decode allocations: %w", err)
	}
	return result, nil
}

func tokensFromEntity(bucket *ent.AttributionUsageBucket) Tokens {
	return Tokens{
		FreshInput:    bucket.FreshInputTokens,
		CacheRead:     bucket.CacheReadTokens,
		CacheWrite:    bucket.CacheWriteTokens,
		Output:        bucket.OutputTokens,
		Reasoning:     bucket.ReasoningTokens,
		ProviderTotal: bucket.ProviderTotalTokens,
		Processed:     bucket.ProcessedTotalTokens,
	}
}

// RefreshCorrelation applies late OTLP evidence to compact bucket metadata.
// Token totals and Git allocations remain immutable.
func (s *Service) RefreshCorrelation(ctx context.Context, principal InstallationPrincipal, evidence []RequestEvidence) (int, error) {
	if s == nil || s.client == nil || s.correlation == nil || principal.DatabaseID <= 0 || principal.UserID <= 0 {
		return 0, nil
	}
	conversationIDs := map[string]struct{}{}
	var from, to time.Time
	for _, item := range evidence {
		conversationID := strings.TrimSpace(item.ConversationID)
		if conversationID == "" || item.ObservedAt.IsZero() {
			continue
		}
		conversationIDs[conversationID] = struct{}{}
		observedAt := item.ObservedAt.UTC()
		if from.IsZero() || observedAt.Before(from) {
			from = observedAt
		}
		if to.IsZero() || observedAt.After(to) {
			to = observedAt
		}
	}
	if len(conversationIDs) == 0 {
		return 0, nil
	}
	buckets, err := s.client.AttributionUsageBucket.Query().Where(
		attributionusagebucket.ReportingInstallationIDEQ(principal.DatabaseID),
		attributionusagebucket.UserIDEQ(principal.UserID),
		attributionusagebucket.ObservedEndAtGTE(from.Add(-2*time.Minute)),
		attributionusagebucket.ObservedStartAtLTE(to.Add(2*time.Minute)),
	).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("refresh request correlation: query buckets: %w", err)
	}
	updated := 0
	for _, bucket := range buckets {
		slices, err := sessionSlicesFromMaps(bucket.SessionSlices)
		if err != nil || !slicesContainConversation(slices, conversationIDs) {
			continue
		}
		summary, err := s.correlation.Match(ctx, principal.InstallationID, slices)
		if err != nil {
			return updated, fmt.Errorf("refresh request correlation: match bucket %s: %w", bucket.BucketID, err)
		}
		if summary.RequestIDCount == 0 || (summary.RequestIDCount == bucket.RequestIDCoverageCount && summary.RequestSetDigest == bucket.RequestSetDigest && summary.Quality == CorrelationQuality(bucket.RequestCorrelationQuality)) {
			continue
		}
		update := bucket.Update().
			SetRequestCorrelationQuality(attributionusagebucket.RequestCorrelationQuality(summary.Quality)).
			SetRequestIDCoverageCount(summary.RequestIDCount).
			SetCorrelationUpdatedAt(time.Now().UTC())
		if summary.RequestSetDigest == "" {
			update.ClearRequestSetDigest()
		} else {
			update.SetRequestSetDigest(summary.RequestSetDigest)
		}
		if err := update.Exec(ctx); err != nil {
			return updated, fmt.Errorf("refresh request correlation: update bucket %s: %w", bucket.BucketID, err)
		}
		updated++
	}
	return updated, nil
}

func sessionSlicesFromMaps(maps []map[string]any) ([]SessionSlice, error) {
	payload, err := json.Marshal(maps)
	if err != nil {
		return nil, fmt.Errorf("marshal session slice maps: %w", err)
	}
	var result []SessionSlice
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("decode session slices: %w", err)
	}
	return result, nil
}

func slicesContainConversation(slices []SessionSlice, conversations map[string]struct{}) bool {
	for _, slice := range slices {
		if _, ok := conversations[slice.ConversationID]; ok {
			return true
		}
	}
	return false
}

func (s *Service) Report(ctx context.Context, userID int, from, to time.Time) (Report, error) {
	result := Report{From: from.UTC(), To: to.UTC(), Repositories: []RepoReport{}, Buckets: []BucketReport{}}
	buckets, err := s.client.AttributionUsageBucket.Query().Where(
		attributionusagebucket.UserIDEQ(userID),
		attributionusagebucket.ObservedEndAtGTE(from.UTC()),
		attributionusagebucket.ObservedEndAtLT(to.UTC()),
	).WithAllocationRevisions(func(query *ent.AttributionAllocationRevisionQuery) {
		query.Order(ent.Desc(attributionallocationrevision.FieldSequence))
	}).All(ctx)
	if err != nil {
		return result, fmt.Errorf("query attribution report: %w", err)
	}
	result.BucketCount = len(buckets)
	repos := map[int]*repoAccumulator{}
	commitSHAs := map[string]struct{}{}
	for _, bucket := range buckets {
		result.CoverageGapCount += bucket.CoverageGapCount
		result.RequestIDCoverage += bucket.RequestIDCoverageCount
		switch bucket.RequestCorrelationQuality {
		case attributionusagebucket.RequestCorrelationQualityExact:
			result.Evidence.ExactCorrelationBuckets++
		case attributionusagebucket.RequestCorrelationQualityAdvisory:
			result.Evidence.AdvisoryCorrelationBuckets++
		default:
			result.Evidence.UnlinkedCorrelationBuckets++
		}
		var allocations []Allocation
		allocationStatus := string(AllocationStatusUnbound)
		revisionSequence := 0
		revisionReason := ""
		if len(bucket.Edges.AllocationRevisions) > 0 {
			latest := bucket.Edges.AllocationRevisions[0]
			allocations, err = allocationsFromMaps(latest.Allocations)
			if err != nil {
				return result, fmt.Errorf("build attribution report: decode bucket %s allocations: %w", bucket.BucketID, err)
			}
			revisionSequence = latest.Sequence
			revisionReason = latest.Reason
			statuses := map[string]struct{}{}
			for _, allocation := range allocations {
				statuses[string(allocation.Target.Status)] = struct{}{}
			}
			allocationStatus = strings.Join(sortedKeys(statuses), ",")
		}
		result.Buckets = append(result.Buckets, BucketReport{
			BucketID: bucket.BucketID, Tool: bucket.Tool, Model: bucket.Model,
			ObservedStart: bucket.ObservedStartAt, ObservedEnd: bucket.ObservedEndAt,
			Tokens: tokensFromEntity(bucket), RequestCount: bucket.RequestCount,
			TokenQuality: TokenQuality(bucket.TokenQuality), CorrelationQuality: CorrelationQuality(bucket.RequestCorrelationQuality),
			RequestIDCoverageCount: bucket.RequestIDCoverageCount, CoverageGapCount: bucket.CoverageGapCount,
			AllocationStatus: allocationStatus, AllocationRevision: revisionSequence, AllocationRevisionReason: revisionReason,
		})
		if bucket.TokenQuality == attributionusagebucket.TokenQualityHistoricalAdvisory {
			result.Evidence.HistoricalAdvisoryBuckets++
			result.HistoricalAdvisory += bucket.ProcessedTotalTokens
			continue
		}
		if bucket.TokenQuality == attributionusagebucket.TokenQualityInvalid {
			result.Evidence.InvalidBuckets++
			continue
		}
		result.Evidence.MeasuredBuckets++
		result.MeasuredTokens += bucket.ProcessedTotalTokens
		if len(allocations) == 0 {
			result.UnboundTokens += bucket.ProcessedTotalTokens
			continue
		}
		for _, allocation := range allocations {
			if !isBoundStatus(allocation.Target.Status) {
				result.UnboundTokens += allocation.Tokens.Processed
				if allocation.Target.Status == AllocationStatusMultiRepoShared {
					result.SharedTokens += allocation.Tokens.Processed
					seenRepoIDs := map[int]struct{}{}
					for _, repoID := range allocation.Target.AssociatedRepoConfigIDs {
						if repoID <= 0 {
							continue
						}
						if _, ok := seenRepoIDs[repoID]; ok {
							continue
						}
						seenRepoIDs[repoID] = struct{}{}
						ensureRepoAccumulator(repos, repoID, "").row.SharedTokens += allocation.Tokens.Processed
					}
					continue
				}
				repoID := allocation.Target.RepoConfigID
				if repoID <= 0 {
					repoID = 0
				}
				unbound := ensureRepoAccumulator(repos, repoID, allocation.Target.RepoKey)
				if repoID == 0 {
					unbound.row.Name = "未归属"
					unbound.row.RepoKey = "unbound"
				}
				unbound.row.UnboundTokens += allocation.Tokens.Processed
				if allocation.Target.WorkspaceID != "" {
					unbound.worktrees[allocation.Target.WorkspaceID] = struct{}{}
				}
				if allocation.Target.Branch != "" {
					unbound.branches[allocation.Target.Branch] = struct{}{}
				}
				continue
			}
			result.BoundTokens += allocation.Tokens.Processed
			acc := ensureRepoAccumulator(repos, allocation.Target.RepoConfigID, allocation.Target.RepoKey)
			acc.row.Tokens += allocation.Tokens.Processed
			acc.worktrees[allocation.Target.WorkspaceID] = struct{}{}
			if allocation.Target.Branch != "" {
				acc.branches[allocation.Target.Branch] = struct{}{}
			}
			commit := acc.commits[allocation.Target.CommitSHA]
			if commit == nil {
				commit = &CommitReport{CommitSHA: allocation.Target.CommitSHA, Lineage: allocation.Target.Lineage, InheritedFromCommitSHAs: []string{}, PRs: []PRReport{}}
				acc.commits[allocation.Target.CommitSHA] = commit
			}
			commit.Tokens += allocation.Tokens.Processed
			commitSHAs[allocation.Target.CommitSHA] = struct{}{}
			for _, inherited := range allocation.Target.InheritedCommits {
				inheritedAcc := ensureRepoAccumulator(repos, inherited.RepoConfigID, inherited.RepoKey)
				inheritedAcc.row.InheritedTokens += allocation.Tokens.Processed
				inheritedAcc.worktrees[inherited.WorkspaceID] = struct{}{}
				if inherited.Branch != "" {
					inheritedAcc.branches[inherited.Branch] = struct{}{}
				}
				inheritedCommit := inheritedAcc.commits[inherited.CommitSHA]
				if inheritedCommit == nil {
					inheritedCommit = &CommitReport{CommitSHA: inherited.CommitSHA, Lineage: inherited.Lineage, InheritedFromCommitSHAs: []string{}, PRs: []PRReport{}}
					inheritedAcc.commits[inherited.CommitSHA] = inheritedCommit
				}
				inheritedCommit.InheritedTokens += allocation.Tokens.Processed
				inheritedCommit.InheritedFromCommitSHAs = appendUniqueString(inheritedCommit.InheritedFromCommitSHAs, allocation.Target.CommitSHA)
				commitSHAs[inherited.CommitSHA] = struct{}{}
			}
		}
	}
	if result.MeasuredTokens > 0 {
		result.AllocationRate = float64(result.BoundTokens) / float64(result.MeasuredTokens)
	}
	if err := s.enrichRepoNames(ctx, repos); err != nil {
		return result, fmt.Errorf("build attribution report: %w", err)
	}
	if err := s.enrichPRs(ctx, repos, commitSHAs); err != nil {
		return result, fmt.Errorf("build attribution report: %w", err)
	}
	ids := make([]int, 0, len(repos))
	for id := range repos {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		acc := repos[id]
		acc.row.ProcessedTokens = acc.row.Tokens + acc.row.UnboundTokens
		acc.row.Worktrees = sortedKeys(acc.worktrees)
		acc.row.Branches = sortedKeys(acc.branches)
		commitKeys := make([]string, 0, len(acc.commits))
		for sha := range acc.commits {
			commitKeys = append(commitKeys, sha)
		}
		sort.Strings(commitKeys)
		for _, sha := range commitKeys {
			sort.Strings(acc.commits[sha].InheritedFromCommitSHAs)
			acc.row.Commits = append(acc.row.Commits, *acc.commits[sha])
		}
		result.Repositories = append(result.Repositories, acc.row)
	}
	return result, nil
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func ensureRepoAccumulator(repos map[int]*repoAccumulator, repoID int, repoKey string) *repoAccumulator {
	acc := repos[repoID]
	if acc != nil {
		if acc.row.RepoKey == "" {
			acc.row.RepoKey = repoKey
		}
		return acc
	}
	acc = &repoAccumulator{
		row:       RepoReport{RepoConfigID: repoID, RepoKey: repoKey, Commits: []CommitReport{}},
		worktrees: map[string]struct{}{},
		branches:  map[string]struct{}{},
		commits:   map[string]*CommitReport{},
	}
	repos[repoID] = acc
	return acc
}

func (s *Service) enrichRepoNames(ctx context.Context, repos map[int]*repoAccumulator) error {
	ids := make([]int, 0, len(repos))
	for id := range repos {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.client.RepoConfig.Query().Where(repoconfig.IDIn(ids...)).All(ctx)
	if err != nil {
		return fmt.Errorf("load attribution repositories: %w", err)
	}
	for _, row := range rows {
		if acc := repos[row.ID]; acc != nil {
			acc.row.Name = row.FullName
			if acc.row.RepoKey == "" {
				acc.row.RepoKey = row.RepoKey
			}
		}
	}
	return nil
}

func (s *Service) enrichPRs(ctx context.Context, repos map[int]*repoAccumulator, commitSHAs map[string]struct{}) error {
	if len(commitSHAs) == 0 {
		return nil
	}
	shas := sortedKeys(commitSHAs)
	rows, err := s.client.PRCommitUsageSnapshot.Query().
		Where(prcommitusagesnapshot.CommitShaIn(shas...)).
		WithPrRecord(func(query *ent.PrRecordQuery) {
			query.WithRepoConfig()
		}).
		All(ctx)
	if err != nil {
		return fmt.Errorf("load attribution PR projections: %w", err)
	}
	for _, row := range rows {
		pr := row.Edges.PrRecord
		if pr == nil || pr.Edges.RepoConfig == nil {
			continue
		}
		acc := repos[pr.Edges.RepoConfig.ID]
		if acc == nil || acc.commits[row.CommitSha] == nil {
			continue
		}
		acc.commits[row.CommitSha].PRs = append(acc.commits[row.CommitSha].PRs, PRReport{
			ID: pr.ID, SCMPRID: pr.ScmPrID, Title: pr.Title, URL: pr.ScmPrURL, Status: string(pr.Status),
		})
	}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
