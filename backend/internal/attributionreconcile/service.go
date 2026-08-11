package attributionreconcile

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionclaimgroup"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/internal/attributionpool"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	DefaultBatchSize   = 20
	DefaultConcurrency = 4
	DefaultLeaseTTL    = 2 * time.Minute
	DefaultInterval    = 30 * time.Second
	FinalAttemptLead   = 24 * time.Hour
)

type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type Metrics interface {
	SetAttributionHealth(pending int, oldestPendingAge time.Duration, nearExpiry int)
	ObserveAttributionReconciliation(outcome string, age time.Duration)
	AddAttributionLifecycle(operation, outcome string, count int)
}

type noopMetrics struct{}

func (noopMetrics) SetAttributionHealth(int, time.Duration, int)           {}
func (noopMetrics) ObserveAttributionReconciliation(string, time.Duration) {}
func (noopMetrics) AddAttributionLifecycle(string, string, int)            {}

type Options struct {
	BatchSize   int
	Concurrency int
	LeaseTTL    time.Duration
	Now         func() time.Time
	RandFloat64 func() float64
	Metrics     Metrics
}

type Service struct {
	client      *ent.Client
	resolver    ProviderResolver
	logger      *zap.Logger
	batchSize   int
	concurrency int
	leaseTTL    time.Duration
	now         func() time.Time
	randFloat64 func() float64
	metrics     Metrics
}

