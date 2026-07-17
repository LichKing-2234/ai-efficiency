package teamusage

import (
	"context"
	"errors"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

var (
	ErrNotRepresentative             = errors.New("not_representative")
	ErrSelfEditForbidden             = errors.New("self_edit_forbidden")
	ErrNotUpperLevelRepresentative   = errors.New("not_upper_level_representative")
	ErrOutOfScope                    = errors.New("out_of_scope")
	ErrNoRelayMapping                = errors.New("no_relay_mapping")
	ErrInactiveSubscription          = errors.New("inactive_subscription")
	ErrPolicyDenied                  = errors.New("policy_denied")
	ErrProviderUnsupported           = errors.New("provider_unsupported")
	ErrMultiplierMetadataUnavailable = errors.New("multiplier_metadata_unavailable")
	ErrPartialFailed                 = errors.New("partial_failed")
	ErrInvalidOverviewParams         = errors.New("invalid_overview_params")
)

const (
	MultiplierMetadataStatusOK          = "ok"
	MultiplierMetadataStatusUnavailable = "unavailable"
)

type ForbiddenError struct {
	Reason string
}

func (e *ForbiddenError) Error() string {
	if e == nil || e.Reason == "" {
		return "forbidden"
	}
	return e.Reason
}

type ScopeResponse struct {
	IsRepresentative bool                                  `json:"is_representative"`
	Departments      []representativescope.DepartmentScope `json:"departments"`
}

type SubjectsResponse struct {
	Subjects []representativescope.Subject `json:"subjects"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
	Total    int                           `json:"total"`
}

type SubjectDashboardResponse struct {
	Subject                   representativescope.Subject    `json:"subject"`
	Configured                bool                           `json:"configured"`
	Range                     relay.UserUsageDashboardRange  `json:"range"`
	Stats                     *relay.UserUsageDashboardStats `json:"stats"`
	Trend                     []relay.UserUsageTrendPoint    `json:"trend"`
	Models                    []relay.UserUsageModelStat     `json:"models"`
	GroupQuotas               relay.UserUsageGroupQuotaState `json:"group_quotas"`
	SubjectSubscriptionGroups []SubscriptionRow              `json:"subject_subscription_groups"`
}

type SubscriptionRow struct {
	GroupID                            string   `json:"group_id"`
	GroupName                          string   `json:"group_name"`
	Platform                           string   `json:"platform"`
	SubscriptionStatus                 string   `json:"subscription_status"`
	GroupDefaultMultiplier             *float64 `json:"group_default_multiplier,omitempty"`
	SystemDefaultMultiplier            float64  `json:"system_default_multiplier"`
	InheritedDefaultMultiplier         float64  `json:"inherited_default_multiplier"`
	UserMultiplier                     *float64 `json:"user_multiplier,omitempty"`
	EffectiveMultiplier                *float64 `json:"effective_multiplier"`
	MultiplierSource                   string   `json:"multiplier_source"`
	MultiplierMetadataStatus           string   `json:"multiplier_metadata_status"`
	MultiplierMetadataMessage          *string  `json:"multiplier_metadata_message,omitempty"`
	DailyLimitUSD                      *float64 `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD                     *float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyLimitUSD                    *float64 `json:"monthly_limit_usd,omitempty"`
	DailyEffectiveAllowanceUSD         *float64 `json:"daily_effective_allowance_usd,omitempty"`
	WeeklyEffectiveAllowanceUSD        *float64 `json:"weekly_effective_allowance_usd,omitempty"`
	MonthlyEffectiveAllowanceUSD       *float64 `json:"monthly_effective_allowance_usd,omitempty"`
	DailyEffectiveAllowanceUnlimited   bool     `json:"daily_effective_allowance_unlimited,omitempty"`
	WeeklyEffectiveAllowanceUnlimited  bool     `json:"weekly_effective_allowance_unlimited,omitempty"`
	MonthlyEffectiveAllowanceUnlimited bool     `json:"monthly_effective_allowance_unlimited,omitempty"`
	DailyDisplayUsedUSD                float64  `json:"daily_display_used_usd"`
	WeeklyDisplayUsedUSD               float64  `json:"weekly_display_used_usd"`
	MonthlyDisplayUsedUSD              float64  `json:"monthly_display_used_usd"`
	DailyUsageUSD                      float64  `json:"daily_usage_usd"`
	WeeklyUsageUSD                     float64  `json:"weekly_usage_usd"`
	MonthlyUsageUSD                    float64  `json:"monthly_usage_usd"`
	UsageValueBasis                    string   `json:"usage_value_basis"`
	QuotaWindowBasis                   string   `json:"quota_window_basis"`
	Editable                           bool     `json:"editable"`
	EditableReason                     *string  `json:"editable_reason,omitempty"`
}

type OverviewParams struct {
	StartDate   string
	EndDate     string
	Granularity string
	Timezone    string
	Page        int
	PageSize    int
}

type OverviewResponse struct {
	Configured       bool                 `json:"configured"`
	IsRepresentative bool                 `json:"is_representative"`
	Window           OverviewWindow       `json:"window"`
	Summary          OverviewSummary      `json:"summary"`
	TopMembers       []OverviewMember     `json:"top_members"`
	TopMemberTrend   TopMemberTrendState  `json:"top_member_trend"`
	DepartmentTrend  DepartmentTrendState `json:"department_trend"`
	Members          []OverviewMember     `json:"members"`
	MemberTree       []OverviewMemberNode `json:"member_tree"`
}

type SnapshotFreshness struct {
	AsOf         time.Time `json:"as_of"`
	FreshUntil   time.Time `json:"fresh_until"`
	StaleUntil   time.Time `json:"stale_until"`
	CacheStatus  string    `json:"cache_status"`
	SourceStatus string    `json:"source_status"`
}

type SummaryResponse struct {
	SnapshotFreshness
	ScopeVersion string          `json:"scope_version"`
	RequestID    string          `json:"request_id"`
	Window       OverviewWindow  `json:"window"`
	Summary      OverviewSummary `json:"summary"`
}

type SnapshotCacheKey struct {
	ProviderID      int
	ProviderVersion int64
	ActorID         int
	ScopeVersion    string
	ScopeHash       string
	Params          OverviewParams
}

type SnapshotOriginLoadResult struct {
	Snapshot    *OverviewResponse
	SnapshotErr error
}

type SnapshotOriginLoader func(context.Context) (SnapshotOriginLoadResult, error)

type SnapshotCacheResult struct {
	Snapshot  *OverviewResponse
	Freshness SnapshotFreshness
}

type SnapshotCacheOptions struct {
	Namespace      string
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	Now            func() time.Time
	RandFloat64    func() float64
	NewToken       func() string
	Sleep          func(context.Context, time.Duration) error
}

type OverviewWindow struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Today       string `json:"today"`
	RollingDays int    `json:"rolling_days"`
	Timezone    string `json:"timezone"`
}

