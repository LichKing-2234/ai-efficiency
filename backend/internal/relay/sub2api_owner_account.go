package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ownerAccountPageSize keeps one directory read to a single upstream page for
// realistic deployments while still walking pagination when there are more.
const ownerAccountPageSize = 1000

// ownerAccountExtraKeys is the exhaustive set of `extra` keys that may leave this
// package for an owner account. `extra` upstream is a denylist, so the allow-list
// has to be explicit: only these five keys are ever read, and they are projected
// into typed snapshots instead of being forwarded as raw JSON.
//
// It must stay in step with sub2apiOwnerAccountExtra;
// TestOwnerAccountExtraAllowListMatchesItsWireStruct enforces that, so a field
// added to one without the other fails tests rather than silently widening the
// boundary.
var ownerAccountExtraKeys = []string{
	"codex_credits_snapshot",
	"codex_reset_credit_snapshot",
	"codex_referral_snapshot",
	"codex_usage_updated_at",
	"auto_reset_credit_enabled",
}

// sub2apiOwnerAccount is the narrowed directory decode. Only fields the owner-safe
// projection may disclose are declared, so parent identity, notes, proxies and
// credential material stay unmaterialized rather than being decoded and then
// dropped.
type sub2apiOwnerAccount struct {
	ID               int64                          `json:"id"`
	Name             string                         `json:"name"`
	Platform         string                         `json:"platform"`
	Type             string                         `json:"type"`
	Status           string                         `json:"status"`
	Schedulable      bool                           `json:"schedulable"`
	RateLimitResetAt *time.Time                     `json:"rate_limit_reset_at"`
	ParentAccountID  *int64                         `json:"parent_account_id"`
	Groups           []sub2apiOwnerAccountGroup     `json:"groups"`
	AccountGroups    []sub2apiOwnerAccountGroupLink `json:"account_groups"`
	Credentials      struct {
		Email string `json:"email"`
	} `json:"credentials"`
	Extra sub2apiOwnerAccountExtra `json:"extra"`
}

type sub2apiOwnerAccountGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type sub2apiOwnerAccountGroupLink struct {
	GroupID  int64                     `json:"group_id"`
	Priority int                       `json:"priority"`
	Group    *sub2apiOwnerAccountGroup `json:"group"`
}

// sub2apiOwnerAccountExtra declares exactly the allow-listed `extra` keys. Values
// stay as raw JSON so an unexpected upstream type cannot fail the whole read.
type sub2apiOwnerAccountExtra struct {
	CreditsSnapshot        json.RawMessage `json:"codex_credits_snapshot"`
	ResetCreditsSnapshot   json.RawMessage `json:"codex_reset_credit_snapshot"`
	ReferralSnapshot       json.RawMessage `json:"codex_referral_snapshot"`
	UsageUpdatedAt         json.RawMessage `json:"codex_usage_updated_at"`
	AutoResetCreditEnabled json.RawMessage `json:"auto_reset_credit_enabled"`
}

