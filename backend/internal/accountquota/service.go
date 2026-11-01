package accountquota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

const (
	defaultConcurrency          = 4
	defaultSweepTimeout         = 10 * time.Minute
	defaultAccountTimeout       = 35 * time.Second
	defaultMinSweepInterval     = 2 * time.Minute
	defaultStaleAfter           = 20 * time.Minute
	defaultJitterMax            = 400 * time.Millisecond
	defaultTerminalWriteTimeout = 10 * time.Second
)

// ProviderRuntime is the relay runtime subset a sweep needs: the cached owner
// account directory, its invalidation after a sweep, and the long-running
// provider used for one quota refresh per account.
type ProviderRuntime interface {
	OwnerAccounts(ctx context.Context, providerID int, configurationVersion int64, platform string) ([]relay.OwnerAccount, error)
	InvalidateOwnerAccounts(providerID int, platform string)
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
	ResolveLongRunningEntity(row *ent.RelayProvider) (relay.Provider, error)
}

// Options configure the service. Zero values fall back to the documented
// defaults; tests override the timing knobs instead of sleeping.
type Options struct {
	Concurrency          int
	SweepTimeout         time.Duration
	AccountTimeout       time.Duration
	MinSweepInterval     time.Duration
	StaleAfter           time.Duration
	JitterMax            time.Duration
	TerminalWriteTimeout time.Duration
	Now                  func() time.Time
	Sleep                func(context.Context, time.Duration) error
	Logger               *zap.Logger
}

// providerReader loads the relay provider row a sweep targets. It is an interface
// so the service can be exercised without a database.
type providerReader interface {
	Get(ctx context.Context, id int) (*ent.RelayProvider, error)
}

type entProviderReader struct {
	client *ent.Client
}

func (r entProviderReader) Get(ctx context.Context, id int) (*ent.RelayProvider, error) {
	return r.client.RelayProvider.Get(ctx, id)
}

// Service owns the account quota read model and the async sweep lifecycle.
type Service struct {
	runtime   ProviderRuntime
	providers providerReader
	store     Repository
	logger    *zap.Logger
	options   Options
}

