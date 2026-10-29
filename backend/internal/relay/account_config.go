package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AccountConfiguration is the allow-listed account-level configuration that AI
// Efficiency may read from and apply to an existing Relay account. Credential
// material is deliberately not represented. The allow-list is exhaustive and
// enforced where the value is projected, never at the call site; see
// docs/adr/0004-allow-listed-account-configuration.md.
type AccountConfiguration struct {
	ModelMapping map[string]string `json:"model_mapping,omitempty"`
	Extra        map[string]any    `json:"extra,omitempty"`
}

// AccountConfigurationDiff reports how one target differs from a baseline over
// the allow-listed keys. A target that still reports any key after a copy has
// not converged on the baseline, so Matches is the only result callers may treat
// as success.
type AccountConfigurationDiff struct {
	Matches        bool     `json:"matches"`
	MissingKeys    []string `json:"missing_keys,omitempty"`
	DifferingKeys  []string `json:"differing_keys,omitempty"`
	AdditionalKeys []string `json:"additional_keys,omitempty"`
}

// AccountConfigurationRecord is one account's entry in a configuration read: the
// allow-listed configuration a copy may apply, plus the reset-credit count the
// copy page displays and filters by.
//
// ResetCreditsAvailable is a display-only observation, not configuration. It
// never joins the copy allow-list, is never compared against a baseline, and is
// never written to an account. A nil value means no cached upstream reset-credit
// snapshot has been observed for the account, which is rendered as unknown
// rather than as zero; an observed snapshot reports only credits that have not
// expired.
type AccountConfigurationRecord struct {
	Configuration         AccountConfiguration
	ResetCreditsAvailable *int
}

const (
	accountModelMappingKey = "model_mapping"
	accountExtraKeyPrefix  = "extra."
	// accountResetCreditCountKey is read for display only. Sub2API persists the
	// account's banked ChatGPT/Codex reset credits under this `extra` key, and the
	// copy page shows that count. It is deliberately not in
	// accountConfigurationExtraKeys: copying a reset-credit snapshot between
	// accounts would fabricate observed upstream state.
	accountResetCreditCountKey = "codex_reset_credit_snapshot"
)

// accountConfigurationExtraKeys is the exhaustive set of `extra` keys a copy may
// compare, write, or report. It must stay in step with the copied declarations on
// sub2apiAccountConfigExtra; TestAccountConfigurationAllowListMatchesItsWireStruct
// enforces that, so a field added to one without the other fails tests rather
// than silently widening or narrowing the copy boundary.
var accountConfigurationExtraKeys = []string{
	"privacy_mode",
	"openai_long_context_billing_enabled",
	"openai_oauth_responses_websockets_v2_enabled",
	"openai_oauth_responses_websockets_v2_mode",
	"codex_fingerprint_mode",
	"codex_cli_only",
	"codex_cli_only_allow_app_server",
	"auto_reset_credit_enabled",
	"auto_reset_credit_5h_threshold",
	"auto_reset_credit_7d_threshold",
	"auto_reset_credit_expiry_enabled",
	"auto_reset_credit_expiry_lead_minutes",
	"openai_compact_mode",
}

// accountConfigurationDisplayOnlyExtraKeys is the exhaustive set of `extra` keys
// that may leave this package alongside a configuration read without ever
// joining the copy allow-list. They are projected into their own field on
// AccountConfigurationRecord, never into AccountConfiguration, and
// TestAccountConfigurationDisplayOnlyKeysStayOutOfTheCopyAllowList enforces the
// separation. See docs/adr/0004-allow-listed-account-configuration.md.
var accountConfigurationDisplayOnlyExtraKeys = []string{
	accountResetCreditCountKey,
}

