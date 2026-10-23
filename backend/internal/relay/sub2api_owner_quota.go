package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// sub2apiOpenAIQuotaUsage is the narrowed decode of the OpenAI quota endpoint.
// The upstream payload also carries user and account identifiers and per-feature
// limits; none of them are part of this boundary.
type sub2apiOpenAIQuotaUsage struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		PrimaryWindow   *sub2apiOpenAIQuotaWindow `json:"primary_window"`
		SecondaryWindow *sub2apiOpenAIQuotaWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	RateLimitResetCredits *struct {
		AvailableCount int `json:"available_count"`
		Credits        []struct {
			ExpiresAt string `json:"expires_at"`
		} `json:"credits"`
	} `json:"rate_limit_reset_credits"`
	Credits *struct {
		HasCredits bool    `json:"has_credits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	FetchedAt int64 `json:"fetched_at"`
}

// sub2apiOpenAIQuotaWindow keeps the upstream window identifier explicit. The
// upstream reports reset times as unix seconds here, unlike the usage read.
type sub2apiOpenAIQuotaWindow struct {
	UsedPercent       float64 `json:"used_percent"`
	LimitWindowSecond int64   `json:"limit_window_seconds"`
	ResetAt           int64   `json:"reset_at"`
}

// RefreshOpenAIQuota queries the account's quota and makes Sub2API persist the
// credit snapshots, so the next directory read reflects them.
func (s *sub2apiRelay) RefreshOpenAIQuota(ctx context.Context, accountID int64) (*OpenAIQuota, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("relay: refresh OpenAI quota: account ID must be positive")
	}
	operation := fmt.Sprintf("relay: refresh OpenAI quota for account %d", accountID)
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/quota/refresh", accountID), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	data, err := readOwnerAccountData(operation, resp, true)
	if err != nil {
		return nil, err
	}
	var payload sub2apiOpenAIQuotaUsage
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("%s: decode: %w", operation, err)
		}
	}
	quota, err := projectOpenAIQuota(payload, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return quota, nil
}

// ConsumeOpenAIResetCredit consumes exactly one banked reset credit. The credit is
// irreversible once the upstream call is made, so this performs a single call and
// never retries internally.
func (s *sub2apiRelay) ConsumeOpenAIResetCredit(ctx context.Context, accountID int64) (*OpenAIResetCreditResult, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("relay: consume OpenAI reset credit: account ID must be positive")
	}
	operation := fmt.Sprintf("relay: consume OpenAI reset credit for account %d", accountID)
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/openai/accounts/%d/reset-quota", accountID), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	data, err := readOwnerAccountData(operation, resp, true)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Code string `json:"code"`
		// WindowsReset stays an int all the way through the boundary.
		WindowsReset int `json:"windows_reset"`
		Credit       *struct {
			ExpiresAt string `json:"expires_at"`
		} `json:"credit"`
		WarningCode string                   `json:"warning_code"`
		Quota       *sub2apiOpenAIQuotaUsage `json:"quota"`
	}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("%s: decode: %w", operation, err)
		}
	}
	result := &OpenAIResetCreditResult{
		Code:         payload.Code,
		WindowsReset: payload.WindowsReset,
		WarningCode:  payload.WarningCode,
	}
	if payload.Credit != nil {
		result.CreditExpiresAt = parseOwnerAccountTime(payload.Credit.ExpiresAt)
	}
	if payload.Quota != nil {
		quota, err := projectOpenAIQuota(*payload.Quota, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operation, err)
		}
		result.Quota = quota
	}
	return result, nil
}

func projectOpenAIQuota(payload sub2apiOpenAIQuotaUsage, now time.Time) (*OpenAIQuota, error) {
	quota := &OpenAIQuota{
		PlanType:   payload.PlanType,
		ObservedAt: unixSecondsTime(payload.FetchedAt),
	}
	if payload.RateLimit != nil {
		quota.PrimaryWindow = projectOpenAIQuotaWindow(payload.RateLimit.PrimaryWindow)
		quota.SecondaryWindow = projectOpenAIQuotaWindow(payload.RateLimit.SecondaryWindow)
	}
	if payload.RateLimitResetCredits != nil {
		expiresAt := make([]string, 0, len(payload.RateLimitResetCredits.Credits))
		for _, credit := range payload.RateLimitResetCredits.Credits {
			expiresAt = append(expiresAt, credit.ExpiresAt)
		}
		quota.ResetCredits = projectOwnerAccountResetCredits(payload.RateLimitResetCredits.AvailableCount, expiresAt, now)
	}
	if payload.Credits != nil {
		quota.Credits = &OwnerAccountCredits{
			HasCredits: payload.Credits.HasCredits,
			Unlimited:  payload.Credits.Unlimited,
			Balance:    payload.Credits.Balance,
		}
		if quota.ObservedAt != nil {
			observed := *quota.ObservedAt
			quota.Credits.ObservedAt = &observed
		}
	}
	return quota, nil
}

func projectOpenAIQuotaWindow(window *sub2apiOpenAIQuotaWindow) *OpenAIQuotaWindow {
	if window == nil {
		return nil
	}
	return &OpenAIQuotaWindow{
		Utilization:   window.UsedPercent,
		WindowSeconds: window.LimitWindowSecond,
		ResetsAt:      unixSecondsTime(window.ResetAt),
	}
}

// parseOwnerAccountTime converts an upstream RFC3339 timestamp, reporting nil for
// an absent or malformed value.
func parseOwnerAccountTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil || parsed.IsZero() {
		return nil
	}
	value := parsed.UTC()
	return &value
}
