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
)

type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type Options struct {
	BatchSize   int
	Concurrency int
	LeaseTTL    time.Duration
	Now         func() time.Time
	RandFloat64 func() float64
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
	return &Service{client: client, resolver: resolver, logger: logger, batchSize: options.BatchSize, concurrency: options.Concurrency, leaseTTL: options.LeaseTTL, now: options.Now, randFloat64: options.RandFloat64}, nil
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
		attributionrequestclaim.NextAttemptAtLTE(now),
		attributionrequestclaim.Or(attributionrequestclaim.LeaseExpiresAtIsNil(), attributionrequestclaim.LeaseExpiresAtLTE(now)),
	).Order(ent.Asc(attributionrequestclaim.FieldNextAttemptAt), ent.Asc(attributionrequestclaim.FieldID)).Limit(s.batchSize).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query reconciliation candidates: %w", err)
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
	return processed, firstErr
}

func (s *Service) reconcileCandidate(ctx context.Context, candidate *ent.AttributionRequestClaim) (bool, error) {
	now := s.now().UTC()
	token := uuid.NewString()
	updated, err := s.client.AttributionRequestClaim.Update().Where(
		attributionrequestclaim.IDEQ(candidate.ID),
		attributionrequestclaim.StatusIn(attributionrequestclaim.StatusPending, attributionrequestclaim.StatusProviderUnavailable),
		attributionrequestclaim.NextAttemptAtLTE(now),
		attributionrequestclaim.Or(attributionrequestclaim.LeaseExpiresAtIsNil(), attributionrequestclaim.LeaseExpiresAtLTE(now)),
	).SetLeaseToken(token).SetLeaseExpiresAt(now.Add(s.leaseTTL)).AddAttemptCount(1).Save(ctx)
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
		return true, s.retry(ctx, candidate.ID, token, attempt, "provider_unavailable", err)
	}
	provider, err := s.resolver.Resolve(ctx, candidate.RelayProviderID)
	if err != nil {
		return true, s.retry(ctx, candidate.ID, token, attempt, "provider_unavailable", err)
	}
	reader, ok := provider.(relay.RequestUsageReader)
	if !ok {
		return true, s.retry(ctx, candidate.ID, token, attempt, "provider_unsupported", nil)
	}
	rows, err := reader.ReadRequestUsage(ctx, candidate.RequestID, 2)
	if err != nil {
		return true, s.retry(ctx, candidate.ID, token, attempt, "read_error", err)
	}
	switch len(rows) {
	case 0:
		return true, s.retryPending(ctx, candidate.ID, token, attempt)
	case 1:
		return true, s.reconcileOne(ctx, candidate, token, attempt, rows[0])
	default:
		return true, s.finish(ctx, candidate.ID, token, attributionrequestclaim.StatusAmbiguous, "ambiguous_request")
	}
}

func (s *Service) reconcileOne(ctx context.Context, candidate *ent.AttributionRequestClaim, token string, attempt int, usage relay.RequestUsage) error {
	providerRow, err := s.client.RelayProvider.Get(ctx, candidate.RelayProviderID)
	if err != nil || !providerRow.Enabled {
		return s.retry(ctx, candidate.ID, token, attempt, "provider_unavailable", err)
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

func (s *Service) retryPending(ctx context.Context, id int, token string, attempt int) error {
	delay := s.retryDelay(attempt)
	now := s.now().UTC()
	updated, err := s.client.AttributionRequestClaim.Update().Where(attributionrequestclaim.IDEQ(id), attributionrequestclaim.LeaseTokenEQ(token)).
		SetStatus(attributionrequestclaim.StatusPending).SetNextAttemptAt(now.Add(delay)).SetLastErrorCode("not_found").ClearLeaseToken().ClearLeaseExpiresAt().Save(ctx)
	return requireSingleUpdate("schedule pending request claim", updated, err)
}

func (s *Service) retry(ctx context.Context, id int, token string, attempt int, code string, cause error) error {
	now := s.now().UTC()
	updated, err := s.client.AttributionRequestClaim.Update().Where(attributionrequestclaim.IDEQ(id), attributionrequestclaim.LeaseTokenEQ(token)).
		SetStatus(attributionrequestclaim.StatusProviderUnavailable).SetNextAttemptAt(now.Add(s.retryDelay(attempt))).SetLastErrorCode(code).ClearLeaseToken().ClearLeaseExpiresAt().Save(ctx)
	if err := requireSingleUpdate("schedule request claim retry", updated, err); err != nil {
		return err
	}
	if cause != nil {
		s.logger.Warn("v2 request usage lookup deferred", zap.Int("request_claim_id", id), zap.String("reason", code), zap.Error(cause))
	}
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