// accountConfigurationExtraDefaults records the effective value Sub2API uses for
// an allow-listed `extra` key the account does not carry.
//
// The auto-reset group is read as "enabled only when explicitly true" with the
// thresholds stored as a ratio (0 disables that window), and Sub2API persists it
// only for OAuth accounts, so an account that never carried it behaves exactly
// like one that stores false/0/1 with expiry auto-use off and a ten-minute lead.
// The expiry switch is independent of the threshold switch; its lead defaults to
// the same ten-minute minimum Sub2API enforces. openai_compact_mode defaults to
// "auto": Sub2API's own account form deletes the key whenever the value is that
// default, while its bulk update materialises "auto" for OAuth accounts, so a
// copy both reads accounts that omit it and produces accounts that carry it.
//
// Comparing raw presence would report either shape as missing or additional
// configuration that no copy could ever converge, and would make a copy write
// values the provider itself treats as their absence.
//
// Values are float64 because they reach this package through encoding/json.
var accountConfigurationExtraDefaults = map[string]any{
	"auto_reset_credit_enabled":             false,
	"auto_reset_credit_5h_threshold":        float64(0),
	"auto_reset_credit_7d_threshold":        float64(1),
	"auto_reset_credit_expiry_enabled":      false,
	"auto_reset_credit_expiry_lead_minutes": float64(10),
	"openai_compact_mode":                   "auto",
}

// extraDefault returns the provider default for an allow-listed key, if the
// provider documents one.
func extraDefault(key string) (any, bool) {
	value, ok := accountConfigurationExtraDefaults[key]
	return value, ok
}

// CompareAccountConfiguration compares one target against a baseline. Keys are
// reported as "model_mapping" and "extra.<key>", sorted, so callers can show and
// store them directly.
func CompareAccountConfiguration(baseline, target AccountConfiguration) AccountConfigurationDiff {
	diff := AccountConfigurationDiff{}

	// model_mapping is compared as one whole value, so it takes part in all three
	// verdicts. A nil and an empty mapping are both "no mapping": comparing them
	// with reflect would report a difference that no copy could resolve.
	baselineHasMapping := len(baseline.ModelMapping) > 0
	targetHasMapping := len(target.ModelMapping) > 0
	switch {
	case baselineHasMapping && !targetHasMapping:
		diff.MissingKeys = append(diff.MissingKeys, accountModelMappingKey)
	case !baselineHasMapping && targetHasMapping:
		diff.AdditionalKeys = append(diff.AdditionalKeys, accountModelMappingKey)
	case baselineHasMapping && !reflect.DeepEqual(baseline.ModelMapping, target.ModelMapping):
		diff.DifferingKeys = append(diff.DifferingKeys, accountModelMappingKey)
	}

	for key, want := range baseline.Extra {
		got, present := target.Extra[key]
		if !present {
			// An absent key the provider defaults to the baseline's value is not a
			// difference: the account already behaves like the baseline.
			if def, ok := extraDefault(key); ok && reflect.DeepEqual(want, def) {
				continue
			}
			diff.MissingKeys = append(diff.MissingKeys, accountExtraKeyPrefix+key)
			continue
		}
		if !reflect.DeepEqual(want, got) {
			diff.DifferingKeys = append(diff.DifferingKeys, accountExtraKeyPrefix+key)
		}
	}

	// A key the baseline does not carry is never copied, so a target that has one
	// cannot converge by copying alone. Report it instead of reporting success,
	// unless it is simply storing the provider default the baseline already
	// implies by not carrying it.
	for key, got := range target.Extra {
		if _, present := baseline.Extra[key]; present {
			continue
		}
		if def, ok := extraDefault(key); ok && reflect.DeepEqual(def, got) {
			continue
		}
		diff.AdditionalKeys = append(diff.AdditionalKeys, accountExtraKeyPrefix+key)
	}

	sort.Strings(diff.MissingKeys)
	sort.Strings(diff.DifferingKeys)
	sort.Strings(diff.AdditionalKeys)
	diff.Matches = len(diff.MissingKeys) == 0 && len(diff.DifferingKeys) == 0 && len(diff.AdditionalKeys) == 0
	return diff
}

