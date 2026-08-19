package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
)

const userUsageTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

type userUsageResolverFunc func(ctx context.Context, providerID int) (relay.Provider, error)

func (f userUsageResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type userUsageRelayStub struct {
	relay.Provider

	mu            sync.Mutex
	requests      []relay.UserUsageOriginRequest
	response      *relay.UserUsageDashboardResponse
	apiKeys       []relay.APIKey
	subscriptions []relay.UserSubscription
	topErr        error
	usageErr      error
	quotaErr      error
}

func (s *userUsageRelayStub) ReadUserUsageOrigin(_ context.Context, request relay.UserUsageOriginRequest) (*relay.UserUsageOriginResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	topErr := s.topErr
	usageErr := s.usageErr
	quotaErr := s.quotaErr
	response := s.response
	apiKeys := append([]relay.APIKey(nil), s.apiKeys...)
	subscriptions := append([]relay.UserSubscription(nil), s.subscriptions...)
	s.mu.Unlock()
	if topErr != nil {
		return nil, topErr
	}
	result := &relay.UserUsageOriginResult{}
	if request.Branches.Usage {
		result.Usage = response
		result.UsageErr = usageErr
	}
	if request.Branches.Quota {
		result.APIKeys = apiKeys
		result.Subscriptions = subscriptions
		result.QuotaErr = quotaErr
	}
	return result, nil
}

func (s *userUsageRelayStub) setErrors(topErr, usageErr, quotaErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.topErr = topErr
	s.usageErr = usageErr
	s.quotaErr = quotaErr
}

func (s *userUsageRelayStub) requestSnapshot() []relay.UserUsageOriginRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]relay.UserUsageOriginRequest(nil), s.requests...)
}

type userUsageHTTPFixture struct {
	env    *testEnv
	router *gin.Engine
	stub   *userUsageRelayStub
	now    time.Time
}

func newUserUsageHTTPFixture(t *testing.T, configured bool) *userUsageHTTPFixture {
	t.Helper()
	env := setupTestEnv(t)
	if configured {
		ciphertext, err := pkg.Encrypt("test-password", userUsageTestEncryptionKey)
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		if err := env.client.User.UpdateOneID(env.userID).
			SetRelayAuthPassword(ciphertext).
			SetRelayUserID(7).
			Exec(context.Background()); err != nil {
			t.Fatalf("configure user Relay identity: %v", err)
		}
	}
	provider := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("synthetic-encrypted-key").
		SetDefaultModel("example-model").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(context.Background())

	monthlyLimit := 100.0
	stub := &userUsageRelayStub{
		response: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{
				StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day", Timezone: "Asia/Shanghai",
			},
			Stats:  &relay.UserUsageDashboardStats{TotalRequests: 12, TotalTokens: 3456},
			Trend:  []relay.UserUsageTrendPoint{{Date: "2026-07-15", TotalTokens: 3456}},
			Models: []relay.UserUsageModelStat{{Model: "example-model", Requests: 12, TotalTokens: 3456}},
		},
		apiKeys: []relay.APIKey{{
			ID: 11, UserID: 7, Status: "active",
			Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "example", SubscriptionType: "subscription", MonthlyLimitUSD: &monthlyLimit},
		}},
		subscriptions: []relay.UserSubscription{{ID: 12, UserID: 7, GroupID: 42, Status: "active", MonthlyUsageUSD: 25}},
	}
	resolver := userUsageResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("provider ID = %d, want %d", providerID, provider.ID)
		}
		return stub, nil
	})
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	fixture := &userUsageHTTPFixture{
		env: env, stub: stub,
		now: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
	}
	cache, err := personalusage.NewCache(readcache.NewRedisStore(redisClient), personalusage.CacheOptions{
		Namespace:   "test-blue",
		Now:         func() time.Time { return fixture.now },
		RandFloat64: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	service := personalusage.NewService(env.client, resolver, userUsageTestEncryptionKey, cache)
	handler := NewUserUsageHandler(service)
	router := gin.New()
	userGroup := router.Group("/api/v1/user", authpkg.RequireAuth(env.authSvc))
	userGroup.GET("/usage/dashboard", handler.Dashboard)
	userGroup.GET("/usage/group-quotas", handler.GroupQuotas)
	fixture.router = router
	return fixture
}

func (f *userUsageHTTPFixture) get(target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+f.env.token)
	recorder := httptest.NewRecorder()
	f.router.ServeHTTP(recorder, req)
	return recorder
}

func decodeUserUsageData[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var response struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
	return response.Data
}

func TestUserUsageDashboardPreservesCombinedCompatibility(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	recorder := fixture.get("/api/v1/user/usage/dashboard?start_date=2026-07-01&end_date=2026-07-15&granularity=day&timezone=Asia%2FShanghai")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	snapshot := decodeUserUsageData[personalusage.Snapshot](t, recorder)
	if !snapshot.Configured || snapshot.Stats == nil || snapshot.Stats.TotalRequests != 12 || snapshot.UsageFreshness == nil {
		t.Fatalf("usage snapshot = %+v", snapshot)
	}
	if snapshot.GroupQuotas == nil || snapshot.GroupQuotas.Status != "ok" || snapshot.QuotaFreshness == nil || snapshot.QuotaFreshness.SourceStatus != "ok" {
		t.Fatalf("quota snapshot = %+v freshness=%+v", snapshot.GroupQuotas, snapshot.QuotaFreshness)
	}
	requests := fixture.stub.requestSnapshot()
	if len(requests) != 1 || !requests[0].Branches.Usage || !requests[0].Branches.Quota {
		t.Fatalf("origin requests = %+v, want one combined read", requests)
	}
}