type OverviewSummary struct {
	Unavailable       bool     `json:"unavailable"`
	UnavailableReason *string  `json:"unavailable_reason"`
	MemberCount       int      `json:"member_count"`
	RelayMemberCount  int      `json:"relay_member_count"`
	RangeActualCost   *float64 `json:"range_actual_cost"`
	RangeTotalTokens  *int64   `json:"range_total_tokens,omitempty"`
	TodayActualCost   *float64 `json:"today_actual_cost"`
	TotalActualCost   *float64 `json:"total_actual_cost"`
	UnitLabel         string   `json:"unit_label"`
}

type OverviewMember struct {
	Rank                      int      `json:"rank,omitempty"`
	UserID                    int      `json:"user_id"`
	DirectoryMemberExternalID string   `json:"directory_member_external_id,omitempty"`
	DisplayName               string   `json:"display_name"`
	Email                     string   `json:"email"`
	DepartmentExternalID      string   `json:"department_external_id,omitempty"`
	DepartmentExternalIDs     []string `json:"department_external_ids,omitempty"`
	DepartmentDisplayPath     string   `json:"department_display_path"`
	RelayUserID               *int     `json:"relay_user_id,omitempty"`
	RangeActualCost           float64  `json:"range_actual_cost"`
	TodayActualCost           float64  `json:"today_actual_cost"`
	TotalActualCost           float64  `json:"total_actual_cost"`
	TotalTokens               *int64   `json:"total_tokens,omitempty"`
	SubscriptionCount         *int     `json:"subscription_count"`
	Selectable                bool     `json:"selectable"`
}