// ListOwnerAccounts reads every account on one platform through the owner-safe
// projection. Accounts are ordered by ID so a cached directory is stable.
func (s *sub2apiRelay) ListOwnerAccounts(ctx context.Context, platform string) ([]OwnerAccount, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil, fmt.Errorf("relay: list owner accounts: platform is required")
	}
	var out []OwnerAccount
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(ownerAccountPageSize))
		query.Set("platform", platform)
		// The lite list projection has no embedded group objects, so group names
		// would degrade to numeric IDs. Always ask for the full account shape.
		query.Set("lite", "false")
		operation := fmt.Sprintf("relay: list owner accounts for platform %s", platform)
		resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/accounts?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operation, err)
		}
		data, err := readOwnerAccountData(operation, resp, false)
		if err != nil {
			return nil, err
		}
		items, pages, err := decodeOwnerAccountPage(data)
		if err != nil {
			return nil, fmt.Errorf("%s: decode page: %w", operation, err)
		}
		for _, item := range items {
			account, err := projectOwnerAccount(item)
			if err != nil {
				return nil, fmt.Errorf("%s: project account %d: %w", operation, item.ID, err)
			}
			out = append(out, account)
		}
		if pages <= 1 || page >= pages {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func decodeOwnerAccountPage(data json.RawMessage) ([]sub2apiOwnerAccount, int, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, 1, nil
	}
	if data[0] == '[' {
		var items []sub2apiOwnerAccount
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, 0, err
		}
		return items, 1, nil
	}
	var page struct {
		Items []sub2apiOwnerAccount `json:"items"`
		Pages int                   `json:"pages"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, err
	}
	if page.Pages <= 0 {
		page.Pages = 1
	}
	return page.Items, page.Pages, nil
}

func projectOwnerAccount(item sub2apiOwnerAccount) (OwnerAccount, error) {
	account := OwnerAccount{
		ID:               item.ID,
		Name:             strings.TrimSpace(item.Name),
		Email:            strings.TrimSpace(item.Credentials.Email),
		Platform:         strings.TrimSpace(item.Platform),
		Type:             strings.TrimSpace(item.Type),
		Status:           strings.TrimSpace(item.Status),
		Shadow:           item.ParentAccountID != nil,
		Schedulable:      item.Schedulable,
		RateLimitedUntil: utcTimePtr(item.RateLimitResetAt),
		Groups:           projectOwnerAccountGroups(item),
	}
	autoReset, err := decodeOwnerAccountAutoResetCreditEnabled(item.Extra.AutoResetCreditEnabled)
	if err != nil {
		return OwnerAccount{}, err
	}
	account.AutoResetCreditEnabled = autoReset
	account.UsageUpdatedAt = decodeOwnerAccountUsageUpdatedAt(item.Extra.UsageUpdatedAt)
	account.Credits = decodeOwnerAccountCredits(item.Extra.CreditsSnapshot)
	account.ResetCredits = decodeOwnerAccountResetCreditsSnapshot(item.Extra.ResetCreditsSnapshot)
	account.Referral = decodeOwnerAccountReferralSnapshot(item.Extra.ReferralSnapshot)
	return account, nil
}

// projectOwnerAccountGroups resolves group names from the embedded group objects.
// A group whose name cannot be decoded is omitted: showing a numeric upstream ID
// as a group name would be worse than showing nothing.
func projectOwnerAccountGroups(item sub2apiOwnerAccount) []OwnerAccountGroup {
	namesByID := make(map[int64]string, len(item.Groups))
	for _, group := range item.Groups {
		if name := strings.TrimSpace(group.Name); group.ID > 0 && name != "" {
			namesByID[group.ID] = name
		}
	}
	out := make([]OwnerAccountGroup, 0, len(item.AccountGroups)+len(item.Groups))
	seen := make(map[int64]struct{}, len(item.AccountGroups)+len(item.Groups))
	appendGroup := func(groupID int64, priority int, name string) {
		if groupID <= 0 {
			return
		}
		if _, exists := seen[groupID]; exists {
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[groupID] = struct{}{}
		out = append(out, OwnerAccountGroup{ID: groupID, Name: name, Priority: priority})
	}
	for _, link := range item.AccountGroups {
		name := ""
		if link.Group != nil {
			name = link.Group.Name
		}
		if strings.TrimSpace(name) == "" {
			name = namesByID[link.GroupID]
		}
		appendGroup(link.GroupID, link.Priority, name)
	}
	if len(item.AccountGroups) == 0 {
		for index, group := range item.Groups {
			appendGroup(group.ID, index+1, group.Name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeOwnerAccountAutoResetCreditEnabled reads the flag as "enabled only when
// explicitly true", which is how Sub2API stores the auto-reset trio.
func decodeOwnerAccountAutoResetCreditEnabled(raw json.RawMessage) (bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, nil
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return false, nil
	}
	return enabled, nil
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// unixSecondsTime converts an upstream unix-second timestamp into the RFC3339
// representation every other timestamp in this projection uses. A zero or
// negative value means "not reported".
func unixSecondsTime(seconds int64) *time.Time {
	if seconds <= 0 {
		return nil
	}
	value := time.Unix(seconds, 0).UTC()
	return &value
}

// sub2apiCodexCreditsSnapshot is the persisted Codex credit balance snapshot.
type sub2apiCodexCreditsSnapshot struct {
	Credits *struct {
		HasCredits bool    `json:"has_credits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	FetchedAt int64 `json:"fetched_at"`
}

