package owneraccount

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// Scope reports the owner-account count and the global mutation switch. A user
// with no matching account gets an empty count, which hides the console tab.
func (s *Service) Scope(ctx context.Context, actorUserID int) (*ScopeResponse, error) {
	actor, err := s.loadActor(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	response := &ScopeResponse{MutationsEnabled: s.options.MutationsEnabled}
	provider, err := s.primaryProvider(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return response, nil
	}
	accounts, err := s.directory(ctx, provider)
	if err != nil {
		return nil, classifyUpstream(err)
	}
	response.AccountCount = len(ownedAccounts(accounts, actor.Email))
	return response, nil
}

// List returns one page of the actor's own accounts with an exact total.
func (s *Service) List(ctx context.Context, actorUserID, page, pageSize int) (*AccountListResponse, error) {
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	actor, err := s.loadActor(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	response := &AccountListResponse{
		Items: []AccountView{}, Page: page, PageSize: pageSize,
		MutationsEnabled: s.options.MutationsEnabled,
	}
	provider, err := s.primaryProvider(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return response, nil
	}
	accounts, err := s.directory(ctx, provider)
	if err != nil {
		return nil, classifyUpstream(err)
	}
	owned := ownedAccounts(accounts, actor.Email)
	response.Total = len(owned)
	start, end := pageBounds(len(owned), page, pageSize)
	if start >= end {
		return response, nil
	}
	pageAccounts := owned[start:end]
	pageIDs := make([]int64, 0, len(pageAccounts))
	for _, account := range pageAccounts {
		pageIDs = append(pageIDs, account.ID)
	}
	if _, err := s.sweepStaleAccounts(ctx, provider.ID, pageIDs); err != nil {
		return nil, err
	}
	facts, err := s.operationFacts(ctx, provider.ID, pageIDs)
	if err != nil {
		return nil, err
	}
	now, err := s.databaseNow(ctx)
	if err != nil {
		return nil, err
	}
	response.Items = make([]AccountView, 0, len(pageAccounts))
	for _, account := range pageAccounts {
		response.Items = append(response.Items, s.accountView(account, facts[account.ID], now))
	}
	return response, nil
}

// Usage reads the 5h/7d windows of the requested accounts. Requests are bounded
// by the list page ceiling and chunked to the upstream batch limit. Non-owned and
// unknown ids are indistinguishable: both appear as account-not-found errors.
func (s *Service) Usage(ctx context.Context, actorUserID int, accountIDs []int64, force bool) (*UsageBatchResponse, error) {
	requested := normalizeAccountIDs(accountIDs)
	if len(requested) == 0 {
		return &UsageBatchResponse{Items: []UsageItem{}, Errors: []UsageError{}}, nil
	}
	if len(requested) > s.options.UsageRequestLimit {
		return nil, errUsageBatchTooLarge()
	}
	actor, err := s.loadActor(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	provider, err := s.primaryProvider(ctx)
	if err != nil {
		return nil, err
	}
	response := &UsageBatchResponse{Items: []UsageItem{}, Errors: []UsageError{}}
	if provider == nil {
		for _, accountID := range requested {
			response.Errors = append(response.Errors, UsageError{AccountID: accountID, Code: CodeAccountNotFound})
		}
		return response, nil
	}
	accounts, err := s.directory(ctx, provider)
	if err != nil {
		return nil, classifyUpstream(err)
	}
	ownedByID := make(map[int64]relay.OwnerAccount, len(accounts))
	for _, account := range ownedAccounts(accounts, actor.Email) {
		ownedByID[account.ID] = account
	}
	ownedIDs := make([]int64, 0, len(requested))
	for _, accountID := range requested {
		if _, ok := ownedByID[accountID]; ok {
			ownedIDs = append(ownedIDs, accountID)
			continue
		}
		response.Errors = append(response.Errors, UsageError{AccountID: accountID, Code: CodeAccountNotFound})
	}
	if len(ownedIDs) == 0 {
		return response, nil
	}
	reader, ok := s.usageReader(ctx, provider)
	if !ok {
		for _, accountID := range ownedIDs {
			response.Errors = append(response.Errors, UsageError{AccountID: accountID, Code: CodeCapabilityUnsupported})
		}
		return response, nil
	}
	for start := 0; start < len(ownedIDs); start += s.options.UsageBatchLimit {
		end := min(start+s.options.UsageBatchLimit, len(ownedIDs))
		chunk := ownedIDs[start:end]
		readCtx, cancel := context.WithTimeout(ctx, s.options.UsageReadTimeout)
		batch, readErr := reader.ReadOwnerAccountUsage(readCtx, chunk, force)
		cancel()
		if readErr != nil {
			code := classifyUpstream(readErr).Code
			for _, accountID := range chunk {
				response.Errors = append(response.Errors, UsageError{AccountID: accountID, Code: code})
			}
			continue
		}
		seen := make(map[int64]bool, len(batch))
		for _, usage := range batch {
			seen[usage.AccountID] = true
			account, owned := ownedByID[usage.AccountID]
			if !owned {
				continue
			}
			if usage.Err != nil {
				response.Errors = append(response.Errors, UsageError{AccountID: usage.AccountID, Code: CodeUsageUnavailable})
				continue
			}
			observedAt := account.UsageUpdatedAt
			response.Items = append(response.Items, UsageItem{
				AccountID: usage.AccountID,
				Usage:     usageView(usage, observedAt),
			})
		}
		for _, accountID := range chunk {
			if !seen[accountID] {
				response.Errors = append(response.Errors, UsageError{AccountID: accountID, Code: CodeUsageUnavailable})
			}
		}
	}
	return response, nil
}

// RefreshUsage re-probes one account's usage with force=true. It shares the
// refresh cooldown with the quota and referral refreshes but writes no operation
// row, because the schema has no usage action.
func (s *Service) RefreshUsage(ctx context.Context, actorUserID int, accountID int64) (*UsageResponse, error) {
	provider, account, err := s.ownedAccount(ctx, actorUserID, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRefreshAllowed(ctx, provider.ID, accountID); err != nil {
		return nil, err
	}
	reader, ok := s.usageReader(ctx, provider)
	if !ok {
		return nil, classifyUpstream(fmt.Errorf("%w: owner account usage", relay.ErrCapabilityUnsupported))
	}
	readCtx, cancel := context.WithTimeout(ctx, s.options.UsageReadTimeout)
	batch, readErr := reader.ReadOwnerAccountUsage(readCtx, []int64{accountID}, true)
	cancel()
	if readErr != nil {
		return nil, classifyUpstream(readErr)
	}
	entry, found := findUsage(batch, accountID)
	if !found || entry.Err != nil {
		return nil, newError(http.StatusBadGateway, CodeUpstreamUnavailable, "the relay provider is unavailable")
	}
	s.runtime.InvalidateOwnerAccounts(provider.ID, platformOpenAI)
	observedAt := account.UsageUpdatedAt
	if refreshed, reloadErr := s.reloadOwnedAccount(ctx, provider, account.ID, actorUserID); reloadErr == nil && refreshed != nil {
		observedAt = refreshed.UsageUpdatedAt
	}
	return &UsageResponse{AccountID: accountID, Usage: usageView(entry, observedAt)}, nil
}

// RefreshQuota performs one fresh quota observation and records it locally so the
// reset gate can require an observation newer than the last reset attempt.
func (s *Service) RefreshQuota(ctx context.Context, actorUserID int, accountID int64) (*QuotaResponse, error) {
	startedAt := time.Now()
	outcome := "failed"
	defer func() { s.observeOperation("refresh_quota", outcome, time.Since(startedAt)) }()
	provider, account, err := s.ownedAccount(ctx, actorUserID, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRefreshAllowed(ctx, provider.ID, accountID); err != nil {
		return nil, err
	}
	manager, ok := s.quotaManager(ctx, provider)
	if !ok {
		return nil, classifyUpstream(fmt.Errorf("%w: owner account quota", relay.ErrCapabilityUnsupported))
	}
	readCtx, cancel := context.WithTimeout(ctx, s.options.QuotaReadTimeout)
	quota, readErr := manager.RefreshOpenAIQuota(readCtx, accountID)
	cancel()
	attempt := mutationAttempt{
		ProviderID: provider.ID, AccountID: accountID, ActorUserID: actorUserID, Action: "refresh_quota",
	}
	if readErr != nil {
		_, _ = s.insertTerminalOperation(ctx, attempt, "failed", classifyUpstreamCode(readErr), nil)
		s.logger.Info("owner account refresh failed",
			zap.Int("provider_id", provider.ID), zap.Int64("account_id", accountID),
			zap.Int("actor_user_id", actorUserID), zap.String("action", "refresh_quota"),
			zap.String("error_class", logClassify(readErr)))
		return nil, classifyUpstream(readErr)
	}
	if _, err := s.insertTerminalOperation(ctx, attempt, "succeeded", "", quotaObservationResult(quota)); err != nil {
		return nil, err
	}
	s.runtime.InvalidateOwnerAccounts(provider.ID, platformOpenAI)
	outcome = "succeeded"
	return &QuotaResponse{AccountID: accountID, Quota: quotaViewFromObservation(quota, account.AutoResetCreditEnabled)}, nil
}

// RefreshReferral performs one fresh referral observation and records it locally
// so the invitation gate can require an observation newer than the last invite
// attempt.
func (s *Service) RefreshReferral(ctx context.Context, actorUserID int, accountID int64) (*ReferralResponse, error) {
	startedAt := time.Now()
	outcome := "failed"
	defer func() { s.observeOperation("refresh_referral", outcome, time.Since(startedAt)) }()
	provider, _, err := s.ownedAccount(ctx, actorUserID, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureRefreshAllowed(ctx, provider.ID, accountID); err != nil {
		return nil, err
	}
	manager, ok := s.referralManager(ctx, provider)
	if !ok {
		return nil, classifyUpstream(fmt.Errorf("%w: owner account referral", relay.ErrCapabilityUnsupported))
	}
	readCtx, cancel := context.WithTimeout(ctx, s.options.ReferralReadTimeout)
	referral, readErr := manager.RefreshOpenAIReferral(readCtx, accountID)
	cancel()
	attempt := mutationAttempt{
		ProviderID: provider.ID, AccountID: accountID, ActorUserID: actorUserID, Action: "refresh_referral",
	}
	if readErr != nil {
		_, _ = s.insertTerminalOperation(ctx, attempt, "failed", classifyUpstreamCode(readErr), nil)
		s.logger.Info("owner account refresh failed",
			zap.Int("provider_id", provider.ID), zap.Int64("account_id", accountID),
			zap.Int("actor_user_id", actorUserID), zap.String("action", "refresh_referral"),
			zap.String("error_class", logClassify(readErr)))
		return nil, classifyUpstream(readErr)
	}
	if _, err := s.insertTerminalOperation(ctx, attempt, "succeeded", "", referralObservationResult(referral)); err != nil {
		return nil, err
	}
	s.runtime.InvalidateOwnerAccounts(provider.ID, platformOpenAI)
	outcome = "succeeded"
	return &ReferralResponse{AccountID: accountID, Referral: referralViewFromSnapshot(referral)}, nil
}

// ensureRefreshAllowed enforces the shared 30-second refresh cooldown from the
// local refresh rows, using database time.
func (s *Service) ensureRefreshAllowed(ctx context.Context, providerID int, accountID int64) error {
	if _, err := s.sweepStaleAccounts(ctx, providerID, []int64{accountID}); err != nil {
		return err
	}
	facts, err := s.operationFacts(ctx, providerID, []int64{accountID})
	if err != nil {
		return err
	}
	now, err := s.databaseNow(ctx)
	if err != nil {
		return err
	}
	until := refreshCooldownUntil(facts[accountID], s.options.RefreshCooldown, now)
	if until == nil {
		return nil
	}
	retryAfter := int(until.Sub(now).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	return errRefreshCooldown(retryAfter, until.UTC().Format(time.RFC3339))
}

// ownedAccount resolves the actor, the primary provider and the explicitly
// requested account, returning account-not-found for an id the actor does not
// own. Existence is never disclosed across owners.
func (s *Service) ownedAccount(ctx context.Context, actorUserID int, accountID int64) (*ent.RelayProvider, *relay.OwnerAccount, error) {
	if accountID <= 0 {
		return nil, nil, errAccountNotFound()
	}
	actor, err := s.loadActor(ctx, actorUserID)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.primaryProvider(ctx)
	if err != nil {
		return nil, nil, err
	}
	if provider == nil {
		return nil, nil, errAccountNotFound()
	}
	accounts, err := s.directory(ctx, provider)
	if err != nil {
		return nil, nil, classifyUpstream(err)
	}
	for index := range accounts {
		if accounts[index].ID != accountID {
			continue
		}
		if !accountOwnedBy(accounts[index], actor.Email) {
			return nil, nil, errAccountNotFound()
		}
		account := accounts[index]
		return provider, &account, nil
	}
	return nil, nil, errAccountNotFound()
}

func (s *Service) reloadOwnedAccount(ctx context.Context, provider *ent.RelayProvider, accountID int64, actorUserID int) (*relay.OwnerAccount, error) {
	actor, err := s.loadActor(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.directory(ctx, provider)
	if err != nil {
		return nil, err
	}
	for index := range accounts {
		if accounts[index].ID == accountID && accountOwnedBy(accounts[index], actor.Email) {
			account := accounts[index]
			return &account, nil
		}
	}
	return nil, errAccountNotFound()
}

// usageReader, quotaManager and referralManager resolve the optional Relay
// capabilities behind type assertions, exactly like the provider boundary
// documents.
func (s *Service) usageReader(ctx context.Context, provider *ent.RelayProvider) (relay.OwnerAccountUsageReader, bool) {
	resolved, err := s.runtime.Resolve(ctx, provider.ID)
	if err != nil {
		return nil, false
	}
	reader, ok := resolved.(relay.OwnerAccountUsageReader)
	return reader, ok
}

func (s *Service) quotaManager(ctx context.Context, provider *ent.RelayProvider) (relay.OpenAIQuotaManager, bool) {
	resolved, err := s.runtime.Resolve(ctx, provider.ID)
	if err != nil {
		return nil, false
	}
	manager, ok := resolved.(relay.OpenAIQuotaManager)
	return manager, ok
}

func (s *Service) referralManager(ctx context.Context, provider *ent.RelayProvider) (relay.OpenAIReferralManager, bool) {
	resolved, err := s.runtime.Resolve(ctx, provider.ID)
	if err != nil {
		return nil, false
	}
	manager, ok := resolved.(relay.OpenAIReferralManager)
	return manager, ok
}

func findUsage(batch []relay.OwnerAccountUsage, accountID int64) (relay.OwnerAccountUsage, bool) {
	for _, usage := range batch {
		if usage.AccountID == accountID {
			return usage, true
		}
	}
	return relay.OwnerAccountUsage{}, false
}

func normalizeAccountIDs(accountIDs []int64) []int64 {
	normalized := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		normalized = append(normalized, accountID)
	}
	return normalized
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = pageSizeDefault
	}
	switch pageSize {
	case 20, 50, 100:
	default:
		return 0, 0, errInvalidPagination()
	}
	return page, pageSize, nil
}

func pageBounds(total, page, pageSize int) (int, int) {
	start := (page - 1) * pageSize
	if start >= total || start < 0 {
		return total, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}
