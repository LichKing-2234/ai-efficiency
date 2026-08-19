package personalusage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const personalUsageTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

type originProviderStub struct {
	relay.Provider
	mu       sync.Mutex
	requests []relay.UserUsageOriginRequest
	read     func(relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error)
	pool     relay.UserUsageGroupPoolUsageState
	poolErr  error
}

func (s *originProviderStub) ReadUserUsageOrigin(_ context.Context, request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()
	return s.read(request)
}

func (s *originProviderStub) requestSnapshot() []relay.UserUsageOriginRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]relay.UserUsageOriginRequest(nil), s.requests...)
}

func (s *originProviderStub) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai"}}, nil
}

func (s *originProviderStub) ReadGroupOAuthPoolUsage(context.Context, []int64) (relay.UserUsageGroupPoolUsageState, error) {
	return s.pool, s.poolErr
}

type providerResolverStub struct {
	provider relay.Provider
	err      error
	ids      []int
}

func (r *providerResolverStub) Resolve(_ context.Context, providerID int) (relay.Provider, error) {
	r.ids = append(r.ids, providerID)
	return r.provider, r.err
}

func createPersonalUsageFixture(t *testing.T, withPassword bool) (*Service, *originProviderStub, int, func() time.Time) {
	t.Helper()
	client := testdb.Open(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	create := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("relay_sso").
		SetRelayUserID(7).
		SetRole("user")
	if withPassword {
		ciphertext, err := pkg.Encrypt("test-password", personalUsageTestEncryptionKey)
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		create.SetRelayAuthPassword(ciphertext)
	}
	user, err := create.Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	providerRow, err := client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("encrypted-test-key").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		SetEnabled(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	monthlyLimit := 100.0
	origin := &originProviderStub{}
	origin.read = func(request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
		result := &relay.UserUsageOriginResult{}
		if request.Branches.Usage {
			result.Usage = testUsageSnapshot(10)
		}
		if request.Branches.Quota {
			result.APIKeys = []relay.APIKey{{
				ID: 11, UserID: 7, Status: "active", Quota: 0,
				Group: &relay.Group{
					ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription", MonthlyLimitUSD: &monthlyLimit,
				},
			}}
			result.Subscriptions = []relay.UserSubscription{{
				ID: 12, UserID: 7, GroupID: 42, Status: "active", MonthlyUsageUSD: 25,
				DailyResetAt:   timePtr(time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)),
				WeeklyResetAt:  timePtr(time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)),
				MonthlyResetAt: timePtr(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)),
			}}
		}
		return result, nil
	}
	resolver := &providerResolverStub{provider: origin}
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	service := NewService(client, resolver, personalUsageTestEncryptionKey, cache)
	if providerRow.ConfigurationVersion != 1 {
		t.Fatalf("provider configuration version = %d", providerRow.ConfigurationVersion)
	}
	return service, origin, user.ID, func() time.Time { return now }
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func TestServiceCombinedColdReadThenWarmReadFetchesQuotaFreshOnly(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	resolver := service.resolver.(*providerResolverStub)
	request := Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{
			StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day", Timezone: "Asia/Shanghai",
		},
		IncludeGroupQuotas: true,
	}

	cold, err := service.Dashboard(context.Background(), request)
	if err != nil {
		t.Fatalf("cold Dashboard() error = %v", err)
	}
	if cold == nil || cold.UsageFreshness == nil || cold.UsageFreshness.CacheStatus != "miss" {
		t.Fatalf("cold snapshot = %+v", cold)
	}
	if cold.GroupQuotas == nil || cold.GroupQuotas.Status != "ok" || len(cold.GroupQuotas.Groups) != 1 {
		t.Fatalf("cold quotas = %+v", cold.GroupQuotas)
	}
	if cold.QuotaFreshness == nil || cold.QuotaFreshness.CacheStatus != "uncached" || cold.QuotaFreshness.SourceStatus != "ok" {
		t.Fatalf("cold quota freshness = %+v", cold.QuotaFreshness)
	}

	warm, err := service.Dashboard(context.Background(), request)
	if err != nil {
		t.Fatalf("warm Dashboard() error = %v", err)
	}
	if warm.UsageFreshness == nil || warm.UsageFreshness.CacheStatus != "fresh" {
		t.Fatalf("warm usage freshness = %+v", warm.UsageFreshness)
	}
	requests := origin.requestSnapshot()
	if len(requests) != 2 {
		t.Fatalf("origin requests = %d, want 2", len(requests))
	}
	if !requests[0].Branches.Usage || !requests[0].Branches.Quota {
		t.Fatalf("cold branches = %+v, want combined", requests[0].Branches)
	}
	if requests[1].Branches.Usage || !requests[1].Branches.Quota {
		t.Fatalf("warm branches = %+v, want quota-only", requests[1].Branches)
	}
	if requests[0].Login != "alice@example.com" || requests[0].Password != "test-password" || requests[0].RelayUserID != 7 {
		t.Fatalf("origin identity = %+v", requests[0])
	}
	if len(resolver.ids) != 2 {
		t.Fatalf("provider resolutions = %d, want one per request", len(resolver.ids))
	}
}

