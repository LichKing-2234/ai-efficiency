// Package owneraccount owns the email-derived owner-account console: which
// pooled Relay accounts belong to the signed-in user, the owner-scoped reads
// over them, the server-side gates in front of the two irreversible upstream
// mutations, and the detached worker that performs those mutations.
//
// Ownership is derived per request and never stored. The only local state this
// package writes is an operation row used for async polling, audit, unknown
// blocking, cooldowns and rolling daily caps. Credentials, upstream snapshots and
// ownership are never persisted.
package owneraccount

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/adminuseraccess"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

const (
	// platformOpenAI is the only platform the owner console can see. Shadow
	// accounts, non-OpenAI accounts and non-OAuth accounts are excluded.
	platformOpenAI                = "openai"
	defaultOperationTimeout       = 65 * time.Second
	defaultHeartbeatInterval      = 5 * time.Second
	defaultTerminalWriteTimeout   = 5 * time.Second
	defaultStaleHeartbeat         = 30 * time.Second
	defaultRefreshCooldown        = 30 * time.Second
	defaultQuotaObservationMaxAge = 15 * time.Minute
	defaultInviteObservationAge   = 10 * time.Minute
	defaultResetDailyCap          = 5
	defaultInvitationDailyCap     = 10
	defaultUsageBatchLimit        = 20
	defaultUsageRequestLimit      = 100
	defaultUsageReadTimeout       = 20 * time.Second
	defaultQuotaReadTimeout       = 25 * time.Second
	defaultReferralReadTimeout    = 25 * time.Second
	defaultDirectoryReadTimeout   = 10 * time.Second
	pageSizeDefault               = 20
)

// ProviderRuntime is the subset of relayruntime.Manager this module depends on.
// The runtime stays the only owner of provider clients, directory caches and the
// long-running client factory.
type ProviderRuntime interface {
	OwnerAccounts(ctx context.Context, providerID int, configurationVersion int64, platform string) ([]relay.OwnerAccount, error)
	InvalidateOwnerAccounts(providerID int, platform string)
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
	ResolveLongRunningEntity(row *ent.RelayProvider) (relay.Provider, error)
}

// OperationObserver receives bounded refresh and mutation outcomes. Implementations
// must not add per-user or per-account labels.
type OperationObserver interface {
	ObserveOwnerAccountOperation(action, outcome string, duration time.Duration)
}

// Options configures the service. Zero values fall back to the documented
// product defaults; tests override the timing knobs instead of sleeping.
type Options struct {
	MutationsEnabled         bool
	OperationTimeout         time.Duration
	HeartbeatInterval        time.Duration
	TerminalWriteTimeout     time.Duration
	StaleHeartbeat           time.Duration
	RefreshCooldown          time.Duration
	QuotaObservationMaxAge   time.Duration
	InvitationObservationAge time.Duration
	ResetDailyCap            int
	InvitationDailyCap       int
	UsageBatchLimit          int
	UsageRequestLimit        int
	UsageReadTimeout         time.Duration
	QuotaReadTimeout         time.Duration
	ReferralReadTimeout      time.Duration
	DirectoryReadTimeout     time.Duration
	Observer                 OperationObserver
}

// Service implements the owner-account console use cases.
type Service struct {
	client  *ent.Client
	db      *sql.DB
	runtime ProviderRuntime
	logger  *zap.Logger
	options Options
}

