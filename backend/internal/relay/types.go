package relay

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

type User struct {
	ID            int64   `json:"id"`
	Email         string  `json:"email"`
	Username      string  `json:"username"`
	Role          string  `json:"role"`
	Notes         string  `json:"notes,omitempty"`
	Concurrency   int     `json:"concurrency,omitempty"`
	AllowedGroups []Group `json:"allowed_groups,omitempty"`
	// AllowedGroupIDs holds IDs that need hydration when allowed_groups is not already a complete group object list.
	AllowedGroupIDs []int64 `json:"-"`
}

type CreateUserRequest struct {
	Username      string  `json:"username"`
	Email         string  `json:"email"`
	Password      string  `json:"password"`
	Role          string  `json:"role,omitempty"`
	Notes         string  `json:"notes,omitempty"`
	Concurrency   int     `json:"concurrency,omitempty"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`
}

type UpdateUserRequest struct {
	Email       string `json:"email,omitempty"`
	Password    string `json:"password,omitempty"`
	Username    string `json:"username,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Concurrency *int   `json:"concurrency,omitempty"`
	Status      string `json:"status,omitempty"`
}

type APIKey struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	Quota      float64    `json:"quota"`
	QuotaUsed  float64    `json:"quota_used"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Group      *Group     `json:"group"`
}

type Group struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Platform         string   `json:"platform"`
	IsExclusive      bool     `json:"is_exclusive,omitempty"`
	SubscriptionType string   `json:"subscription_type,omitempty"`
	RateMultiplier   *float64 `json:"rate_multiplier,omitempty"`
	DailyLimitUSD    *float64 `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD   *float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyLimitUSD  *float64 `json:"monthly_limit_usd,omitempty"`
}

type UserSubscription struct {
	ID              int64   `json:"id"`
	UserID          int64   `json:"user_id"`
	GroupID         int64   `json:"group_id"`
	Status          string  `json:"status"`
	DailyUsageUSD   float64 `json:"daily_usage_usd"`
	WeeklyUsageUSD  float64 `json:"weekly_usage_usd"`
	MonthlyUsageUSD float64 `json:"monthly_usage_usd"`
	Group           *Group  `json:"group,omitempty"`
}

func (u *User) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID            int64           `json:"id"`
		Email         string          `json:"email"`
		Username      string          `json:"username"`
		Role          string          `json:"role"`
		Notes         string          `json:"notes,omitempty"`
		Concurrency   int             `json:"concurrency,omitempty"`
		AllowedGroups json.RawMessage `json:"allowed_groups"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	u.ID = raw.ID
	u.Email = raw.Email
	u.Username = raw.Username
	u.Role = raw.Role
	u.Notes = raw.Notes
	u.Concurrency = raw.Concurrency
	u.AllowedGroups = nil
	u.AllowedGroupIDs = nil

	groups, ids, err := decodeAllowedGroupFacts(raw.AllowedGroups)
	if err != nil {
		return err
	}
	u.AllowedGroups = groups
	u.AllowedGroupIDs = ids
	return nil
}

func decodeAllowedGroupFacts(raw json.RawMessage) ([]Group, []int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil, nil
	}

	var groups []Group
	if err := json.Unmarshal(raw, &groups); err == nil {
		ids := make([]int64, 0, len(groups))
		for _, group := range groups {
			if group.ID > 0 && strings.TrimSpace(group.Platform) == "" {
				ids = append(ids, group.ID)
			}
		}
		return groups, ids, nil
	}

	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, nil, err
	}
	return nil, ids, nil
}

type APIKeyCreateRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	GroupID   string     `json:"group_id,omitempty"`
}

type APIKeyWithSecret struct {
	APIKey
	Secret string `json:"secret"`
}

type UsageLog struct {
	ID           int64     `json:"id"`
	RequestID    string    `json:"request_id"`
	CreatedAt    time.Time `json:"created_at"`
	APIKeyID     int64     `json:"api_key_id"`
	UserID       int64     `json:"user_id"`
	AccountID    string    `json:"account_id"`
	GroupID      string    `json:"group_id"`
	Model        string    `json:"model"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CacheTokens  int64     `json:"cache_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
	TotalCost    float64   `json:"total_cost"`
	ActualCost   float64   `json:"actual_cost"`
}