func TestServiceUsageOnlyProjectionNeverReadsQuota(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	snapshot, err := service.Dashboard(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if snapshot.GroupQuotas != nil || snapshot.QuotaFreshness != nil {
		t.Fatalf("usage-only response contains quota: %+v", snapshot)
	}
	if snapshot.Trend == nil || snapshot.Models == nil {
		t.Fatalf("usage-only response must preserve empty arrays: trend=%#v models=%#v", snapshot.Trend, snapshot.Models)
	}
	requests := origin.requestSnapshot()
	if len(requests) != 1 || !requests[0].Branches.Usage || requests[0].Branches.Quota {
		t.Fatalf("origin requests = %+v", requests)
	}
}

func TestGroupQuotasMapsSelectedSubscriptionResetTime(t *testing.T) {
	service, _, userID, _ := createPersonalUsageFixture(t, true)
	response, err := service.GroupQuotas(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("GroupQuotas() error = %v", err)
	}
	if len(response.GroupQuotas.Groups) != 1 || response.GroupQuotas.Groups[0].ResetAt == nil {
		t.Fatalf("group quota reset = %+v, want one reset timestamp", response.GroupQuotas.Groups)
	}
	if got, want := response.GroupQuotas.Groups[0].ResetAt.UTC(), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("reset at = %s, want %s", got, want)
	}
}

func TestGroupQuotasMapsDailyAndWeeklySubscriptionResetTimes(t *testing.T) {
	service, _, userID, _ := createPersonalUsageFixture(t, true)
	tests := []struct {
		name   string
		params relay.UserUsageDashboardParams
		want   time.Time
	}{
		{
			name:   "daily",
			params: relay.UserUsageDashboardParams{StartDate: "2026-07-15", EndDate: "2026-07-15", Granularity: "hour"},
			want:   time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name:   "weekly",
			params: relay.UserUsageDashboardParams{StartDate: "2026-07-09", EndDate: "2026-07-15", Granularity: "day"},
			want:   time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := service.GroupQuotas(context.Background(), Request{UserID: userID, Params: test.params})
			if err != nil {
				t.Fatalf("GroupQuotas() error = %v", err)
			}
			if len(response.GroupQuotas.Groups) != 1 || response.GroupQuotas.Groups[0].ResetAt == nil {
				t.Fatalf("group quota reset = %+v", response.GroupQuotas.Groups)
			}
			if got := response.GroupQuotas.Groups[0].ResetAt.UTC(); !got.Equal(test.want) {
				t.Fatalf("reset at = %s, want %s", got, test.want)
			}
		})
	}
}

func TestGroupQuotasDoesNotMarkAPIKeyQuotaAsSubscriptionReset(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	origin.read = func(request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
		result := &relay.UserUsageOriginResult{}
		if request.Branches.Quota {
			apiKeyQuota := 20.0
			result.APIKeys = []relay.APIKey{{
				ID: 11, UserID: 7, Status: "active", Quota: apiKeyQuota,
				Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"},
			}}
			result.Subscriptions = []relay.UserSubscription{{
				ID: 12, UserID: 7, GroupID: 42, Status: "active",
				MonthlyResetAt: timePtr(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)),
			}}
		}
		return result, nil
	}
	response, err := service.GroupQuotas(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("GroupQuotas() error = %v", err)
	}
	if len(response.GroupQuotas.Groups) != 1 || response.GroupQuotas.Groups[0].ResetAt != nil {
		t.Fatalf("API key quota reset = %+v, want nil", response.GroupQuotas.Groups)
	}
}

func TestGroupPoolUsageReturnsAggregateWithoutBlockingPersonalQuota(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	origin.pool = relay.UserUsageGroupPoolUsageState{
		Status: "ok",
		Groups: []relay.UserUsageGroupPoolUsageGroupItem{{
			GroupID:                  "42",
			Status:                   "partial",
			AverageWeeklyUtilization: 52,
			ValidOAuthAccounts:       1,
			TotalActiveOAuthAccounts: 2,
		}},
	}
	response, err := service.GroupPoolUsage(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("GroupPoolUsage() error = %v", err)
	}
	if response.PoolUsage.Status != "ok" || len(response.PoolUsage.Groups) != 1 {
		t.Fatalf("pool usage = %+v", response.PoolUsage)
	}
	if got := response.PoolUsage.Groups[0].AverageWeeklyUtilization; got != 52 {
		t.Fatalf("average utilization = %v, want 52", got)
	}
}