// NewService builds the account quota service. It needs the Ent client for the
// provider row and for the sweep repository, and the relay runtime for the
// account directory and the long-running quota capability.
func NewService(client *ent.Client, runtime ProviderRuntime, logger *zap.Logger, options Options) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("account quota Ent client is required")
	}
	if runtime == nil {
		return nil, fmt.Errorf("account quota relay runtime is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	options = withDefaults(options)
	return newService(runtime, entProviderReader{client: client}, newEntRepository(client), logger, options), nil
}

// newService assembles the service from its seams. Tests build it directly so the
// worker, the single-flight rules and the read model run without a database.
func newService(runtime ProviderRuntime, providers providerReader, store Repository, logger *zap.Logger, options Options) *Service {
	return &Service{
		runtime:   runtime,
		providers: providers,
		store:     store,
		logger:    logger,
		options:   options,
	}
}

func withDefaults(options Options) Options {
	if options.Concurrency <= 0 {
		options.Concurrency = defaultConcurrency
	}
	if options.SweepTimeout <= 0 {
		options.SweepTimeout = defaultSweepTimeout
	}
	if options.AccountTimeout <= 0 {
		options.AccountTimeout = defaultAccountTimeout
	}
	if options.MinSweepInterval <= 0 {
		options.MinSweepInterval = defaultMinSweepInterval
	}
	if options.StaleAfter <= 0 {
		options.StaleAfter = defaultStaleAfter
	}
	if options.JitterMax < 0 {
		options.JitterMax = 0
	}
	if options.JitterMax == 0 {
		options.JitterMax = defaultJitterMax
	}
	if options.TerminalWriteTimeout <= 0 {
		options.TerminalWriteTimeout = defaultTerminalWriteTimeout
	}
	return options
}

// List returns the quota tab read model: the eligible OpenAI OAuth accounts with
// their last successful observation, the cached directory snapshot and the latest
// sweep. It performs no upstream call; the directory read is served by the relay
// runtime cache.
func (s *Service) List(ctx context.Context, providerID int, platform string) (*Page, error) {
	platform = normalizePlatform(platform)
	if providerID <= 0 {
		return nil, errInvalidRequest("provider_id is required")
	}
	if platform != PlatformOpenAI {
		return nil, errInvalidRequest("only the openai platform is supported")
	}
	row, err := s.providerRow(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if err := s.settleStale(ctx, providerID); err != nil {
		return nil, err
	}
	accounts, err := s.runtime.OwnerAccounts(ctx, providerID, row.ConfigurationVersion, platform)
	if err != nil {
		return nil, s.classifyRuntimeError(err, "list owner accounts")
	}
	observations, err := s.store.Observations(ctx, providerID)
	if err != nil {
		s.logError("load account quota observations", err)
		return nil, errInternal()
	}
	latest, err := s.store.LatestSweep(ctx, providerID)
	if err != nil {
		s.logError("load latest account quota sweep", err)
		return nil, errInternal()
	}

	items := make([]Item, 0, len(accounts))
	for _, account := range accounts {
		if !isEligible(account) {
			continue
		}
		item := Item{Account: projectAccount(account), Snapshot: snapshotFromAccount(account)}
		if observation, ok := observations[account.ID]; ok {
			copied := observation
			item.Observation = &copied
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Account.Name != items[right].Account.Name {
			return items[left].Account.Name < items[right].Account.Name
		}
		return items[left].Account.ID < items[right].Account.ID
	})
	return &Page{
		ProviderID:  providerID,
		Platform:    platform,
		Items:       items,
		Total:       len(items),
		LatestSweep: latest,
	}, nil
}

// StartSweep accepts one async sweep and returns it immediately. The detached
// worker performs exactly one quota refresh per target; a target failure is
// recorded on its row and never aborts the sweep.
func (s *Service) StartSweep(ctx context.Context, request StartSweepRequest) (*Sweep, error) {
	platform := normalizePlatform(request.Platform)
	if request.ProviderID <= 0 {
		return nil, errInvalidRequest("provider_id is required")
	}
	if platform != PlatformOpenAI {
		return nil, errInvalidRequest("only the openai platform is supported")
	}
	row, err := s.providerRow(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}
	if err := s.settleStale(ctx, request.ProviderID); err != nil {
		return nil, err
	}
	scope := ScopeAll
	if len(request.AccountIDs) > 0 {
		scope = ScopeSelected
	}
	if err := s.checkSweepAllowed(ctx, request.ProviderID, scope); err != nil {
		return nil, err
	}
	accounts, err := s.runtime.OwnerAccounts(ctx, request.ProviderID, row.ConfigurationVersion, platform)
	if err != nil {
		return nil, s.classifyRuntimeError(err, "list owner accounts")
	}
	eligible := make([]relay.OwnerAccount, 0, len(accounts))
	for _, account := range accounts {
		if isEligible(account) {
			eligible = append(eligible, account)
		}
	}
	sort.Slice(eligible, func(left, right int) bool { return eligible[left].ID < eligible[right].ID })
	targets, skipped, err := selectTargets(eligible, request)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errNoTargets("no eligible account would be refreshed")
	}
	if len(targets) > MaxSweepTargets {
		return nil, errInvalidRequest(fmt.Sprintf("at most %d accounts can be refreshed in one sweep", MaxSweepTargets))
	}

	now := s.now()
	sweep := &Sweep{
		Status:           SweepQueued,
		ProviderID:       request.ProviderID,
		Platform:         platform,
		Scope:            scope,
		IncludeAutoReset: request.IncludeAutoReset,
		TotalCount:       len(targets) + len(skipped),
		SkippedCount:     len(skipped),
		CreatedAt:        now,
		Results:          skippedResults(skipped),
	}
	if err := s.store.CreateSweep(ctx, sweep); err != nil {
		return nil, err
	}
	go s.runSweep(context.WithoutCancel(ctx), sweep, targets, len(skipped))
	return sweep, nil
}

// GetSweep returns one sweep by ID, settling a sweeper that no worker finished.
func (s *Service) GetSweep(ctx context.Context, id int) (*Sweep, error) {
	sweep, err := s.store.SweepByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.settleIfStale(ctx, sweep)
}

// LatestSweep returns the newest sweep of one provider, or nil when the provider
// has never been swept.
func (s *Service) LatestSweep(ctx context.Context, providerID int) (*Sweep, error) {
	if providerID <= 0 {
		return nil, errInvalidRequest("provider_id is required")
	}
	if err := s.settleStale(ctx, providerID); err != nil {
		return nil, err
	}
	sweep, err := s.store.LatestSweep(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return s.settleIfStale(ctx, sweep)
}

// checkSweepAllowed enforces the single in-flight sweep per provider and the
// minimum interval between all-account sweeps. Selected refreshes are explicit and
// small, so they skip the interval check.
func (s *Service) checkSweepAllowed(ctx context.Context, providerID int, scope string) error {
	latest, err := s.store.LatestSweep(ctx, providerID)
	if err != nil {
		s.logError("load latest account quota sweep", err)
		return errInternal()
	}
	if latest == nil {
		return nil
	}
	if latest.Status == SweepQueued || latest.Status == SweepRunning {
		return errSweepInFlight()
	}
	if scope != ScopeAll {
		return nil
	}
	elapsed := s.now().Sub(latest.CreatedAt)
	if elapsed >= s.options.MinSweepInterval {
		return nil
	}
	remaining := s.options.MinSweepInterval - elapsed
	return errSweepTooSoon(int(math.Ceil(remaining.Seconds())))
}

func (s *Service) settleStale(ctx context.Context, providerID int) error {
	now := s.now()
	abandoned, err := s.store.AbandonStaleSweeps(ctx, providerID, now.Add(-s.options.StaleAfter), now)
	if err != nil {
		s.logError("settle stale account quota sweeps", err)
		return errInternal()
	}
	if abandoned > 0 {
		s.logger.Warn("abandoned stale account quota sweeps",
			zap.Int("provider_id", providerID),
			zap.Int("sweeps", abandoned),
		)
	}
	return nil
}

// settleIfStale settles a sweep whose worker never finished, then reloads it.
func (s *Service) settleIfStale(ctx context.Context, sweep *Sweep) (*Sweep, error) {
	if sweep == nil {
		return nil, nil
	}
	if sweep.Status != SweepQueued && sweep.Status != SweepRunning {
		return sweep, nil
	}
	if s.now().Sub(sweep.CreatedAt) <= s.options.StaleAfter {
		return sweep, nil
	}
	if err := s.settleStale(ctx, sweep.ProviderID); err != nil {
		return nil, err
	}
	return s.store.SweepByID(ctx, sweep.ID)
}

func (s *Service) providerRow(ctx context.Context, providerID int) (*ent.RelayProvider, error) {
	row, err := s.providers.Get(ctx, providerID)
	if ent.IsNotFound(err) {
		return nil, errProviderNotFound()
	}
	if err != nil {
		s.logError("load relay provider", err)
		return nil, errInternal()
	}
	return row, nil
}

// classifyRuntimeError keeps relay sentinels out of the service error space while
// never forwarding upstream text.
func (s *Service) classifyRuntimeError(err error, operation string) error {
	switch {
	case errors.Is(err, relay.ErrCapabilityUnsupported):
		return errCapabilityUnsupported("the relay provider does not support this capability")
	case errors.Is(err, relay.ErrComplianceAcknowledgementRequired):
		return errComplianceAckRequired()
	default:
		s.logError(operation, err)
		return errUpstreamUnavailable()
	}
}

func (s *Service) logError(operation string, err error) {
	s.logger.Error(operation, zap.String("error_class", logClass(err)))
}

// logClass keeps logs free of upstream text; the error chain is never printed.
func logClass(err error) string {
	switch {
	case errors.Is(err, relay.ErrCapabilityUnsupported):
		return "capability_unsupported"
	case errors.Is(err, relay.ErrComplianceAcknowledgementRequired):
		return "compliance_ack_required"
	case errors.Is(err, relay.ErrUpstreamOutcomeUnknown):
		return "upstream_outcome_unknown"
	}
	if _, ok := relay.UpstreamHTTPStatus(err); ok {
		return "upstream_status"
	}
	return "storage_or_internal"
}

func (s *Service) terminalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.options.TerminalWriteTimeout)
}

