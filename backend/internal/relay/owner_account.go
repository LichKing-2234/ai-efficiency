package relay

import "time"

// OwnerAccount is the owner-safe projection of one pooled account. It is the
// only account shape the owner-account console may receive, so every field here
// is an explicit disclosure decision:
//
//   - Parent-account identity, notes, proxies and every credential except
//     credentials.email are deliberately absent.
//   - Exactly five `extra` keys may be read, and they are projected into typed
//     snapshots rather than passed through as raw JSON.
//   - A group without a decoded name is omitted instead of being rendered as a
//     numeric ID.
//
// See docs/adr/0004-allow-listed-account-configuration.md for the allow-listing
// precedent this projection follows.
type OwnerAccount struct {
	ID       int64
	Name     string
	Email    string
	Platform string
	Type     string
	Status   string
	// Shadow reports that the account carries a parent-account relation. It is a
	// derived fact; the parent identity itself is never projected.
	Shadow      bool
	Schedulable bool
	// RateLimitedUntil is the upstream rate-limit reset time, when one is known.
	RateLimitedUntil *time.Time
	Groups           []OwnerAccountGroup
	// UsageUpdatedAt is the Codex usage snapshot timestamp. Usage reads report
	// request time instead, so this is the only usable freshness signal; a nil
	// value means the observation time is unknown.
	UsageUpdatedAt         *time.Time
	Credits                *OwnerAccountCredits
	ResetCredits           *OwnerAccountResetCredits
	Referral               *OwnerAccountReferral
	AutoResetCreditEnabled bool
}

// OwnerAccountGroup is one group name an account is bound to. Priority is the
// upstream position and is meaningless when zero.
type OwnerAccountGroup struct {
	ID       int64
	Name     string
	Priority int
}

// OwnerAccountCredits is the Codex spendable credit balance. Balance keeps the
// upstream nullable decimal string so no precision is invented here.
type OwnerAccountCredits struct {
	HasCredits bool
	Unlimited  bool
	Balance    *string
	ObservedAt *time.Time
}

// OwnerAccountResetCredits is the banked ChatGPT/Codex reset-credit count with
// the expiry of every credit that has not expired yet.
type OwnerAccountResetCredits struct {
	Available int
	ExpiresAt []time.Time
}

// OwnerAccountReferral is the invitation eligibility snapshot attached to an
// account's referral program.
type OwnerAccountReferral struct {
	ShouldShow                   bool
	ProgramID                    string
	AvailableInvites             *int
	RequiresExplicitConfirmation *bool
	ObservedAt                   *time.Time
}

// OwnerAccountUsage is one account's usage windows plus the health facts the
// upstream usage read derives. A per-account upstream failure is reported on the
// entry instead of failing the whole batch, so one slow account cannot hide the
// rest of the page.
type OwnerAccountUsage struct {
	AccountID int64
	FiveHour  *OwnerAccountUsageWindow
	SevenDay  *OwnerAccountUsageWindow
	// UpdatedAt is the upstream request time, not an observation time. The
	// console reports OwnerAccount.UsageUpdatedAt instead.
	UpdatedAt *time.Time
	Health    OwnerAccountUsageHealth
	// Err is the upstream failure for this account. Its text is internal: the
	// HTTP layer maps it to a stable reason code and never forwards it.
	Err error
}

// OwnerAccountUsageWindow is one 5h or 7d usage window. Cost is the
// account-multiplied total shown as "A $"; StandardCost is disclosure-limited to
// a tooltip. A nil Cost means the upstream did not report one, and the console
// hides the value rather than substituting standard cost.
type OwnerAccountUsageWindow struct {
	Utilization  float64
	ResetsAt     *time.Time
	Requests     int64
	Tokens       int64
	Cost         *float64
	StandardCost float64
	UserCost     float64
}

// OwnerAccountUsageHealth holds the upstream health facts that are only
// observable from a usage read.
type OwnerAccountUsageHealth struct {
	NeedsReauth bool
	IsBanned    bool
	IsForbidden bool
	// ErrorCode is the machine-readable upstream code (forbidden,
	// unauthenticated, rate_limited, network_error), never upstream error text.
	ErrorCode string
}

// OpenAIQuota is one fresh quota observation: the plan, the two upstream rate
// limit windows, the expiry-clamped banked reset credits, and the spendable Codex
// credit balance.
type OpenAIQuota struct {
	PlanType        string
	PrimaryWindow   *OpenAIQuotaWindow
	SecondaryWindow *OpenAIQuotaWindow
	ResetCredits    *OwnerAccountResetCredits
	Credits         *OwnerAccountCredits
	ObservedAt      *time.Time
}

// OpenAIQuotaWindow is one rate-limit window as the quota endpoint reports it.
// WindowSeconds identifies the window (5h is 18000, 7d is 604800) instead of
// guessing from position.
type OpenAIQuotaWindow struct {
	Utilization   float64
	WindowSeconds int64
	ResetsAt      *time.Time
}

// OpenAIResetCreditResult is the outcome of one consumed reset credit.
type OpenAIResetCreditResult struct {
	Code string
	// WindowsReset is the number of windows the upstream reset, as an integer.
	WindowsReset int
	// CreditExpiresAt is the expiry of the consumed credit, when reported.
	CreditExpiresAt *time.Time
	// WarningCode is a stable upstream warning identifier for a reset whose
	// post-processing was only partial. Empty means no warning.
	WarningCode string
	// Quota is the post-reset quota observation, when the upstream returned one.
	Quota *OpenAIQuota
}

// OpenAIReferral is one fresh invitation eligibility observation.
type OpenAIReferral = OwnerAccountReferral

// OpenAIReferralInviteRequest is one invitation to send. ProgramID and Confirmed
// are taken from a fresh eligibility observation, never from a stale UI cache.
type OpenAIReferralInviteRequest struct {
	Email     string
	ProgramID string
	Confirmed bool
}

// OpenAIReferralInviteResult is the outcome of one sent invitation, together with
// the eligibility refreshed after it. Referral is nil when the post-send refresh
// failed, which must not turn a sent invitation into a failed submission.
type OpenAIReferralInviteResult struct {
	Email    string
	Referral *OpenAIReferral
}