func TestServiceMissingCredentialsReturnsUnconfiguredWithoutOrigin(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, false)
	snapshot, err := service.Dashboard(context.Background(), Request{
		UserID:             userID,
		Params:             relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
		IncludeGroupQuotas: true,
	})
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if snapshot == nil || snapshot.Configured || snapshot.UsageFreshness != nil || snapshot.QuotaFreshness != nil {
		t.Fatalf("unconfigured snapshot = %+v", snapshot)
	}
	if len(origin.requestSnapshot()) != 0 {
		t.Fatalf("origin called without credentials: %+v", origin.requestSnapshot())
	}
}

func TestServiceQuotaFailureIsSectionLocalAndUncached(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	origin.read = func(request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
		result := &relay.UserUsageOriginResult{}
		if request.Branches.Usage {
			result.Usage = testUsageSnapshot(10)
		}
		if request.Branches.Quota {
			result.QuotaErr = errors.New("synthetic quota outage")
		}
		return result, nil
	}
	snapshot, err := service.Dashboard(context.Background(), Request{
		UserID:             userID,
		Params:             relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
		IncludeGroupQuotas: true,
	})
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if snapshot.Stats == nil || snapshot.Stats.TotalRequests != 10 {
		t.Fatalf("usage missing after quota failure: %+v", snapshot)
	}
	if snapshot.GroupQuotas == nil || snapshot.GroupQuotas.Status != "unavailable" {
		t.Fatalf("quota state = %+v", snapshot.GroupQuotas)
	}
	if snapshot.QuotaFreshness == nil || snapshot.QuotaFreshness.SourceStatus != "error" || snapshot.QuotaFreshness.AsOf != nil {
		t.Fatalf("quota freshness = %+v", snapshot.QuotaFreshness)
	}
}

func TestServiceGroupQuotasUsesQuotaOnlyOrigin(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	response, err := service.GroupQuotas(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("GroupQuotas() error = %v", err)
	}
	if response.GroupQuotas.Status != "ok" || response.QuotaFreshness.SourceStatus != "ok" || response.QuotaFreshness.AsOf == nil {
		t.Fatalf("GroupQuotas() response = %+v", response)
	}
	requests := origin.requestSnapshot()
	if len(requests) != 1 || requests[0].Branches.Usage || !requests[0].Branches.Quota {
		t.Fatalf("origin requests = %+v, want one quota-only read", requests)
	}
}

func TestServicePreservesCredentialAndConfigurationErrors(t *testing.T) {
	request := Request{
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	}
	t.Run("invalid credentials", func(t *testing.T) {
		service, origin, userID, _ := createPersonalUsageFixture(t, true)
		request.UserID = userID
		origin.read = func(relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
			return nil, relay.ErrInvalidCredentials
		}
		if _, err := service.Dashboard(context.Background(), request); !errors.Is(err, relay.ErrInvalidCredentials) {
			t.Fatalf("Dashboard() error = %v, want invalid credentials", err)
		}
	})
	t.Run("decryption", func(t *testing.T) {
		service, origin, userID, _ := createPersonalUsageFixture(t, true)
		request.UserID = userID
		service.encryptionKey = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		if _, err := service.Dashboard(context.Background(), request); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("Dashboard() error = %v, want configuration error", err)
		}
		if len(origin.requestSnapshot()) != 0 {
			t.Fatalf("origin called after decryption failure: %+v", origin.requestSnapshot())
		}
	})
	t.Run("provider resolution", func(t *testing.T) {
		service, _, userID, _ := createPersonalUsageFixture(t, true)
		request.UserID = userID
		service.resolver = &providerResolverStub{err: errors.New("synthetic resolver failure")}
		if _, err := service.Dashboard(context.Background(), request); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("Dashboard() error = %v, want configuration error", err)
		}
	})
}

func TestServiceReturnsEmptyQuotaState(t *testing.T) {
	service, origin, userID, _ := createPersonalUsageFixture(t, true)
	origin.read = func(request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
		result := &relay.UserUsageOriginResult{}
		if request.Branches.Usage {
			result.Usage = testUsageSnapshot(10)
		}
		return result, nil
	}
	response, err := service.GroupQuotas(context.Background(), Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	})
	if err != nil {
		t.Fatalf("GroupQuotas() error = %v", err)
	}
	if response.GroupQuotas.Status != "empty" || response.GroupQuotas.Groups == nil || response.QuotaFreshness.SourceStatus != "ok" {
		t.Fatalf("GroupQuotas() response = %+v", response)
	}
}

