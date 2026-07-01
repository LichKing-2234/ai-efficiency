package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

const userUsageTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

type userUsageResolverFunc func(ctx context.Context, providerID int) (relay.Provider, error)

func (f userUsageResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type userUsageRelayStub struct {
	relay.Provider
	gotLogin    string
	gotPassword string
	gotParams   relay.UserUsageDashboardParams
	apiKeys     []relay.APIKey
	apiKeysErr  error
	subscriptions []relay.UserSubscription
	response    *relay.UserUsageDashboardResponse
	err         error
}

func (s *userUsageRelayStub) GetUserUsageDashboard(ctx context.Context, login, password string, params relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	s.gotLogin = login
	s.gotPassword = password
	s.gotParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *userUsageRelayStub) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	if s.apiKeysErr != nil {
		return nil, s.apiKeysErr
	}
	return s.apiKeys, nil
}

func (s *userUsageRelayStub) ListUserSubscriptions(ctx context.Context, userID int64) ([]relay.UserSubscription, error) {
	return s.subscriptions, nil
}

func newUserUsageTestRouter(t *testing.T, env *testEnv, handler *UserUsageHandler) *gin.Engine {
	t.Helper()
	router := gin.New()
	router.GET("/api/v1/user/usage/dashboard", authpkg.RequireAuth(env.authSvc), handler.Dashboard)
	return router
}

func performUserUsageRequest(router *gin.Engine, token string, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestUserUsageDashboardReturnsConfiguredFalseWithoutRelayPassword(t *testing.T) {
	env := setupTestEnv(t)
	_, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		t.Fatal("resolver should not be called when relay password is missing")
		return nil, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("response should be configured=false: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"trend":[]`) || !strings.Contains(w.Body.String(), `"models":[]`) {
		t.Fatalf("response should contain empty arrays: %s", w.Body.String())
	}
}

func TestUserUsageDashboardReturnsSnapshot(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("set relay user id: %v", err)
	}
	provider, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{
				StartDate:   "2026-06-01",
				EndDate:     "2026-06-06",
				Granularity: "day",
				Timezone:    "Asia/Shanghai",
			},
			Stats: &relay.UserUsageDashboardStats{
				TodayRequests:     12,
				TodayTokens:       3456,
				TodayActualCost:   1.25,
				AverageDurationMs: 850,
				Rpm:               2,
				Tpm:               3000,
			},
			Trend: []relay.UserUsageTrendPoint{{Date: "2026-06-06", TotalTokens: 3456}},
			Models: []relay.UserUsageModelStat{{
				Model:       "example-model",
				Requests:    12,
				TotalTokens: 3456,
				ActualCost:  1.25,
			}},
			GroupQuotas: relay.UserUsageGroupQuotaState{
				Status: "empty",
				Groups: []relay.UserUsageGroupQuotaGroupItem{},
			},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if stub.gotLogin != "admin@test.com" || stub.gotPassword != "test-password" {
		t.Fatalf("credentials = %q/%q", stub.gotLogin, stub.gotPassword)
	}
	if stub.gotParams.StartDate != "2026-06-01" || stub.gotParams.EndDate != "2026-06-06" || stub.gotParams.Granularity != "day" || stub.gotParams.Timezone != "Asia/Shanghai" {
		t.Fatalf("params = %+v", stub.gotParams)
	}
	if !strings.Contains(w.Body.String(), `"today_requests":12`) || strings.Contains(w.Body.String(), "total_api_keys") {
		t.Fatalf("unexpected response body: %s", w.Body.String())
	}
}