// FilterAccountConfiguration drops the named allow-listed keys so an
// administrator can copy the rest of a baseline. Names are the keys the diff
// reports ("model_mapping", "extra.<key>"); an unknown name is ignored, since an
// ignore list can only narrow what a copy writes.
func FilterAccountConfiguration(configuration AccountConfiguration, ignoredKeys []string) AccountConfiguration {
	if len(ignoredKeys) == 0 {
		return configuration
	}
	ignored := make(map[string]struct{}, len(ignoredKeys))
	for _, key := range ignoredKeys {
		if key = strings.TrimSpace(key); key != "" {
			ignored[key] = struct{}{}
		}
	}
	filtered := AccountConfiguration{}
	if _, skip := ignored[accountModelMappingKey]; !skip {
		filtered.ModelMapping = configuration.ModelMapping
	}
	for key, value := range configuration.Extra {
		if _, skip := ignored[accountExtraKeyPrefix+key]; skip {
			continue
		}
		if filtered.Extra == nil {
			filtered.Extra = make(map[string]any, len(configuration.Extra))
		}
		filtered.Extra[key] = value
	}
	return filtered
}

// sub2apiAccountConfigItem is the narrowed decode used by configuration reads.
// Only allow-listed keys are declared, so credential material and every other
// private configuration field stays unmaterialized rather than being decoded and
// then dropped.
type sub2apiAccountConfigItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
	Credentials struct {
		ModelMapping map[string]string `json:"model_mapping"`
	} `json:"credentials"`
	Extra sub2apiAccountConfigExtra `json:"extra"`
}

// sub2apiAccountConfigExtra declares exactly the copied allow-listed extra keys
// plus the display-only keys this read is permitted to observe. Values stay as
// raw JSON so an unexpected upstream type cannot fail the whole read.
type sub2apiAccountConfigExtra struct {
	PrivacyMode                   json.RawMessage `json:"privacy_mode"`
	LongContextBilling            json.RawMessage `json:"openai_long_context_billing_enabled"`
	ResponsesWebsocketsV2         json.RawMessage `json:"openai_oauth_responses_websockets_v2_enabled"`
	ResponsesWebsocketsV2Mode     json.RawMessage `json:"openai_oauth_responses_websockets_v2_mode"`
	CodexFingerprintMode          json.RawMessage `json:"codex_fingerprint_mode"`
	CodexCLIOnly                  json.RawMessage `json:"codex_cli_only"`
	CodexCLIOnlyAllowAppServer    json.RawMessage `json:"codex_cli_only_allow_app_server"`
	AutoResetCreditEnabled        json.RawMessage `json:"auto_reset_credit_enabled"`
	AutoResetCredit5hThreshold    json.RawMessage `json:"auto_reset_credit_5h_threshold"`
	AutoResetCredit7dThreshold    json.RawMessage `json:"auto_reset_credit_7d_threshold"`
	AutoResetCreditExpiryEnabled  json.RawMessage `json:"auto_reset_credit_expiry_enabled"`
	AutoResetCreditExpiryLeadMins json.RawMessage `json:"auto_reset_credit_expiry_lead_minutes"`
	OpenAICompactMode             json.RawMessage `json:"openai_compact_mode"`
	// ResetCreditsSnapshot is display-only; it is projected onto
	// AccountConfigurationRecord.ResetCreditsAvailable, never onto
	// AccountConfiguration.
	ResetCreditsSnapshot json.RawMessage `json:"codex_reset_credit_snapshot"`
}

// toAllowListedMap renders the declared keys as comparable values, keeping a
// JSON null indistinguishable from an absent key.
func (e sub2apiAccountConfigExtra) toAllowListedMap() (map[string]any, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(accountConfigurationExtraKeys))
	for _, key := range accountConfigurationExtraKeys {
		if value, ok := values[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out, nil
}

func allowListedExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(extra))
	for _, key := range accountConfigurationExtraKeys {
		if value, ok := extra[key]; ok {
			out[key] = value
		}
	}
	return out
}

func projectAccountConfiguration(item sub2apiAccountConfigItem) (AccountConfiguration, error) {
	config := AccountConfiguration{}
	if len(item.Credentials.ModelMapping) > 0 {
		config.ModelMapping = item.Credentials.ModelMapping
	}
	extra, err := item.Extra.toAllowListedMap()
	if err != nil {
		return AccountConfiguration{}, err
	}
	if len(extra) > 0 {
		config.Extra = extra
	}
	return config, nil
}