func TestUserUsageDashboardUsageOnlyProjection(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	recorder := fixture.get("/api/v1/user/usage/dashboard?start_date=2026-07-01&end_date=2026-07-15&include_group_quotas=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	snapshot := decodeUserUsageData[personalusage.Snapshot](t, recorder)
	if snapshot.UsageFreshness == nil || snapshot.GroupQuotas != nil || snapshot.QuotaFreshness != nil {
		t.Fatalf("usage-only snapshot = %+v", snapshot)
	}
	requests := fixture.stub.requestSnapshot()
	if len(requests) != 1 || !requests[0].Branches.Usage || requests[0].Branches.Quota {
		t.Fatalf("origin requests = %+v, want one usage-only read", requests)
	}
}

func TestUserUsageGroupQuotasReturnsOnlyFreshQuotaProjection(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	recorder := fixture.get("/api/v1/user/usage/group-quotas?start_date=2026-07-01&end_date=2026-07-15")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeUserUsageData[personalusage.GroupQuotaResponse](t, recorder)
	if response.GroupQuotas.Status != "ok" || response.QuotaFreshness.CacheStatus != "uncached" || response.QuotaFreshness.AsOf == nil {
		t.Fatalf("quota response = %+v", response)
	}
	requests := fixture.stub.requestSnapshot()
	if len(requests) != 1 || requests[0].Branches.Usage || !requests[0].Branches.Quota {
		t.Fatalf("origin requests = %+v, want one quota-only read", requests)
	}
	var raw map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	data := raw["data"].(map[string]any)
	if _, exists := data["stats"]; exists {
		t.Fatalf("quota response contains usage fields: %s", recorder.Body.String())
	}
}

func TestUserUsageDashboardRejectsInvalidQuotaProjection(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	recorder := fixture.get("/api/v1/user/usage/dashboard?include_group_quotas=not-a-bool")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if len(fixture.stub.requestSnapshot()) != 0 {
		t.Fatalf("origin called for invalid projection: %+v", fixture.stub.requestSnapshot())
	}
}

func TestUserUsageDashboardReturnsConfiguredFalseWithoutFreshness(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, false)
	recorder := fixture.get("/api/v1/user/usage/dashboard")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	snapshot := decodeUserUsageData[personalusage.Snapshot](t, recorder)
	if snapshot.Configured || snapshot.UsageFreshness != nil || snapshot.QuotaFreshness != nil || snapshot.Trend == nil || snapshot.Models == nil {
		t.Fatalf("unconfigured snapshot = %+v", snapshot)
	}
	if len(fixture.stub.requestSnapshot()) != 0 {
		t.Fatalf("origin called without credentials: %+v", fixture.stub.requestSnapshot())
	}
}

func TestUserUsageDashboardInvalidCredentialsNeverUseStale(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	target := "/api/v1/user/usage/dashboard?start_date=2026-07-01&end_date=2026-07-15&include_group_quotas=false"
	if recorder := fixture.get(target); recorder.Code != http.StatusOK {
		t.Fatalf("seed status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.now = fixture.now.Add(28 * time.Second)
	fixture.stub.setErrors(relay.ErrInvalidCredentials, nil, nil)
	recorder := fixture.get(target)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUserUsageDashboardUsesEligibleStaleOnTransientFailure(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	target := "/api/v1/user/usage/dashboard?start_date=2026-07-01&end_date=2026-07-15"
	if recorder := fixture.get(target); recorder.Code != http.StatusOK {
		t.Fatalf("seed status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.now = fixture.now.Add(28 * time.Second)
	fixture.stub.setErrors(nil, errors.New("synthetic Relay outage"), nil)
	recorder := fixture.get(target)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	snapshot := decodeUserUsageData[personalusage.Snapshot](t, recorder)
	if snapshot.UsageFreshness == nil || snapshot.UsageFreshness.CacheStatus != "stale" || snapshot.UsageFreshness.SourceStatus != "error" {
		t.Fatalf("stale freshness = %+v", snapshot.UsageFreshness)
	}
	if snapshot.GroupQuotas == nil || snapshot.GroupQuotas.Status != "ok" {
		t.Fatalf("fresh quota missing from stale usage response: %+v", snapshot.GroupQuotas)
	}
}

func TestUserUsageQuotaFailureRemainsSectionLocal(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	fixture.stub.setErrors(nil, nil, errors.New("synthetic quota outage"))
	recorder := fixture.get("/api/v1/user/usage/dashboard?start_date=2026-07-01&end_date=2026-07-15")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	snapshot := decodeUserUsageData[personalusage.Snapshot](t, recorder)
	if snapshot.Stats == nil || snapshot.GroupQuotas == nil || snapshot.GroupQuotas.Status != "unavailable" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.QuotaFreshness == nil || snapshot.QuotaFreshness.SourceStatus != "error" || snapshot.QuotaFreshness.AsOf != nil {
		t.Fatalf("quota freshness = %+v", snapshot.QuotaFreshness)
	}
}

func TestUserUsageDashboardColdTransientFailureReturnsBadGateway(t *testing.T) {
	fixture := newUserUsageHTTPFixture(t, true)
	fixture.stub.setErrors(nil, errors.New("synthetic Relay outage"), nil)
	recorder := fixture.get("/api/v1/user/usage/dashboard")
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUserUsageRoutesAreRegisteredAndAuthProtected(t *testing.T) {
	env := setupTestEnvWithProvider(t)
	routes := make(map[string]bool)
	for _, route := range env.router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, target := range []string{"/api/v1/user/usage/dashboard", "/api/v1/user/usage/group-quotas"} {
		if !routes[http.MethodGet+" "+target] {
			t.Fatalf("route %s is not registered", target)
		}
		recorder := httptest.NewRecorder()
		env.router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status = %d, want 401", target, recorder.Code)
		}
	}
}
