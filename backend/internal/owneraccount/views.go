package owneraccount

import (
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

// ScopeResponse reports whether the signed-in user has any owner account and
// whether the two mutations are globally enabled.
type ScopeResponse struct {
	AccountCount     int  `json:"account_count"`
	MutationsEnabled bool `json:"mutations_enabled"`
}

// AccountListResponse is one page of owner accounts plus the exact total.
type AccountListResponse struct {
	Items            []AccountView `json:"items"`
	Total            int           `json:"total"`
	Page             int           `json:"page"`
	PageSize         int           `json:"page_size"`
	MutationsEnabled bool          `json:"mutations_enabled"`
}

// AccountView is the owner-safe account surface. Only fields listed in the
// agreed API contract are disclosed.
type AccountView struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Email      *string       `json:"email"`
	Platform   string        `json:"platform"`
	Type       string        `json:"type"`
	Status     string        `json:"status"`
	GroupNames []string      `json:"group_names"`
	Quota      *QuotaView    `json:"quota"`
	Referral   *ReferralView `json:"referral"`
	Health     HealthView    `json:"health"`
	// BlockedActions lists mutations currently blocked by an unknown outcome. A
	// `reason` of "unknown" disables the action until a successful refresh.
	BlockedActions       []BlockedActionView `json:"blocked_actions"`
	RefreshCooldownUntil *time.Time          `json:"refresh_cooldown_until,omitempty"`
}

// QuotaView is the expiry-clamped banked reset-credit and Codex credit snapshot.
type QuotaView struct {
	ResetCreditsRemaining int        `json:"reset_credits_remaining"`
	ResetCreditExpiries   []string   `json:"reset_credit_expiries"`
	CodexCreditsUnlimited bool       `json:"codex_credits_unlimited"`
	CodexCreditsBalance   *float64   `json:"codex_credits_balance"`
	AutoResetCredit       bool       `json:"auto_reset_credit"`
	ObservedAt            *time.Time `json:"observed_at"`
}

// ReferralView is the invitation eligibility snapshot. ProgramName has no
// upstream source in the owner-safe Relay projection and is always null.
type ReferralView struct {
	Eligible         bool       `json:"eligible"`
	ProgramID        *string    `json:"program_id"`
	ProgramName      *string    `json:"program_name"`
	RemainingInvites int        `json:"remaining_invites"`
	ConsentRequired  *bool      `json:"consent_required"`
	ObservedAt       *time.Time `json:"observed_at"`
}

// HealthView carries the health facts the owner-safe projection can observe:
// schedulability and the rate-limit reset time. Re-auth, ban and forbidden facts
// are derived from a usage read and are reported by the usage surface.
type HealthView struct {
	Schedulable      bool       `json:"schedulable"`
	ReauthRequired   bool       `json:"reauth_required"`
	Banned           bool       `json:"banned"`
	Forbidden        bool       `json:"forbidden"`
	RateLimitResetAt *time.Time `json:"rate_limit_reset_at"`
}

// BlockedActionView identifies a mutation that is disabled for one account.
type BlockedActionView struct {
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	OperationID *int   `json:"operation_id,omitempty"`
}

// UsageWindowView is one 5h or 7d usage window. A window the upstream did not
// report is still present with null members, so the page never has to special
// case its absence.
type UsageWindowView struct {
	UtilizationPercent *float64   `json:"utilization_percent"`
	ResetsAt           *time.Time `json:"resets_at"`
	Requests           *int64     `json:"requests"`
	Tokens             *int64     `json:"tokens"`
	// APICostUSD is the upstream account-multiplied cost ("A $"), never the
	// unmultiplied standard cost. StandardCostUSD exists only for the tooltip.
	APICostUSD      *float64 `json:"api_cost_usd"`
	StandardCostUSD *float64 `json:"standard_cost_usd"`
	UserCostUSD     *float64 `json:"user_cost_usd"`
}

// UsageView is one account's usage windows. ObservedAt is the account's stored
// usage snapshot timestamp, because the usage read's own updated_at is request
// time.
type UsageView struct {
	FiveHour   UsageWindowView `json:"five_hour"`
	SevenDay   UsageWindowView `json:"seven_day"`
	ObservedAt *time.Time      `json:"observed_at"`
}

// UsageItem is one account's usage in a batch response.
type UsageItem struct {
	AccountID int64     `json:"account_id"`
	Usage     UsageView `json:"usage"`
}

// UsageError is one per-account failure in a batch response.
type UsageError struct {
	AccountID int64  `json:"account_id"`
	Code      string `json:"code"`
}

// UsageBatchResponse reports partial usage failures per account.
type UsageBatchResponse struct {
	Items  []UsageItem  `json:"items"`
	Errors []UsageError `json:"errors"`
}