// projectAccountConfigurationRecord assembles one account's configuration read:
// the allow-listed configuration plus the display-only reset-credit count.
func projectAccountConfigurationRecord(item sub2apiAccountConfigItem) (AccountConfigurationRecord, error) {
	config, err := projectAccountConfiguration(item)
	if err != nil {
		return AccountConfigurationRecord{}, err
	}
	return AccountConfigurationRecord{
		Configuration:         config,
		ResetCreditsAvailable: projectResetCreditsAvailable(item.Extra.ResetCreditsSnapshot),
	}, nil
}

// projectResetCreditsAvailable clamps the cached reset-credit snapshot to the
// credits that are still unexpired, which is the same count the owner console
// reports. It is a display-only observation: the value is never compared,
// copied, or stored, and an undecodable snapshot is unknown rather than zero.
func projectResetCreditsAvailable(raw json.RawMessage) *int {
	snapshot, ok := decodeResetCreditsSnapshot(raw)
	if !ok {
		return nil
	}
	available := projectOwnerAccountResetCredits(snapshot.AvailableCount, snapshot.expiries(), time.Now().UTC()).Available
	return &available
}

func decodeAccountConfigPage(data json.RawMessage) ([]sub2apiAccountConfigItem, int, error) {
	var page struct {
		Items []sub2apiAccountConfigItem `json:"items"`
		Pages int                        `json:"pages"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, err
	}
	if page.Pages <= 0 {
		page.Pages = 1
	}
	return page.Items, page.Pages, nil
}

// ReadAccountConfigurations returns the allow-listed configuration of every
// account on one platform, keyed by account ID, together with the display-only
// reset-credit count the copy page shows. An account without configuration is
// present with an empty value so callers can still reason about it.
func (s *sub2apiRelay) ReadAccountConfigurations(ctx context.Context, platform string) (map[int64]AccountConfigurationRecord, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil, fmt.Errorf("relay: read account configurations: platform is required")
	}
	out := map[int64]AccountConfigurationRecord{}
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", "1000")
		query.Set("platform", platform)
		resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/accounts?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: %w", platform, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: read body: %w", platform, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: unexpected status %d%s", platform, resp.StatusCode, relayErrorMessageSuffixFromData(body))
		}
		var envelope struct {
			envelopeStatus
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: decode: %w", platform, err)
		}
		if !envelope.ok() {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: request failed%s", platform, envelope.messageSuffix())
		}
		items, pages, err := decodeAccountConfigPage(envelope.Data)
		if err != nil {
			return nil, fmt.Errorf("relay: read account configurations for platform %s: decode page: %w", platform, err)
		}
		for _, item := range items {
			record, err := projectAccountConfigurationRecord(item)
			if err != nil {
				return nil, fmt.Errorf("relay: read account configurations for platform %s: project account %d: %w", platform, item.ID, err)
			}
			out[item.ID] = record
		}
		if pages <= 1 || page >= pages {
			return out, nil
		}
	}
}

// ApplyAccountConfiguration merges the allow-listed configuration onto existing
// accounts. The upstream call is a single key-level JSONB merge, so it neither
// carries credential material nor replaces the stored credential document, and a
// concurrent token refresh cannot be clobbered in either direction.
func (s *sub2apiRelay) ApplyAccountConfiguration(ctx context.Context, accountIDs []int64, config AccountConfiguration) error {
	if len(accountIDs) == 0 {
		return fmt.Errorf("relay: apply account configuration: at least one account id is required")
	}
	// Omit an empty section instead of sending it as null. The upstream merge is
	// `column || $1::jsonb`, and `jsonb || NULL` resolves to NULL, so a null
	// section would wipe the stored column rather than leaving it alone.
	payload := map[string]any{"account_ids": accountIDs}
	if len(config.ModelMapping) > 0 {
		payload["credentials"] = map[string]any{accountModelMappingKey: config.ModelMapping}
	}
	if extra := allowListedExtra(config.Extra); len(extra) > 0 {
		payload["extra"] = extra
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("relay: apply account configuration: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/accounts/bulk-update", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("relay: apply account configuration: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: apply account configuration: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("relay: apply account configuration: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("relay: apply account configuration: request failed%s", result.messageSuffix())
	}
	return nil
}