func (s *Service) now() time.Time {
	if s.options.Now != nil {
		return s.options.Now().UTC()
	}
	return time.Now().UTC()
}

func normalizePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

// isEligible keeps the sweep on OpenAI OAuth accounts. Shadow (spark) accounts
// carry no independent quota to refresh and are deliberately excluded.
func isEligible(account relay.OwnerAccount) bool {
	return strings.EqualFold(strings.TrimSpace(account.Type), "oauth") && !account.Shadow
}

func projectAccount(account relay.OwnerAccount) AccountQuotaAccount {
	projected := AccountQuotaAccount{
		ID:                     account.ID,
		Name:                   account.Name,
		Email:                  account.Email,
		Status:                 account.Status,
		Schedulable:            account.Schedulable,
		Shadow:                 account.Shadow,
		AutoResetCreditEnabled: account.AutoResetCreditEnabled,
		Groups:                 make([]AccountQuotaGroup, 0, len(account.Groups)),
	}
	for _, group := range account.Groups {
		projected.Groups = append(projected.Groups, AccountQuotaGroup{ID: group.ID, Name: group.Name, Priority: group.Priority})
	}
	return projected
}

func snapshotFromAccount(account relay.OwnerAccount) *Snapshot {
	snapshot := &Snapshot{UsageUpdatedAt: account.UsageUpdatedAt, ObservedAt: account.UsageUpdatedAt}
	if account.Credits != nil {
		snapshot.Credits = &Credits{
			HasCredits: account.Credits.HasCredits,
			Unlimited:  account.Credits.Unlimited,
			Balance:    account.Credits.Balance,
		}
		if account.Credits.ObservedAt != nil {
			observed := account.Credits.ObservedAt.UTC()
			snapshot.ObservedAt = &observed
		}
	}
	if account.ResetCredits != nil {
		snapshot.ResetCredits = &ResetCredits{
			Available: account.ResetCredits.Available,
			ExpiresAt: formatExpiries(account.ResetCredits.ExpiresAt),
		}
	}
	if snapshot.Credits == nil && snapshot.ResetCredits == nil && snapshot.UsageUpdatedAt == nil {
		return nil
	}
	return snapshot
}