func NewService(client *ent.Client, resolver ProviderResolver, logger *zap.Logger, options Options) (*Service, error) {
	if client == nil || resolver == nil || logger == nil {
		return nil, fmt.Errorf("attribution reconciler client, provider resolver, and logger are required")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = DefaultBatchSize
	}
	if options.Concurrency <= 0 {
		options.Concurrency = DefaultConcurrency
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = DefaultLeaseTTL
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RandFloat64 == nil {
		options.RandFloat64 = rand.Float64
	}
	if options.Metrics == nil {
		options.Metrics = noopMetrics{}
	}
	return &Service{client: client, resolver: resolver, logger: logger, batchSize: options.BatchSize, concurrency: options.Concurrency, leaseTTL: options.LeaseTTL, now: options.Now, randFloat64: options.RandFloat64, metrics: options.Metrics}, nil
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(DefaultInterval)
	defer ticker.Stop()
	for {
		if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("v2 attribution reconciliation pass failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) RunOnce(ctx context.Context) (int, error) {
	now := s.now().UTC()
	candidates, err := s.client.AttributionRequestClaim.Query().Where(
		attributionrequestclaim.StatusIn(attributionrequestclaim.StatusPending, attributionrequestclaim.StatusProviderUnavailable),
		attributionrequestclaim.ExpiresAtLTE(now.Add(FinalAttemptLead)),
		attributionrequestclaim.Or(attributionrequestclaim.LeaseExpiresAtIsNil(), attributionrequestclaim.LeaseExpiresAtLTE(now)),
	).Order(ent.Asc(attributionrequestclaim.FieldExpiresAt), ent.Asc(attributionrequestclaim.FieldID)).Limit(s.batchSize).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query final reconciliation candidates: %w", err)
	}
	if remaining := s.batchSize - len(candidates); remaining > 0 {
		ordinary, err := s.client.AttributionRequestClaim.Query().Where(
			attributionrequestclaim.StatusIn(attributionrequestclaim.StatusPending, attributionrequestclaim.StatusProviderUnavailable),
			attributionrequestclaim.ExpiresAtGT(now.Add(FinalAttemptLead)),
			attributionrequestclaim.NextAttemptAtLTE(now),
			attributionrequestclaim.Or(attributionrequestclaim.LeaseExpiresAtIsNil(), attributionrequestclaim.LeaseExpiresAtLTE(now)),
		).Order(ent.Asc(attributionrequestclaim.FieldNextAttemptAt), ent.Asc(attributionrequestclaim.FieldID)).Limit(remaining).All(ctx)
		if err != nil {
			return 0, fmt.Errorf("query ordinary reconciliation candidates: %w", err)
		}
		candidates = append(candidates, ordinary...)
	}
	sem := make(chan struct{}, s.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	processed := 0
	var firstErr error
	for _, candidate := range candidates {
		candidate := candidate
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			claimed, err := s.reconcileCandidate(ctx, candidate)
			if claimed && err == nil {
				current, queryErr := s.client.AttributionRequestClaim.Get(ctx, candidate.ID)
				if queryErr == nil {
					age := s.now().UTC().Sub(current.CreatedAt)
					if age < 0 {
						age = 0
					}
					s.metrics.ObserveAttributionReconciliation(string(current.Status), age)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if claimed {
				processed++
			}
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}()
	}
	wg.Wait()
	if err := s.finalizeDueGroups(ctx, now); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.cleanupExpiredGroups(ctx, now); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.recordHealth(ctx, now); err != nil && firstErr == nil {
		firstErr = err
	}
	return processed, firstErr
}

func (s *Service) reconcileCandidate(ctx context.Context, candidate *ent.AttributionRequestClaim) (bool, error) {
	now := s.now().UTC()
	token := uuid.NewString()
	leaseUpdate := s.client.AttributionRequestClaim.Update().Where(
		attributionrequestclaim.IDEQ(candidate.ID),
		attributionrequestclaim.StatusIn(attributionrequestclaim.StatusPending, attributionrequestclaim.StatusProviderUnavailable),
		attributionrequestclaim.Or(
			attributionrequestclaim.NextAttemptAtLTE(now),
			attributionrequestclaim.ExpiresAtLTE(now.Add(FinalAttemptLead)),
		),
		attributionrequestclaim.Or(attributionrequestclaim.LeaseExpiresAtIsNil(), attributionrequestclaim.LeaseExpiresAtLTE(now)),
	).SetLeaseToken(token).SetLeaseExpiresAt(now.Add(s.leaseTTL)).AddAttemptCount(1)
	updated, err := leaseUpdate.Save(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire request claim %d lease: %w", candidate.ID, err)
	}
	if updated == 0 {
		return false, nil
	}
	attempt := candidate.AttemptCount + 1
	if _, status, code := s.currentOwnerIdentity(ctx, candidate); code != "" {
		return true, s.finish(ctx, candidate.ID, token, status, code)
	}
	providerRow, err := s.client.RelayProvider.Get(ctx, candidate.RelayProviderID)
	if err != nil || !providerRow.Enabled {
		return true, s.retry(ctx, candidate, token, attempt, "provider_unavailable", err)
	}
	provider, err := s.resolver.Resolve(ctx, candidate.RelayProviderID)
	if err != nil {
		return true, s.retry(ctx, candidate, token, attempt, "provider_unavailable", err)
	}
	reader, ok := provider.(relay.RequestUsageReader)
	if !ok {
		return true, s.retry(ctx, candidate, token, attempt, "provider_unsupported", nil)
	}
	rows, err := reader.ReadRequestUsage(ctx, candidate.RequestID, 2)
	if err != nil {
		return true, s.retry(ctx, candidate, token, attempt, "read_error", err)
	}
	switch len(rows) {
	case 0:
		return true, s.retryPending(ctx, candidate, token, attempt)
	case 1:
		return true, s.reconcileOne(ctx, candidate, token, attempt, rows[0])
	default:
		return true, s.finish(ctx, candidate.ID, token, attributionrequestclaim.StatusAmbiguous, "ambiguous_request")
	}
}

func (s *Service) reconcileOne(ctx context.Context, candidate *ent.AttributionRequestClaim, token string, attempt int, usage relay.RequestUsage) error {
	providerRow, err := s.client.RelayProvider.Get(ctx, candidate.RelayProviderID)
	if err != nil || !providerRow.Enabled {
		return s.retry(ctx, candidate, token, attempt, "provider_unavailable", err)
	}
	currentRelayUserID, status, code := s.currentOwnerIdentity(ctx, candidate)
	if code != "" {
		return s.finish(ctx, candidate.ID, token, status, code)
	}
	if currentRelayUserID != usage.UserID {
		return s.finish(ctx, candidate.ID, token, attributionrequestclaim.StatusOwnerMismatch, "owner_mismatch")
	}
	total, valid := normalizeUsage(candidate.RequestID, usage)
	if !valid {
		return s.finish(ctx, candidate.ID, token, attributionrequestclaim.StatusInvalidUsage, "invalid_usage")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin reconciliation materialization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Allocation ingest updates this row before rematerializing the group. A
	// no-op update takes the same row lock, so reconciliation cannot snapshot an
	// older allocation while a newer revision is being committed.
	if err := lockClaimGroup(ctx, tx.Client(), candidate.ClaimGroupID); err != nil {
		return err
	}
	updated, err := tx.Client().AttributionRequestClaim.Update().Where(
		attributionrequestclaim.IDEQ(candidate.ID), attributionrequestclaim.LeaseTokenEQ(token),
	).SetStatus(attributionrequestclaim.StatusReconciled).SetRequestedModel(strings.TrimSpace(usage.RequestedModel)).SetUsageAt(usage.UsageAt.UTC()).
		SetInputTokens(usage.InputTokens).SetOutputTokens(usage.OutputTokens).SetCacheCreationTokens(usage.CacheCreationTokens).SetCacheReadTokens(usage.CacheReadTokens).
		SetTotalTokens(total).SetReconciledAt(s.now().UTC()).SetLastErrorCode("").ClearLeaseToken().ClearLeaseExpiresAt().Save(ctx)
	if err != nil {
		return fmt.Errorf("persist reconciled request claim: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("request claim lease was lost before reconciliation")
	}
	if err := attributionpool.MaterializeRequestClaim(ctx, tx.Client(), candidate.ID, s.now().UTC()); err != nil {
		return fmt.Errorf("materialize reconciled request claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reconciled request claim: %w", err)
	}
	return nil
}

func lockClaimGroup(ctx context.Context, client *ent.Client, groupID int) error {
	locked, err := client.AttributionClaimGroup.Update().Where(
		attributionclaimgroup.IDEQ(groupID),
	).AddRequestCount(0).Save(ctx)
	if err != nil {
		return fmt.Errorf("lock claim group for reconciliation: %w", err)
	}
	if locked != 1 {
		return fmt.Errorf("claim group disappeared before reconciliation")
	}
	return nil
}

func (s *Service) currentOwnerIdentity(ctx context.Context, candidate *ent.AttributionRequestClaim) (int64, attributionrequestclaim.Status, string) {
	group, err := s.client.AttributionClaimGroup.Get(ctx, candidate.ClaimGroupID)
	if err != nil {
		return 0, attributionrequestclaim.StatusInvalidUsage, "missing_claim_group"
	}
	if group.RelayProviderID != candidate.RelayProviderID {
		return 0, attributionrequestclaim.StatusInvalidUsage, "provider_mismatch"
	}
	owner, err := s.client.User.Get(ctx, group.UserID)
	if err != nil || owner.RelayUserID == nil {
		return 0, attributionrequestclaim.StatusOwnerMismatch, "owner_mismatch"
	}
	return int64(*owner.RelayUserID), "", ""
}

func normalizeUsage(requestID string, usage relay.RequestUsage) (int64, bool) {
	if strings.TrimSpace(usage.RequestID) != requestID || usage.UserID <= 0 || strings.TrimSpace(usage.RequestedModel) == "" || usage.UsageAt.IsZero() {
		return 0, false
	}
	values := []int64{usage.InputTokens, usage.OutputTokens, usage.CacheCreationTokens, usage.CacheReadTokens}
	var total int64
	for _, value := range values {
		if value < 0 || value > math.MaxInt64-total {
			return 0, false
		}
		total += value
	}
	if usage.TotalTokens != nil && *usage.TotalTokens != total {
		return 0, false
	}
	return total, true
}

func (s *Service) retryPending(ctx context.Context, claim *ent.AttributionRequestClaim, token string, attempt int) error {
	delay := s.retryDelay(attempt)
	now := s.now().UTC()
	update := s.client.AttributionRequestClaim.Update().Where(attributionrequestclaim.IDEQ(claim.ID), attributionrequestclaim.LeaseTokenEQ(token)).
		SetNextAttemptAt(nextAttempt(now, delay, claim.ExpiresAt)).SetLastErrorCode("not_found").ClearLeaseToken().ClearLeaseExpiresAt()
	if finalAttemptDue(now, claim.ExpiresAt) {
		update.SetStatus(attributionrequestclaim.StatusSourceExpired)
	} else {
		update.SetStatus(attributionrequestclaim.StatusPending)
	}
	updated, err := update.Save(ctx)
	return requireSingleUpdate("schedule pending request claim", updated, err)
}

func (s *Service) retry(ctx context.Context, claim *ent.AttributionRequestClaim, token string, attempt int, code string, cause error) error {
	now := s.now().UTC()
	update := s.client.AttributionRequestClaim.Update().Where(attributionrequestclaim.IDEQ(claim.ID), attributionrequestclaim.LeaseTokenEQ(token)).
		SetNextAttemptAt(nextAttempt(now, s.retryDelay(attempt), claim.ExpiresAt)).SetLastErrorCode(code).ClearLeaseToken().ClearLeaseExpiresAt()
	if finalAttemptDue(now, claim.ExpiresAt) {
		update.SetStatus(attributionrequestclaim.StatusSourceExpired)
	} else {
		update.SetStatus(attributionrequestclaim.StatusProviderUnavailable)
	}
	updated, err := update.Save(ctx)
	if err := requireSingleUpdate("schedule request claim retry", updated, err); err != nil {
		return err
	}
	if cause != nil {
		s.logger.Warn("v2 request usage lookup deferred", zap.Int("request_claim_id", claim.ID), zap.String("reason", code), zap.Error(cause))
	}
	return nil
}

func finalAttemptDue(now, expiresAt time.Time) bool {
	return !now.Before(expiresAt.UTC().Add(-FinalAttemptLead))
}

func nextAttempt(now time.Time, delay time.Duration, expiresAt time.Time) time.Time {
	next := now.Add(delay)
	deadline := expiresAt.UTC().Add(-FinalAttemptLead)
	if next.After(deadline) {
		return deadline
	}
	return next
}

func (s *Service) finalizeDueGroups(ctx context.Context, now time.Time) error {
	groups, err := s.client.AttributionClaimGroup.Query().Where(
		attributionclaimgroup.FinalizedAtIsNil(),
		attributionclaimgroup.ExpiresAtLTE(now.Add(FinalAttemptLead)),
		attributionclaimgroup.Or(
			attributionclaimgroup.FinalizationAttemptCountEQ(0),
			attributionclaimgroup.FinalizationNextAttemptAtLTE(now),
		),
	).Order(ent.Asc(attributionclaimgroup.FieldExpiresAt), ent.Asc(attributionclaimgroup.FieldID)).Limit(s.batchSize).All(ctx)
	if err != nil {
		return fmt.Errorf("query claim groups due for finalization: %w", err)
	}
	var firstErr error
	for _, group := range groups {
		if err := s.finalizeGroup(ctx, group.ID, now); err != nil {
			s.metrics.AddAttributionLifecycle("finalization", "error", 1)
			next := now.Add(s.retryDelay(group.FinalizationAttemptCount + 1))
			if _, deferErr := s.client.AttributionClaimGroup.Update().Where(
				attributionclaimgroup.IDEQ(group.ID), attributionclaimgroup.FinalizedAtIsNil(),
			).AddFinalizationAttemptCount(1).SetFinalizationNextAttemptAt(next).SetFinalizationLastErrorCode("finalization_failed").Save(ctx); deferErr != nil {
				err = fmt.Errorf("finalize claim group: %w; defer error: %v", err, deferErr)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		current, err := s.client.AttributionClaimGroup.Get(ctx, group.ID)
		if err == nil && current.FinalizedAt != nil {
			s.metrics.AddAttributionLifecycle("finalization", "succeeded", 1)
		} else if err == nil {
			s.metrics.AddAttributionLifecycle("finalization", "deferred", 1)
		}
	}
	return firstErr
}

func (s *Service) finalizeGroup(ctx context.Context, groupID int, now time.Time) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin claim group finalization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockClaimGroup(ctx, tx.Client(), groupID); err != nil {
		return err
	}
	group, err := tx.Client().AttributionClaimGroup.Get(ctx, groupID)
	if err != nil {
		return fmt.Errorf("reload claim group for finalization: %w", err)
	}
	if group.FinalizedAt != nil {
		return nil
	}
	claims, err := tx.Client().AttributionRequestClaim.Query().Where(
		attributionrequestclaim.ClaimGroupIDEQ(groupID),
	).Order(ent.Asc(attributionrequestclaim.FieldID)).All(ctx)
	if err != nil {
		return fmt.Errorf("query request claims for finalization: %w", err)
	}
	unresolved := 0
	for _, claim := range claims {
		if claim.LeaseExpiresAt != nil && claim.LeaseExpiresAt.After(now) {
			return nil
		}
		if claim.Status == attributionrequestclaim.StatusPending || claim.Status == attributionrequestclaim.StatusProviderUnavailable {
			return nil
		}
		if claim.Status != attributionrequestclaim.StatusReconciled {
			unresolved++
			continue
		}
		if err := attributionpool.MaterializeRequestClaim(ctx, tx.Client(), claim.ID, now); err != nil {
			return fmt.Errorf("verify reconciled request materialization: %w", err)
		}
	}
	if unresolved > 0 {
		if err := attributionpool.MaterializeCoverageGaps(ctx, tx.Client(), groupID, unresolved); err != nil {
			return fmt.Errorf("materialize unresolved coverage gaps: %w", err)
		}
	}
	if _, err := tx.Client().AttributionRequestClaim.Delete().Where(attributionrequestclaim.ClaimGroupIDEQ(groupID)).Exec(ctx); err != nil {
		return fmt.Errorf("delete finalized request claims: %w", err)
	}
	updated, err := tx.Client().AttributionClaimGroup.Update().Where(
		attributionclaimgroup.IDEQ(groupID), attributionclaimgroup.FinalizedAtIsNil(),
	).SetFinalizedAt(now).SetRequestCount(0).SetThreadID("").SetTurnID("").SetEvidenceDigest("").SetCommitAllocations([]map[string]any{}).ClearFinalizationLastErrorCode().ClearCalibrationDigest().
		SetCalibrationInputTokens(0).SetCalibrationOutputTokens(0).SetCalibrationCacheCreationTokens(0).
		SetCalibrationCacheReadTokens(0).SetCalibrationTotalTokens(0).Save(ctx)
	if err != nil {
		return fmt.Errorf("strip finalized claim group details: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("finalize claim group: updated=%d", updated)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit claim group finalization: %w", err)
	}
	return nil
}

func (s *Service) cleanupExpiredGroups(ctx context.Context, now time.Time) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin expired claim cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	hardExpired, err := tx.Client().AttributionClaimGroup.Update().Where(
		attributionclaimgroup.FinalizedAtIsNil(), attributionclaimgroup.ExpiresAtLTE(now),
	).SetFinalizedAt(now).SetRequestCount(0).SetThreadID("").SetTurnID("").SetEvidenceDigest("").SetCommitAllocations([]map[string]any{}).
		ClearCalibrationDigest().SetCalibrationInputTokens(0).SetCalibrationOutputTokens(0).SetCalibrationCacheCreationTokens(0).
		SetCalibrationCacheReadTokens(0).SetCalibrationTotalTokens(0).SetFinalizationLastErrorCode("hard_expired").Save(ctx)
	if err != nil {
		s.metrics.AddAttributionLifecycle("cleanup", "error", 1)
		return fmt.Errorf("strip hard-expired claim groups: %w", err)
	}
	if _, err := tx.Client().AttributionRequestClaim.Delete().Where(attributionrequestclaim.ExpiresAtLTE(now)).Exec(ctx); err != nil {
		s.metrics.AddAttributionLifecycle("cleanup", "error", 1)
		return fmt.Errorf("delete hard-expired request claims: %w", err)
	}
	deleted, err := tx.Client().AttributionClaimGroup.Delete().Where(
		attributionclaimgroup.FinalizedAtNotNil(), attributionclaimgroup.ExpiresAtLTE(now),
	).Exec(ctx)
	if err != nil {
		s.metrics.AddAttributionLifecycle("cleanup", "error", 1)
		return fmt.Errorf("delete expired finalized claim groups: %w", err)
	}
	if err := tx.Commit(); err != nil {
		s.metrics.AddAttributionLifecycle("cleanup", "error", 1)
		return fmt.Errorf("commit expired claim cleanup: %w", err)
	}
	if hardExpired > 0 {
		s.metrics.AddAttributionLifecycle("cleanup", "hard_expired", hardExpired)
	}
	if deleted > 0 {
		s.metrics.AddAttributionLifecycle("cleanup", "succeeded", deleted)
	}
	return nil
}

func (s *Service) recordHealth(ctx context.Context, now time.Time) error {
	pendingPredicate := attributionrequestclaim.StatusIn(attributionrequestclaim.StatusPending, attributionrequestclaim.StatusProviderUnavailable)
	pending, err := s.client.AttributionRequestClaim.Query().Where(pendingPredicate).Count(ctx)
	if err != nil {
		return fmt.Errorf("count pending request claims: %w", err)
	}
	oldestAge := time.Duration(0)
	if pending > 0 {
		oldest, err := s.client.AttributionRequestClaim.Query().Where(pendingPredicate).
			Order(ent.Asc(attributionrequestclaim.FieldCreatedAt), ent.Asc(attributionrequestclaim.FieldID)).First(ctx)
		if err != nil {
			return fmt.Errorf("load oldest pending request claim: %w", err)
		}
		oldestAge = now.Sub(oldest.CreatedAt)
		if oldestAge < 0 {
			oldestAge = 0
		}
	}
	nearExpiry, err := s.client.AttributionClaimGroup.Query().Where(
		attributionclaimgroup.FinalizedAtIsNil(), attributionclaimgroup.ExpiresAtLTE(now.Add(FinalAttemptLead)),
	).Count(ctx)
	if err != nil {
		return fmt.Errorf("count near-expiry claim groups: %w", err)
	}
	s.metrics.SetAttributionHealth(pending, oldestAge, nearExpiry)
	return nil
}

func (s *Service) finish(ctx context.Context, id int, token string, status attributionrequestclaim.Status, code string) error {
	updated, err := s.client.AttributionRequestClaim.Update().Where(attributionrequestclaim.IDEQ(id), attributionrequestclaim.LeaseTokenEQ(token)).
		SetStatus(status).SetLastErrorCode(code).ClearLeaseToken().ClearLeaseExpiresAt().Save(ctx)
	return requireSingleUpdate("finish request claim reconciliation", updated, err)
}

func requireSingleUpdate(action string, updated int, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if updated != 1 {
		return fmt.Errorf("%s: updated=%d", action, updated)
	}
	return nil
}

func (s *Service) retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	base := time.Minute * time.Duration(1<<shift)
	if base > time.Hour {
		base = time.Hour
	}
	return base + time.Duration(s.randFloat64()*float64(base/4))
}