func decodeOwnerAccountCredits(raw json.RawMessage) *OwnerAccountCredits {
	var snapshot sub2apiCodexCreditsSnapshot
	if !decodeOwnerAccountExtra(raw, &snapshot) {
		return nil
	}
	if snapshot.Credits == nil {
		// A snapshot without a balance assertion describes no credit state; reporting
		// it as "no credits, not unlimited" would invent a balance.
		return nil
	}
	return &OwnerAccountCredits{
		HasCredits: snapshot.Credits.HasCredits,
		Unlimited:  snapshot.Credits.Unlimited,
		Balance:    snapshot.Credits.Balance,
		ObservedAt: unixSecondsTime(snapshot.FetchedAt),
	}
}

// sub2apiResetCreditsSnapshot is the persisted reset-credit snapshot.
type sub2apiResetCreditsSnapshot struct {
	AvailableCount int `json:"available_count"`
	Credits        []struct {
		ExpiresAt string `json:"expires_at"`
	} `json:"credits"`
}

func decodeOwnerAccountResetCreditsSnapshot(raw json.RawMessage) *OwnerAccountResetCredits {
	var snapshot sub2apiResetCreditsSnapshot
	if !decodeOwnerAccountExtra(raw, &snapshot) {
		return nil
	}
	expiresAt := make([]string, 0, len(snapshot.Credits))
	for _, credit := range snapshot.Credits {
		expiresAt = append(expiresAt, credit.ExpiresAt)
	}
	return projectOwnerAccountResetCredits(snapshot.AvailableCount, expiresAt, time.Now().UTC())
}

// projectOwnerAccountResetCredits clamps a reset-credit snapshot to the credits
// that are still live. An expiry that cannot be parsed, and a positive count
// without any expiry detail, cannot be proven live, so both fail closed: offering
// such a credit could spend one that is already gone.
func projectOwnerAccountResetCredits(availableCount int, expiresAt []string, now time.Time) *OwnerAccountResetCredits {
	live := make([]time.Time, 0, len(expiresAt))
	for _, raw := range expiresAt {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil || !parsed.After(now) {
			continue
		}
		live = append(live, parsed.UTC())
	}
	available := len(live)
	// The count is the upstream's own total; the credit details are the only part
	// that can be expiry-clamped. Never report more than both of them support.
	if availableCount > 0 && availableCount < available {
		available = availableCount
	}
	if available == 0 {
		return &OwnerAccountResetCredits{}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].Before(live[j]) })
	// Keep the reported expiries in step with the clamped count so a caller never
	// sees more credit details than credits it may consume.
	return &OwnerAccountResetCredits{Available: available, ExpiresAt: live[:available]}
}

// sub2apiReferralSnapshot is the persisted invitation eligibility snapshot.
type sub2apiReferralSnapshot struct {
	ShouldShow                   bool   `json:"should_show"`
	ProgramID                    string `json:"program_id"`
	AvailableInvites             *int   `json:"available_invites"`
	RequiresExplicitConfirmation *bool  `json:"requires_explicit_confirmation"`
	FetchedAt                    int64  `json:"fetched_at"`
}