func TestUserUsageDashboardIncludesEligibleGroupQuotasFromReusableKeys(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("set relay user id: %v", err)
	}
	provider, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	monthlyLimit := 100.0
	dailyLimit := 10.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{
				StartDate:   "2026-06-01",
				EndDate:     "2026-06-06",
				Granularity: "day",
				Timezone:    "Asia/Shanghai",
			},
			Trend:       []relay.UserUsageTrendPoint{},
			Models:      []relay.UserUsageModelStat{},
			GroupQuotas: relay.UserUsageGroupQuotaState{Status: "empty", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        44,
				UserID:    7,
				Key:       "sk-existing-openai",
				Name:      "admin",
				Status:    "active",
				Quota:     100,
				QuotaUsed: 32.4,
				Group: &relay.Group{
					ID:              42,
					Name:            "Group Alpha",
					Platform:        "openai",
					DailyLimitUSD:   &dailyLimit,
					MonthlyLimitUSD: &monthlyLimit,
				},
			},
			{
				ID:        45,
				UserID:    7,
				Key:       "sk-existing-unlimited",
				Name:      "admin",
				Status:    "active",
				Quota:     0,
				QuotaUsed: 18.2,
				Group: &relay.Group{
					ID:       43,
					Name:     "Group Beta",
					Platform: "anthropic",
				},
			},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_quotas":{"status":"ok"`) {
		t.Fatalf("missing ok group quotas: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_name":"Group Alpha"`) || !strings.Contains(w.Body.String(), `"group_name":"Group Beta"`) {
		t.Fatalf("missing expected group names: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"quota_amount":100`) {
		t.Fatalf("missing finite quota amount: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"is_unlimited":true`) {
		t.Fatalf("missing unlimited group marker: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesSubscriptionUsageForSubscriptionGroupsWithoutKeyQuota(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	monthlyLimit := 200.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-01", EndDate: "2026-06-06", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        46,
				UserID:    7,
				Key:       "sk-subscription",
				Name:      "admin",
				Status:    "active",
				Quota:     0,
				QuotaUsed: 0,
				Group: &relay.Group{
					ID:               99,
					Name:             "Anthropic-RD",
					Platform:         "anthropic",
					SubscriptionType: "subscription",
					MonthlyLimitUSD:  &monthlyLimit,
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{
				ID:              901,
				UserID:          7,
				GroupID:         99,
				Status:          "active",
				MonthlyUsageUSD: 123.45,
			},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-05-18&end_date=2026-06-16&granularity=day")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":123.45`) {
		t.Fatalf("expected subscription monthly usage to be used: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"quota_amount":200`) {
		t.Fatalf("expected subscription monthly limit to be used: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesSubscriptionUsageForUnlimitedSubscriptionGroups(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-01", EndDate: "2026-06-06", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        47,
				UserID:    7,
				Key:       "sk-subscription-unlimited",
				Name:      "admin",
				Status:    "active",
				Quota:     0,
				QuotaUsed: 0,
				Group: &relay.Group{
					ID:               100,
					Name:             "OpenAI-RD",
					Platform:         "openai",
					SubscriptionType: "subscription",
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{
				ID:              902,
				UserID:          7,
				GroupID:         100,
				Status:          "active",
				MonthlyUsageUSD: 88.88,
			},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-05-18&end_date=2026-06-16&granularity=day")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":88.88`) {
		t.Fatalf("expected unlimited subscription usage to be surfaced: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"is_unlimited":true`) {
		t.Fatalf("expected unlimited flag: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"quota_amount":0`) {
		t.Fatalf("unexpected zero quota amount for unlimited subscription: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesDailySubscriptionWindowForTodayRange(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	dailyLimit := 10.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-16", EndDate: "2026-06-16", Granularity: "hour", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        48,
				UserID:    7,
				Key:       "sk-daily",
				Name:      "admin",
				Status:    "active",
				Group: &relay.Group{
					ID:               101,
					Name:             "Gemini-RD",
					Platform:         "gemini",
					SubscriptionType: "subscription",
					DailyLimitUSD:    &dailyLimit,
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{ID: 903, UserID: 7, GroupID: 101, Status: "active", DailyUsageUSD: 6.66, WeeklyUsageUSD: 33.33, MonthlyUsageUSD: 88.88},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-16&end_date=2026-06-16&granularity=hour")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":6.66`) || !strings.Contains(w.Body.String(), `"quota_amount":10`) {
		t.Fatalf("expected daily window usage/limit: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesWeeklySubscriptionWindowFor7DayRange(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	weeklyLimit := 70.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-10", EndDate: "2026-06-16", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        49,
				UserID:    7,
				Key:       "sk-weekly",
				Name:      "admin",
				Status:    "active",
				Group: &relay.Group{
					ID:               102,
					Name:             "OpenAI-RD",
					Platform:         "openai",
					SubscriptionType: "subscription",
					WeeklyLimitUSD:   &weeklyLimit,
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{ID: 904, UserID: 7, GroupID: 102, Status: "active", DailyUsageUSD: 6.66, WeeklyUsageUSD: 44.44, MonthlyUsageUSD: 99.99},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-10&end_date=2026-06-16&granularity=day")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":44.44`) || !strings.Contains(w.Body.String(), `"quota_amount":70`) {
		t.Fatalf("expected weekly window usage/limit: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesDashWhenCurrentWindowHasNoQuotaButOtherWindowDoes(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	monthlyLimit := 1000.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-10", EndDate: "2026-06-16", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        51,
				UserID:    7,
				Key:       "sk-anthropic-rd",
				Name:      "admin",
				Status:    "active",
				Group: &relay.Group{
					ID:               5,
					Name:             "Anthropic-RD",
					Platform:         "anthropic",
					SubscriptionType: "subscription",
					MonthlyLimitUSD:  &monthlyLimit,
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{ID: 906, UserID: 7, GroupID: 5, Status: "active", DailyUsageUSD: 12.34, WeeklyUsageUSD: 56.78, MonthlyUsageUSD: 123.45},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-06-10&end_date=2026-06-16&granularity=day")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":56.78`) {
		t.Fatalf("expected weekly usage to be surfaced: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"quota_amount":1000`) {
		t.Fatalf("expected current-window-unconfigured card to omit monthly quota amount in weekly view: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"is_unlimited":false`) {
		t.Fatalf("expected non-unlimited state when another window has a quota: %s", w.Body.String())
	}
}

func TestUserUsageDashboardUsesMonthlySubscriptionWindowFor30DayRange(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("update user auth/relay id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	monthlyLimit := 200.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-05-18", EndDate: "2026-06-16", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{
			{
				ID:        50,
				UserID:    7,
				Key:       "sk-monthly",
				Name:      "admin",
				Status:    "active",
				Group: &relay.Group{
					ID:               103,
					Name:             "Anthropic-RD",
					Platform:         "anthropic",
					SubscriptionType: "subscription",
					MonthlyLimitUSD:  &monthlyLimit,
				},
			},
		},
		subscriptions: []relay.UserSubscription{
			{ID: 905, UserID: 7, GroupID: 103, Status: "active", DailyUsageUSD: 6.66, WeeklyUsageUSD: 44.44, MonthlyUsageUSD: 123.45},
		},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard?start_date=2026-05-18&end_date=2026-06-16&granularity=day")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"used_amount":123.45`) || !strings.Contains(w.Body.String(), `"quota_amount":200`) {
		t.Fatalf("expected monthly window usage/limit: %s", w.Body.String())
	}
}

func TestUserUsageDashboardReturnsEmptyGroupQuotasWhenNoReusableKeysExist(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("set relay user id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-01", EndDate: "2026-06-06", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeys: []relay.APIKey{},
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_quotas":{"status":"empty"`) {
		t.Fatalf("expected empty group quotas: %s", w.Body.String())
	}
}

func TestUserUsageDashboardReturnsUnavailableGroupQuotasWhenAPIKeyListFails(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayUserID(7).Exec(context.Background()); err != nil {
		t.Fatalf("set relay user id: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{StartDate: "2026-06-01", EndDate: "2026-06-06", Granularity: "day", Timezone: "Asia/Shanghai"},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		},
		apiKeysErr: errors.New("list api keys failed"),
	}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"group_quotas":{"status":"unavailable"`) {
		t.Fatalf("expected unavailable group quotas: %s", w.Body.String())
	}
}

func TestUserUsageDashboardInvalidRelayCredentialsReturnsConflict(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	stub := &userUsageRelayStub{err: relay.ErrInvalidCredentials}
	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return stub, nil
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard")
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Relay credentials need attention") {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}

func TestUserUsageDashboardResolverFailureReturnsUnprocessableEntity(t *testing.T) {
	env := setupTestEnv(t)
	ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := env.client.User.UpdateOneID(env.userID).SetRelayAuthPassword(ciphertext).Exec(context.Background()); err != nil {
		t.Fatalf("update user password: %v", err)
	}
	if _, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("unused").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		Save(context.Background()); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	h := NewUserUsageHandler(env.client, userUsageResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return nil, errors.New("resolve failed")
	}), userUsageTestEncryptionKey)
	router := newUserUsageTestRouter(t, env, h)

	w := performUserUsageRequest(router, env.token, "/api/v1/user/usage/dashboard")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
}