// NewService builds the owner-account service. The Ent client is used for regular
// reads, the SQL handle for database-time writes and comparisons, and it exists
// because freshness, cooldown, caps and stale-heartbeat sweeps must not depend on
// process clocks.
func NewService(client *ent.Client, db *sql.DB, runtime ProviderRuntime, logger *zap.Logger, options Options) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("owner account Ent client is required")
	}
	if db == nil {
		return nil, fmt.Errorf("owner account SQL handle is required")
	}
	if runtime == nil {
		return nil, fmt.Errorf("owner account relay runtime is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	options = withDefaults(options)
	return &Service{client: client, db: db, runtime: runtime, logger: logger, options: options}, nil
}

// observeOperation forwards one bounded outcome to the optional observer. A nil
// observer keeps tests and deployments without metrics a no-op.
func (s *Service) observeOperation(action, outcome string, duration time.Duration) {
	if s.options.Observer == nil {
		return
	}
	s.options.Observer.ObserveOwnerAccountOperation(action, outcome, duration)
}

func withDefaults(options Options) Options {
	if options.OperationTimeout <= 0 {
		options.OperationTimeout = defaultOperationTimeout
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = defaultHeartbeatInterval
	}
	if options.TerminalWriteTimeout <= 0 {
		options.TerminalWriteTimeout = defaultTerminalWriteTimeout
	}
	if options.StaleHeartbeat <= 0 {
		options.StaleHeartbeat = defaultStaleHeartbeat
	}
	if options.RefreshCooldown <= 0 {
		options.RefreshCooldown = defaultRefreshCooldown
	}
	if options.QuotaObservationMaxAge <= 0 {
		options.QuotaObservationMaxAge = defaultQuotaObservationMaxAge
	}
	if options.InvitationObservationAge <= 0 {
		options.InvitationObservationAge = defaultInviteObservationAge
	}
	if options.ResetDailyCap <= 0 {
		options.ResetDailyCap = defaultResetDailyCap
	}
	if options.InvitationDailyCap <= 0 {
		options.InvitationDailyCap = defaultInvitationDailyCap
	}
	if options.UsageBatchLimit <= 0 {
		options.UsageBatchLimit = defaultUsageBatchLimit
	}
	if options.UsageRequestLimit <= 0 {
		options.UsageRequestLimit = defaultUsageRequestLimit
	}
	if options.UsageReadTimeout <= 0 {
		options.UsageReadTimeout = defaultUsageReadTimeout
	}
	if options.QuotaReadTimeout <= 0 {
		options.QuotaReadTimeout = defaultQuotaReadTimeout
	}
	if options.ReferralReadTimeout <= 0 {
		options.ReferralReadTimeout = defaultReferralReadTimeout
	}
	if options.DirectoryReadTimeout <= 0 {
		options.DirectoryReadTimeout = defaultDirectoryReadTimeout
	}
	return options
}

// actor is the signed-in local user whose email derives ownership.
type actor struct {
	ID    int
	Email string
}

// loadActor rejects a missing or locally disabled/offboarded user. The console
// and its APIs stop exposing accounts as soon as the local user is inactive.
func (s *Service) loadActor(ctx context.Context, userID int) (*actor, error) {
	if userID <= 0 {
		return nil, errOwnerInactive()
	}
	user, err := s.client.User.Get(ctx, userID)
	if ent.IsNotFound(err) {
		return nil, errOwnerInactive()
	}
	if err != nil {
		return nil, fmt.Errorf("load owner actor: %w", err)
	}
	offboarding, err := adminuseraccess.OffboardingFactsForUsers(ctx, s.client, []int{user.ID})
	if err != nil {
		return nil, fmt.Errorf("load owner actor offboarding facts: %w", err)
	}
	if adminuseraccess.Disabled(user, offboarding[user.ID].Succeeded) {
		return nil, errOwnerInactive()
	}
	email := normalizeEmail(user.Email)
	if email == "" {
		// An actor without an email cannot own an account. Treat it like an
		// inactive owner instead of matching the empty string.
		return nil, errOwnerInactive()
	}
	return &actor{ID: user.ID, Email: email}, nil
}

// primaryProvider returns the only provider ownership is derived from. A
// deployment without an enabled primary provider has no owner accounts; reads
// return empty results rather than an error.
func (s *Service) primaryProvider(ctx context.Context) (*ent.RelayProvider, error) {
	row, err := s.client.RelayProvider.Query().
		Where(relayprovider.Enabled(true), relayprovider.IsPrimary(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve primary relay provider: %w", err)
	}
	return row, nil
}

// directory loads the owner-safe account directory through the runtime cache.
func (s *Service) directory(ctx context.Context, row *ent.RelayProvider) ([]relay.OwnerAccount, error) {
	if row == nil {
		return []relay.OwnerAccount{}, nil
	}
	readCtx, cancel := context.WithTimeout(ctx, s.options.DirectoryReadTimeout)
	defer cancel()
	accounts, err := s.runtime.OwnerAccounts(readCtx, row.ID, row.ConfigurationVersion, platformOpenAI)
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

// ownedAccounts keeps only the accounts derived from the actor's email, ordered
// by upstream account ID so pagination is stable.
func ownedAccounts(all []relay.OwnerAccount, email string) []relay.OwnerAccount {
	owned := make([]relay.OwnerAccount, 0, len(all))
	for _, account := range all {
		if accountOwnedBy(account, email) {
			owned = append(owned, account)
		}
	}
	return owned
}

// accountOwnedBy implements the agreed derivation: normalize trim+lowercase and
// match the actor email against credentials.email OR an email-shaped name. A
// shadow account, a non-OpenAI platform, a non-OAuth type or a non-primary
// provider never matches.
func accountOwnedBy(account relay.OwnerAccount, email string) bool {
	if email == "" {
		return false
	}
	if normalizeEmail(account.Platform) != platformOpenAI {
		return false
	}
	if normalizeEmail(account.Type) != "oauth" {
		return false
	}
	if account.Shadow {
		return false
	}
	if normalizeEmail(account.Email) == email {
		return true
	}
	if isEmailShaped(account.Name) && normalizeEmail(account.Name) == email {
		return true
	}
	return false
}

// normalizeEmail applies the only normalization the spec allows: trim and
// lowercase. No plus-address handling, aliases or fuzzy matching.
func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// isEmailShaped reports whether a value is a single-@ address with non-empty
// local and domain parts. Only such names may participate in ownership.
func isEmailShaped(value string) bool {
	value = strings.TrimSpace(value)
	if strings.Count(value, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(value, "@")
	return strings.TrimSpace(local) != "" && strings.TrimSpace(domain) != ""
}