func decodeOwnerAccountReferralSnapshot(raw json.RawMessage) *OwnerAccountReferral {
	var snapshot sub2apiReferralSnapshot
	if !decodeOwnerAccountExtra(raw, &snapshot) {
		return nil
	}
	return projectOwnerAccountReferral(snapshot)
}

// projectOwnerAccountReferral is shared by the cached snapshot and the fresh
// referral read so both surfaces report the same shape and observation time.
func projectOwnerAccountReferral(snapshot sub2apiReferralSnapshot) *OwnerAccountReferral {
	return &OwnerAccountReferral{
		ShouldShow:                   snapshot.ShouldShow,
		ProgramID:                    strings.TrimSpace(snapshot.ProgramID),
		AvailableInvites:             snapshot.AvailableInvites,
		RequiresExplicitConfirmation: snapshot.RequiresExplicitConfirmation,
		ObservedAt:                   unixSecondsTime(snapshot.FetchedAt),
	}
}

func decodeOwnerAccountUsageUpdatedAt(raw json.RawMessage) *time.Time {
	var value string
	if !decodeOwnerAccountExtra(raw, &value) {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil || parsed.IsZero() {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

// decodeOwnerAccountExtra decodes one allow-listed raw value, reporting whether
// the key was present at all. A malformed value is treated as absent so one
// unexpected upstream type cannot fail an entire directory read.
func decodeOwnerAccountExtra(raw json.RawMessage, target any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

// readOwnerAccountData reads one read-only admin response and returns its data.
// newEndpoint marks the calls this adapter added for owner accounts: a 404/405
// there means the upstream is older than the verified baseline, not that the
// resource is missing.
func readOwnerAccountData(operation string, resp *http.Response, newEndpoint bool) (json.RawMessage, error) {
	return ownerAccountEnvelope(operation, resp, func(statusCode int, body []byte) error {
		return ownerAccountStatusError(operation, statusCode, body, newEndpoint)
	})
}

// mutateOwnerAccountData reads one owner-account mutation response. An invite has
// no definitive failure signal: the upstream handler answers 502 for its own
// "outcome is unknown" case, so that status must never be reported as a plain
// failure.
func mutateOwnerAccountData(operation string, resp *http.Response) (json.RawMessage, error) {
	return ownerAccountEnvelope(operation, resp, func(statusCode int, body []byte) error {
		return ownerAccountEndpointError(operation, statusCode, body)
	})
}

func ownerAccountEnvelope(operation string, resp *http.Response, statusError func(int, []byte) error) (json.RawMessage, error) {
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("%s: read response: %w", operation, readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp.StatusCode, body)
	}
	var envelope struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", operation, err)
	}
	if !envelope.ok() {
		return nil, fmt.Errorf("%s: request failed%s", operation, envelope.messageSuffix())
	}
	return envelope.Data, nil
}

// ownerAccountStatusError interprets the upstream status codes that carry a stable
// meaning for owner-account capabilities. A 423 means the deployment has not
// accepted Sub2API's administrator compliance commitment, which no retry can
// resolve.
func ownerAccountStatusError(operation string, statusCode int, body []byte, newEndpoint bool) error {
	switch statusCode {
	case http.StatusLocked:
		return fmt.Errorf("%w: %s", ErrComplianceAcknowledgementRequired, operation)
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		if newEndpoint {
			return fmt.Errorf("%w: %s", ErrCapabilityUnsupported, operation)
		}
	}
	return fmt.Errorf("%s: unexpected status %d%s", operation, statusCode, relayErrorMessageSuffixFromData(body))
}

func ownerAccountEndpointError(operation string, statusCode int, body []byte) error {
	if statusCode == http.StatusBadGateway {
		return fmt.Errorf("%w: %s returned %d", ErrUpstreamOutcomeUnknown, operation, statusCode)
	}
	return ownerAccountStatusError(operation, statusCode, body, true)
}