// selectTargets resolves the explicit selection or every eligible account. An
// account whose Sub2API auto reset-credit worker is enabled is skipped unless the
// operator opted in: refreshing it can make the upstream spend a credit
// asynchronously.
func selectTargets(eligible []relay.OwnerAccount, request StartSweepRequest) ([]relay.OwnerAccount, []relay.OwnerAccount, error) {
	if len(request.AccountIDs) > 0 {
		byID := make(map[int64]relay.OwnerAccount, len(eligible))
		for _, account := range eligible {
			byID[account.ID] = account
		}
		seen := make(map[int64]struct{}, len(request.AccountIDs))
		targets := make([]relay.OwnerAccount, 0, len(request.AccountIDs))
		for _, accountID := range request.AccountIDs {
			if accountID <= 0 {
				return nil, nil, errInvalidRequest("account IDs must be positive")
			}
			if _, duplicate := seen[accountID]; duplicate {
				continue
			}
			seen[accountID] = struct{}{}
			account, ok := byID[accountID]
			if !ok {
				return nil, nil, errTargetNotEligible(fmt.Sprintf("account %d is not an eligible OpenAI OAuth account", accountID))
			}
			if account.AutoResetCreditEnabled && !request.IncludeAutoReset {
				return nil, nil, errTargetNotEligible(fmt.Sprintf("account %d has the auto reset-credit worker enabled; include it explicitly to refresh it", accountID))
			}
			targets = append(targets, account)
		}
		if len(targets) == 0 {
			return nil, nil, errInvalidRequest("no account IDs were provided")
		}
		return targets, nil, nil
	}
	targets := make([]relay.OwnerAccount, 0, len(eligible))
	skipped := make([]relay.OwnerAccount, 0, 0)
	for _, account := range eligible {
		if account.AutoResetCreditEnabled && !request.IncludeAutoReset {
			skipped = append(skipped, account)
			continue
		}
		targets = append(targets, account)
	}
	return targets, skipped, nil
}

func skippedResults(skipped []relay.OwnerAccount) []SweepResult {
	results := make([]SweepResult, 0, len(skipped))
	for _, account := range skipped {
		results = append(results, SweepResult{
			AccountID: account.ID,
			Status:    ResultSkipped,
			ErrorCode: "auto-reset-enabled",
			Message:   "skipped because the account has the auto reset-credit worker enabled",
		})
	}
	return results
}
