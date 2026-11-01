// Package accountquota refreshes and displays the quota facts of the OpenAI OAuth
// accounts in the relay pool: the 5h/7d usage windows, the Codex credit balance
// and the banked reset credits. A sweep is an administrator-triggered, async,
// read-only upstream fan-out; it never consumes a reset credit.
package accountquota

import "time"

const (
	// PlatformOpenAI is the only platform whose quota facts this module observes.
	// The relay quota capability is OpenAI-specific.
	PlatformOpenAI = "openai"

	// MaxSweepTargets bounds one sweep. The relay pool is O(100) accounts, so this
	// only stops a mistaken request from fanning out without limit.
	MaxSweepTargets = 200
)

// Sweep lifecycle states. A sweep is created queued, the detached worker marks it
// running, and it ends completed (even with per-account failures), failed when the
// worker could not run at all, or abandoned when no worker finished in time.
const (
	SweepQueued    = "queued"
	SweepRunning   = "running"
	SweepCompleted = "completed"
	SweepFailed    = "failed"
	SweepAbandoned = "abandoned"
)

// Sweep scopes. An all-scope sweep covers every eligible account; a selected
// sweep covers only the explicitly requested account IDs.
const (
	ScopeAll      = "all"
	ScopeSelected = "selected"
)

// SweepResult statuses. A skipped account was never called upstream because its
// auto reset-credit worker is enabled and the operator did not opt in.
const (
	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"
	ResultSkipped   = "skipped"
)

// AccountQuotaGroup is one group name the account is bound to. Priority is the
// upstream position and is meaningless when zero.
type AccountQuotaGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// AccountQuotaAccount is the privacy-safe account identity the quota tab lists.
// It carries no credential material.
type AccountQuotaAccount struct {
	ID                     int64               `json:"id"`
	Name                   string              `json:"name"`
	Email                  string              `json:"email"`
	Status                 string              `json:"status"`
	Schedulable            bool                `json:"schedulable"`
	Shadow                 bool                `json:"shadow"`
	AutoResetCreditEnabled bool                `json:"auto_reset_credit_enabled"`
	Groups                 []AccountQuotaGroup `json:"groups"`
}

// Credits is the Codex spendable credit balance. Balance keeps the upstream
// nullable decimal string so no precision is invented here.
type Credits struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

// ResetCredits is the banked ChatGPT/Codex reset-credit count with the expiry of
// every credit that has not expired yet. It is a display fact only: this module
// never spends a credit.
type ResetCredits struct {
	Available int      `json:"available"`
	ExpiresAt []string `json:"expires_at"`
}

// QuotaWindow is one rate-limit window as the quota endpoint reports it.
// WindowSeconds identifies the window (5h is 18000, 7d is 604800) instead of
// guessing from position.
type QuotaWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowSeconds int64   `json:"window_seconds"`
	ResetsAt      *string `json:"resets_at"`
}

// Observation is one successful upstream quota observation, stored per account so
// a later single-account sweep cannot hide the other accounts' last good facts.
type Observation struct {
	SweepID      int           `json:"sweep_id"`
	ObservedAt   time.Time     `json:"observed_at"`
	PlanType     string        `json:"plan_type"`
	Windows      []QuotaWindow `json:"windows"`
	Credits      *Credits      `json:"credits"`
	ResetCredits *ResetCredits `json:"reset_credits"`
}

// Snapshot is the cached relay directory snapshot. It is the display fallback
// before the first successful observation and may lag behind an observation.
type Snapshot struct {
	ObservedAt     *time.Time    `json:"observed_at"`
	UsageUpdatedAt *time.Time    `json:"usage_updated_at"`
	Credits        *Credits      `json:"credits"`
	ResetCredits   *ResetCredits `json:"reset_credits"`
}

// Item is one row of the quota tab.
type Item struct {
	Account     AccountQuotaAccount `json:"account"`
	Snapshot    *Snapshot           `json:"snapshot"`
	Observation *Observation        `json:"observation"`
}

// Page is the read model of the quota tab.
type Page struct {
	ProviderID  int    `json:"provider_id"`
	Platform    string `json:"platform"`
	Items       []Item `json:"items"`
	Total       int    `json:"total"`
	LatestSweep *Sweep `json:"latest_sweep"`
}

// Sweep is the async job resource the API returns and the page polls.
type Sweep struct {
	ID               int           `json:"id"`
	Status           string        `json:"status"`
	ProviderID       int           `json:"provider_id"`
	Platform         string        `json:"platform"`
	Scope            string        `json:"scope"`
	IncludeAutoReset bool          `json:"include_auto_reset"`
	TotalCount       int           `json:"total_count"`
	ProcessedCount   int           `json:"processed_count"`
	SuccessCount     int           `json:"success_count"`
	SkippedCount     int           `json:"skipped_count"`
	FailedCount      int           `json:"failed_count"`
	CreatedAt        time.Time     `json:"created_at"`
	StartedAt        *time.Time    `json:"started_at"`
	FinishedAt       *time.Time    `json:"finished_at"`
	LastError        string        `json:"last_error"`
	Results          []SweepResult `json:"results"`
}

// SweepResult is one account's terminal outcome inside a sweep. ErrorCode and
// Message are sanitized: neither carries upstream error text.
type SweepResult struct {
	AccountID   int64        `json:"account_id"`
	Status      string       `json:"status"`
	ErrorCode   string       `json:"error_code"`
	Message     string       `json:"message"`
	Observation *Observation `json:"observation"`
}

// StartSweepRequest is one accepted sweep request. Empty AccountIDs means every
// eligible account of the provider.
type StartSweepRequest struct {
	ProviderID       int
	Platform         string
	AccountIDs       []int64
	IncludeAutoReset bool
}