// UsageResponse is the explicit usage refresh result.
type UsageResponse struct {
	AccountID int64     `json:"account_id"`
	Usage     UsageView `json:"usage"`
}

// QuotaResponse is the explicit quota refresh result.
type QuotaResponse struct {
	AccountID int64     `json:"account_id"`
	Quota     QuotaView `json:"quota"`
}

// ReferralResponse is the explicit referral refresh result.
type ReferralResponse struct {
	AccountID int64         `json:"account_id"`
	Referral  *ReferralView `json:"referral"`
}

// OperationView is the sanitized async operation surface.
type OperationView struct {
	ID          int            `json:"id"`
	AccountID   int64          `json:"account_id"`
	ActorUserID int            `json:"actor_user_id"`
	Action      string         `json:"action"`
	Status      string         `json:"status"`
	ErrorCode   *string        `json:"error_code,omitempty"`
	WarningCode *string        `json:"warning_code,omitempty"`
	InviteEmail *string        `json:"invite_email,omitempty"`
	Reason      *string        `json:"reason,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// OperationPage is one page of operation history with an exact total.
type OperationPage struct {
	Items    []OperationView `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// AcceptedOperation is the 202 body of an accepted mutation.
type AcceptedOperation struct {
	OperationID int `json:"operation_id"`
}

// accountView projects one relay account. facts may be nil for surfaces that do
// not expose local operation state (it is never nil for the list).
func (s *Service) accountView(account relay.OwnerAccount, facts *accountOperationFacts, now time.Time) AccountView {
	groupNames := make([]string, 0, len(account.Groups))
	for _, group := range account.Groups {
		name := strings.TrimSpace(group.Name)
		if name != "" {
			groupNames = append(groupNames, name)
		}
	}
	view := AccountView{
		ID:         account.ID,
		Name:       account.Name,
		Email:      stringPtr(account.Email),
		Platform:   account.Platform,
		Type:       account.Type,
		Status:     account.Status,
		GroupNames: groupNames,
		Quota:      quotaViewFromAccount(account),
		Referral:   referralViewFromSnapshot(account.Referral),
		Health: HealthView{
			Schedulable:      account.Schedulable,
			RateLimitResetAt: account.RateLimitedUntil,
		},
	}
	if facts != nil {
		view.BlockedActions = blockedActions(facts)
		view.RefreshCooldownUntil = refreshCooldownUntil(facts, s.options.RefreshCooldown, now)
	}
	return view
}

func quotaViewFromAccount(account relay.OwnerAccount) *QuotaView {
	if account.ResetCredits == nil && account.Credits == nil {
		return nil
	}
	view := &QuotaView{AutoResetCredit: account.AutoResetCreditEnabled, ResetCreditExpiries: []string{}}
	if account.ResetCredits != nil {
		view.ResetCreditsRemaining = account.ResetCredits.Available
		for _, expiry := range account.ResetCredits.ExpiresAt {
			view.ResetCreditExpiries = append(view.ResetCreditExpiries, expiry.UTC().Format(time.RFC3339))
		}
	}
	if account.Credits != nil {
		view.CodexCreditsUnlimited = account.Credits.Unlimited
		view.CodexCreditsBalance = parseCreditBalance(account.Credits.Balance)
		view.ObservedAt = account.Credits.ObservedAt
	}
	return view
}

// quotaObservationResult is the sanitized quota fact persisted on a succeeded
// refresh_quota or reset_credit row. It is the only source mutation gates may use
// for reset credits, so it deliberately omits display-only fields.
func quotaObservationResult(quota *relay.OpenAIQuota) map[string]any {
	view := quotaViewFromObservation(quota, false)
	return map[string]any{
		"reset_credits_remaining": view.ResetCreditsRemaining,
		"reset_credit_expiries":   view.ResetCreditExpiries,
	}
}

// referralObservationResult is the sanitized eligibility fact persisted on a
// succeeded refresh_referral or send_invitation row. A nil program id is stored as
// an empty string so the invitation gate can still tell "no program" from "no
// observation".
func referralObservationResult(referral *relay.OwnerAccountReferral) map[string]any {
	view := referralViewFromSnapshot(referral)
	if view == nil {
		return nil
	}
	programID := ""
	if view.ProgramID != nil {
		programID = *view.ProgramID
	}
	result := map[string]any{
		"eligible":          view.Eligible,
		"program_id":        programID,
		"remaining_invites": view.RemainingInvites,
	}
	if view.ConsentRequired != nil {
		result["consent_required"] = *view.ConsentRequired
	}
	return result
}

// mergeObservationResult adds sanitized observation fields to an operation result
// without dropping the mutation-specific fields the history surface renders.
func mergeObservationResult(result, observation map[string]any) map[string]any {
	if len(observation) == 0 {
		return result
	}
	if result == nil {
		result = make(map[string]any, len(observation))
	}
	for key, value := range observation {
		result[key] = value
	}
	return result
}

func quotaViewFromObservation(quota *relay.OpenAIQuota, autoResetCredit bool) QuotaView {
	view := QuotaView{AutoResetCredit: autoResetCredit, ResetCreditExpiries: []string{}}
	if quota == nil {
		return view
	}
	view.ObservedAt = quota.ObservedAt
	if quota.ResetCredits != nil {
		view.ResetCreditsRemaining = quota.ResetCredits.Available
		for _, expiry := range quota.ResetCredits.ExpiresAt {
			view.ResetCreditExpiries = append(view.ResetCreditExpiries, expiry.UTC().Format(time.RFC3339))
		}
	}
	if quota.Credits != nil {
		view.CodexCreditsUnlimited = quota.Credits.Unlimited
		view.CodexCreditsBalance = parseCreditBalance(quota.Credits.Balance)
		if quota.Credits.ObservedAt != nil {
			view.ObservedAt = quota.Credits.ObservedAt
		}
	}
	return view
}

// referralViewFromSnapshot projects the cached referral snapshot. ProgramName has
// no source in the projection and stays null.
func referralViewFromSnapshot(referral *relay.OwnerAccountReferral) *ReferralView {
	if referral == nil {
		return nil
	}
	view := &ReferralView{
		Eligible:         referral.ShouldShow,
		ProgramID:        stringPtr(strings.TrimSpace(referral.ProgramID)),
		ConsentRequired:  referral.RequiresExplicitConfirmation,
		ObservedAt:       referral.ObservedAt,
		RemainingInvites: 0,
	}
	if referral.AvailableInvites != nil {
		view.RemainingInvites = *referral.AvailableInvites
	}
	return view
}

func usageView(usage relay.OwnerAccountUsage, observedAt *time.Time) UsageView {
	return UsageView{
		FiveHour:   usageWindowView(usage.FiveHour),
		SevenDay:   usageWindowView(usage.SevenDay),
		ObservedAt: observedAt,
	}
}

func usageWindowView(window *relay.OwnerAccountUsageWindow) UsageWindowView {
	view := UsageWindowView{}
	if window == nil {
		return view
	}
	utilization := window.Utilization
	requests := window.Requests
	tokens := window.Tokens
	standardCost := window.StandardCost
	userCost := window.UserCost
	view.UtilizationPercent = &utilization
	view.ResetsAt = window.ResetsAt
	view.Requests = &requests
	view.Tokens = &tokens
	view.APICostUSD = window.Cost
	view.StandardCostUSD = &standardCost
	view.UserCostUSD = &userCost
	return view
}

func blockedActions(facts *accountOperationFacts) []BlockedActionView {
	blocked := make([]BlockedActionView, 0, 2)
	if reset, ok := facts.Latest["reset_credit"]; ok && reset.Status == "unknown" {
		if observed := facts.quotaObservedAt(); observed == nil || !observed.After(reset.CreatedAt) {
			operationID := reset.ID
			blocked = append(blocked, BlockedActionView{
				Action: "reset_credit", Reason: "unknown", OperationID: &operationID,
			})
		}
	}
	if invite, ok := facts.Latest["send_invitation"]; ok && invite.Status == "unknown" {
		if observed := facts.referralObservedAt(); observed == nil || !observed.After(invite.CreatedAt) {
			operationID := invite.ID
			blocked = append(blocked, BlockedActionView{
				Action: "send_invitation", Reason: "unknown", OperationID: &operationID,
			})
		}
	}
	return blocked
}

// refreshCooldownUntil reports when the next explicit refresh may run. It is
// derived from the most recent refresh row of either refresh action, so the three
// refresh endpoints share one cooldown per account.
func refreshCooldownUntil(facts *accountOperationFacts, cooldown time.Duration, now time.Time) *time.Time {
	var latest *time.Time
	for _, action := range []string{"refresh_quota", "refresh_referral"} {
		if operation, ok := facts.Latest[action]; ok {
			candidate := operation.CreatedAt
			if latest == nil || candidate.After(*latest) {
				latest = &candidate
			}
		}
	}
	if latest == nil {
		return nil
	}
	until := latest.Add(cooldown)
	if !until.After(now) {
		return nil
	}
	utc := until.UTC()
	return &utc
}

// parseCreditBalance converts the upstream decimal string into the number the
// console renders. A missing or malformed value stays null rather than inventing
// a balance.
func parseCreditBalance(balance *string) *float64 {
	if balance == nil {
		return nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(*balance), 64)
	if err != nil {
		return nil
	}
	return &value
}
