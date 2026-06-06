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