func TestQuotaPresentationUsesCurrentDailyWeeklyAndMonthlyWindows(t *testing.T) {
	dailyLimit, weeklyLimit, monthlyLimit := 10.0, 50.0, 200.0
	keys := []relay.APIKey{{
		ID: 11, UserID: 7, Status: "active",
		Group: &relay.Group{
			ID: 42, Name: "Group Alpha", Platform: "example", SubscriptionType: "subscription",
			DailyLimitUSD: &dailyLimit, WeeklyLimitUSD: &weeklyLimit, MonthlyLimitUSD: &monthlyLimit,
		},
	}}
	subscriptions := []relay.UserSubscription{{
		ID: 12, UserID: 7, GroupID: 42, Status: "active",
		DailyUsageUSD: 1.5, WeeklyUsageUSD: 7.5, MonthlyUsageUSD: 25,
	}}
	tests := []struct {
		name       string
		params     relay.UserUsageDashboardParams
		wantUsed   float64
		wantQuota  float64
		wantSource string
	}{
		{name: "daily by hour", params: relay.UserUsageDashboardParams{StartDate: "2026-07-15", EndDate: "2026-07-15", Granularity: "hour"}, wantUsed: 1.5, wantQuota: 10, wantSource: "group_daily_subscription"},
		{name: "weekly by seven days", params: relay.UserUsageDashboardParams{StartDate: "2026-07-09", EndDate: "2026-07-15", Granularity: "day"}, wantUsed: 7.5, wantQuota: 50, wantSource: "group_weekly_subscription"},
		{name: "monthly by longer range", params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"}, wantUsed: 25, wantQuota: 200, wantSource: "group_monthly_subscription"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := mergeGroupQuotas(keys, subscriptions, quotaWindow(tt.params))
			if state.Status != "ok" || len(state.Groups) != 1 {
				t.Fatalf("quota state = %+v", state)
			}
			group := state.Groups[0]
			if group.UsedAmount == nil || *group.UsedAmount != tt.wantUsed || group.QuotaAmount == nil || *group.QuotaAmount != tt.wantQuota || group.QuotaSource != tt.wantSource {
				t.Fatalf("quota group = %+v", group)
			}
		})
	}
}

func TestQuotaPresentationPreservesKeyUnlimitedAndUnconfiguredWindowSemantics(t *testing.T) {
	monthlyLimit := 200.0
	tests := []struct {
		name          string
		key           relay.APIKey
		subscription  relay.UserSubscription
		window        string
		wantUsed      *float64
		wantQuota     *float64
		wantSource    string
		wantUnlimited bool
	}{
		{
			name:   "api key quota takes precedence",
			key:    relay.APIKey{Status: "active", Quota: 100, QuotaUsed: 32.4, Group: &relay.Group{ID: 42}},
			window: "monthly", wantUsed: float64Pointer(32.4), wantQuota: float64Pointer(100), wantSource: "api_key",
		},
		{
			name:         "subscription without any limits is unlimited",
			key:          relay.APIKey{Status: "active", Group: &relay.Group{ID: 42, SubscriptionType: "subscription"}},
			subscription: relay.UserSubscription{MonthlyUsageUSD: 25},
			window:       "monthly", wantUsed: float64Pointer(25), wantSource: "group_subscription_unlimited", wantUnlimited: true,
		},
		{
			name:         "another configured window does not make the selected window unlimited",
			key:          relay.APIKey{Status: "active", Group: &relay.Group{ID: 42, SubscriptionType: "subscription", MonthlyLimitUSD: &monthlyLimit}},
			subscription: relay.UserSubscription{DailyUsageUSD: 6.66},
			window:       "daily", wantUsed: float64Pointer(6.66), wantSource: "group_daily_subscription_window_unconfigured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, quota, source, unlimited := selectQuotaPresentation(&tt.key, tt.subscription, tt.window)
			assertOptionalFloat(t, "used", used, tt.wantUsed)
			assertOptionalFloat(t, "quota", quota, tt.wantQuota)
			if source != tt.wantSource || unlimited != tt.wantUnlimited {
				t.Fatalf("source/unlimited = %q/%v, want %q/%v", source, unlimited, tt.wantSource, tt.wantUnlimited)
			}
		})
	}
}

func assertOptionalFloat(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %v, want %v", name, *got, *want)
	}
}