type UsageStats struct {
	TotalTokens int64   `json:"total_tokens"`
	TotalCost   float64 `json:"total_cost"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	Content    string `json:"content"`
	TokensUsed int    `json:"tokens_used"`
}

type ModelOption struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionWithToolsResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}

type UserUsageDashboardParams struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type UserUsageOriginBranches struct {
	Usage bool
	Quota bool
}

type UserUsageOriginRequest struct {
	Login       string
	Password    string
	RelayUserID int64
	Params      UserUsageDashboardParams
	Branches    UserUsageOriginBranches
}

type UserUsageOriginResult struct {
	Usage         *UserUsageDashboardResponse
	UsageErr      error
	APIKeys       []APIKey
	Subscriptions []UserSubscription
	QuotaErr      error
}

type UserUsageDashboardRange struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type UserUsageDashboardResponse struct {
	Configured  bool                     `json:"configured"`
	Range       UserUsageDashboardRange  `json:"range"`
	Stats       *UserUsageDashboardStats `json:"stats"`
	Trend       []UserUsageTrendPoint    `json:"trend"`
	Models      []UserUsageModelStat     `json:"models"`
	GroupQuotas UserUsageGroupQuotaState `json:"group_quotas"`
}

type UserUsageDashboardStats struct {
	TotalRequests            int64   `json:"total_requests"`
	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	TotalCost                float64 `json:"total_cost"`
	TotalActualCost          float64 `json:"total_actual_cost"`
	TodayRequests            int64   `json:"today_requests"`
	TodayInputTokens         int64   `json:"today_input_tokens"`
	TodayOutputTokens        int64   `json:"today_output_tokens"`
	TodayCacheCreationTokens int64   `json:"today_cache_creation_tokens"`
	TodayCacheReadTokens     int64   `json:"today_cache_read_tokens"`
	TodayTokens              int64   `json:"today_tokens"`
	TodayCost                float64 `json:"today_cost"`
	TodayActualCost          float64 `json:"today_actual_cost"`
	AverageDurationMs        float64 `json:"average_duration_ms"`
	Rpm                      int64   `json:"rpm"`
	Tpm                      int64   `json:"tpm"`
}

type UserUsageTrendPoint struct {
	Date                string  `json:"date"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Cost                float64 `json:"cost"`
	ActualCost          float64 `json:"actual_cost"`
}

type UserUsageModelStat struct {
	Model               string  `json:"model"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Cost                float64 `json:"cost"`
	ActualCost          float64 `json:"actual_cost"`
}

type UserUsageGroupQuotaState struct {
	Status    string                         `json:"status"`
	UnitLabel string                         `json:"unit_label,omitempty"`
	Message   string                         `json:"message,omitempty"`
	Groups    []UserUsageGroupQuotaGroupItem `json:"groups"`
}

type UserUsageGroupQuotaGroupItem struct {
	GroupID     string   `json:"group_id"`
	GroupName   string   `json:"group_name"`
	Platform    string   `json:"platform"`
	UsedAmount  *float64 `json:"used_amount,omitempty"`
	QuotaAmount *float64 `json:"quota_amount,omitempty"`
	IsUnlimited bool     `json:"is_unlimited"`
	QuotaSource string   `json:"quota_source,omitempty"`
}

type TeamUserUsageStats struct {
	UserID           int64    `json:"user_id"`
	TodayActualCost  float64  `json:"today_actual_cost"`
	TotalActualCost  float64  `json:"total_actual_cost"`
	TotalTokens      *int64   `json:"total_tokens,omitempty"`
	RangeActualCost  *float64 `json:"range_actual_cost,omitempty"`
	RangeTotalTokens *int64   `json:"range_total_tokens,omitempty"`
}

type TeamUsageSummaryParams struct {
	StartDate            string `json:"start_date"`
	EndDate              string `json:"end_date"`
	Granularity          string `json:"granularity"`
	Timezone             string `json:"timezone"`
	RequireCompleteRange bool   `json:"-"`
}

type TeamMemberTrendParams struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type UsageTrendPoint struct {
	Date        string  `json:"date"`
	ActualCost  float64 `json:"actual_cost"`
	TotalTokens *int64  `json:"total_tokens,omitempty"`
}

type UserGroupRateEntry struct {
	UserID         int64    `json:"user_id"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	RPMOverride    *int     `json:"rpm_override,omitempty"`
}

type GroupRateMultiplierInput struct {
	UserID         int64    `json:"user_id"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	RPMOverride    *int     `json:"rpm_override,omitempty"`
}
