package personalusage

import (
	"context"
	"errors"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

var ErrConfiguration = errors.New("personal usage configuration")

type UsageFreshness struct {
	AsOf         time.Time `json:"as_of"`
	FreshUntil   time.Time `json:"fresh_until"`
	StaleUntil   time.Time `json:"stale_until"`
	CacheStatus  string    `json:"cache_status"`
	SourceStatus string    `json:"source_status"`
}

type QuotaFreshness struct {
	AsOf         *time.Time `json:"as_of"`
	CacheStatus  string     `json:"cache_status"`
	SourceStatus string     `json:"source_status"`
}

type Snapshot struct {
	Configured     bool                            `json:"configured"`
	Range          relay.UserUsageDashboardRange   `json:"range"`
	Stats          *relay.UserUsageDashboardStats  `json:"stats"`
	Trend          []relay.UserUsageTrendPoint     `json:"trend"`
	Models         []relay.UserUsageModelStat      `json:"models"`
	UsageFreshness *UsageFreshness                 `json:"usage_freshness,omitempty"`
	GroupQuotas    *relay.UserUsageGroupQuotaState `json:"group_quotas,omitempty"`
	QuotaFreshness *QuotaFreshness                 `json:"quota_freshness,omitempty"`
}

type GroupQuotaResponse struct {
	GroupQuotas    relay.UserUsageGroupQuotaState `json:"group_quotas"`
	QuotaFreshness QuotaFreshness                 `json:"quota_freshness"`
}

type Request struct {
	UserID             int
	Params             relay.UserUsageDashboardParams
	IncludeGroupQuotas bool
}

type CacheKey struct {
	ProviderID      int
	ProviderVersion int64
	ActorID         int
	RelayUserID     int64
	BindingVersion  int64
	Params          relay.UserUsageDashboardParams
}

type OriginLoadResult struct {
	Usage          *relay.UserUsageDashboardResponse
	UsageErr       error
	Quota          relay.UserUsageGroupQuotaState
	QuotaFreshness QuotaFreshness
	QuotaLoaded    bool
}

type OriginLoader func(context.Context, bool) (OriginLoadResult, error)

type CacheResult struct {
	Usage          *relay.UserUsageDashboardResponse
	UsageFreshness UsageFreshness
	Quota          relay.UserUsageGroupQuotaState
	QuotaFreshness QuotaFreshness
	QuotaLoaded    bool
}

type CacheOptions struct {
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

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}