type OverviewMemberNode struct {
	DepartmentExternalID string               `json:"department_external_id"`
	ParentExternalID     *string              `json:"parent_external_id,omitempty"`
	Name                 string               `json:"name"`
	DisplayPath          string               `json:"display_path"`
	Depth                int                  `json:"depth"`
	ChildCount           int                  `json:"child_count"`
	MemberCount          int                  `json:"member_count"`
	ConnectedMemberCount int                  `json:"connected_member_count"`
	RangeActualCost      float64              `json:"range_actual_cost"`
	RangeTotalTokens     *int64               `json:"range_total_tokens,omitempty"`
	Members              []OverviewMember     `json:"members"`
	Children             []OverviewMemberNode `json:"children"`
}

type TopMemberTrendState struct {
	UnitLabel         string                 `json:"unit_label"`
	RankBasis         string                 `json:"rank_basis"`
	Unavailable       bool                   `json:"unavailable"`
	UnavailableReason *string                `json:"unavailable_reason"`
	Series            []TopMemberTrendSeries `json:"series"`
}

type TopMemberTrendSeries struct {
	UserID                    int                     `json:"user_id"`
	DirectoryMemberExternalID string                  `json:"directory_member_external_id,omitempty"`
	DisplayName               string                  `json:"display_name"`
	Rank                      int                     `json:"rank"`
	Unavailable               bool                    `json:"unavailable"`
	UnavailableReason         *string                 `json:"unavailable_reason"`
	Points                    []relay.UsageTrendPoint `json:"points"`
}

type DepartmentTrendState struct {
	UnitLabel         string                  `json:"unit_label"`
	Unavailable       bool                    `json:"unavailable"`
	UnavailableReason *string                 `json:"unavailable_reason"`
	Series            []DepartmentTrendSeries `json:"series"`
}

type DepartmentTrendSeries struct {
	SeriesType           string                  `json:"series_type"`
	DepartmentExternalID string                  `json:"department_external_id,omitempty"`
	DisplayName          string                  `json:"display_name"`
	Rank                 int                     `json:"rank,omitempty"`
	Unavailable          bool                    `json:"unavailable"`
	UnavailableReason    *string                 `json:"unavailable_reason"`
	Points               []relay.UsageTrendPoint `json:"points"`
}

type UpdateMultiplierRequest struct {
	Mode           string   `json:"mode"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	Reason         string   `json:"reason,omitempty"`
}

type UpdateMultiplierResponse struct {
	Status                          string   `json:"status"`
	AuditID                         int      `json:"audit_id"`
	GroupID                         string   `json:"group_id"`
	OldMultiplier                   *float64 `json:"old_multiplier,omitempty"`
	OldMultiplierSource             string   `json:"old_multiplier_source"`
	NewMultiplier                   *float64 `json:"new_multiplier,omitempty"`
	NewMultiplierSource             string   `json:"new_multiplier_source"`
	Changed                         bool     `json:"changed"`
	OldEffectiveMonthlyAllowanceUSD *float64 `json:"old_effective_monthly_allowance_usd,omitempty"`
	NewEffectiveMonthlyAllowanceUSD *float64 `json:"new_effective_monthly_allowance_usd,omitempty"`
}

type AuditRecord struct {
	ID                int            `json:"id"`
	ActorUserID       int            `json:"actor_user_id"`
	TargetUserID      *int           `json:"target_user_id,omitempty"`
	TargetDisplayName string         `json:"target_display_name,omitempty"`
	TargetEmail       string         `json:"target_email,omitempty"`
	GroupID           string         `json:"group_id"`
	GroupName         string         `json:"group_name"`
	Action            string         `json:"action"`
	Status            string         `json:"status"`
	OldMultiplier     *float64       `json:"old_multiplier,omitempty"`
	NewMultiplier     *float64       `json:"new_multiplier,omitempty"`
	Changed           bool           `json:"changed"`
	RejectionReason   *string        `json:"rejection_reason,omitempty"`
	RequestMetadata   map[string]any `json:"request_metadata,omitempty"`
	Reason            string         `json:"reason"`
	ErrorMessage      string         `json:"error_message,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type AuditListParams struct {
	Page         int
	PageSize     int
	TargetUserID *int
}

type AdminAuditListParams struct {
	Page         int
	PageSize     int
	ActorUserID  *int
	TargetUserID *int
	Status       string
}

type AuditListResponse struct {
	Items    []AuditRecord `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Total    int           `json:"total"`
}
