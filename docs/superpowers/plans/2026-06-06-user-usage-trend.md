# User Usage Dashboard Snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current three-endpoint `/user/usage` trend page with an ai-efficiency usage-only dashboard backed by one snapshot endpoint.

**Architecture:** The frontend calls one AE endpoint, `GET /api/v1/user/usage/dashboard`, with date range parameters. The backend resolves the current user, decrypts their stored relay password once, resolves the primary relay provider, logs in to sub2api once, calls the three sub2api user dashboard endpoints with that JWT, and returns one AE-shaped snapshot that excludes sub2api account-management fields.

**Tech Stack:** Go, Gin, Ent, AES-GCM credential decrypt, sub2api HTTP user API, Vue 3, Vite, Pinia, TailwindCSS, axios, chart.js, vue-chartjs, Vitest.

---

## File Structure

### Backend

| File | Responsibility |
| --- | --- |
| `backend/internal/relay/types.go` | Replace the current separate stats/trend/models types with one snapshot contract and nested usage-only structs. |
| `backend/internal/relay/provider.go` | Replace the three user usage Provider methods with `GetUserUsageDashboard`. |
| `backend/internal/relay/sub2api.go` | Replace three public usage methods with one snapshot method that logs in once and fetches stats, trend, and models. |
| `backend/internal/relay/sub2api_test.go` | Replace old three-endpoint tests with snapshot tests. |
| `backend/internal/handler/user_usage.go` | Replace `Stats`, `Trend`, and `Models` handlers with `Dashboard`. |
| `backend/internal/handler/user_usage_test.go` | Create focused handler tests for configured=false, success, and invalid relay credentials. |
| `backend/internal/handler/router.go` | Register `GET /api/v1/user/usage/dashboard` and remove old `/stats`, `/trend`, `/models` routes. |
| `backend/internal/auth/sso_test.go` | Update mock relay provider to satisfy the new Provider interface. |
| `backend/internal/attribution/service_test.go` | Update fake relay provider to satisfy the new Provider interface. |
| `backend/internal/usersetup/service_test.go` | Update fake relay provider to satisfy the new Provider interface. |

### Frontend

| File | Responsibility |
| --- | --- |
| `frontend/package.json` | Add `chart.js` and `vue-chartjs`. |
| `frontend/package-lock.json` | Refresh after dependency install because CI and Docker builds use npm lockfile. |
| `frontend/src/types/index.ts` | Add snapshot, stats, trend, model, params, and range types. |
| `frontend/src/api/userUsage.ts` | Replace three API functions with one `getUserUsageDashboard` function. |
| `frontend/src/__tests__/user-usage-api.test.ts` | Verify API path and params. |
| `frontend/src/views/user/UsageView.vue` | Rewrite page to load one snapshot and render range controls, cards, and charts. |
| `frontend/src/__tests__/user-usage-view.test.ts` | Verify configured=false, success rendering, range changes, and 409 handling. |
| `frontend/src/components/user/usage/UsageStatsCards.vue` | Rewrite cards for Today Cost, Today Requests, Today Tokens, and Avg Response. |
| `frontend/src/components/user/usage/UsageTrendChart.vue` | Replace hand-rolled bars with chart.js line chart. |
| `frontend/src/components/user/usage/UsageModelChart.vue` | Rewrite model distribution as usage table with optional doughnut chart. |

---

## Task 1: Backend Relay Snapshot Contract

**Files:**
- Modify: `backend/internal/relay/sub2api_test.go`
- Modify: `backend/internal/relay/types.go`
- Modify: `backend/internal/relay/provider.go`
- Modify: `backend/internal/relay/sub2api.go`

- [x] **Step 1: Replace old relay usage tests with snapshot tests**

In `backend/internal/relay/sub2api_test.go`, remove the current `TestGetUserUsageStats`, `TestGetUserUsageTrend`, and `TestGetUserUsageModels` tests. Add these tests in their place:

```go
func TestGetUserUsageDashboard(t *testing.T) {
	var loginCount int
	var meCount int
	var seenPaths []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		loginCount++
		if r.Method != http.MethodPost {
			t.Errorf("login method = %s, want POST", r.Method)
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body.Email != "alice@example.com" || body.Password != "test-password" {
			t.Fatalf("login body = %+v, want alice credentials", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "test-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		meCount++
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("/me Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 1, "username": "alice", "email": "alice@example.com"},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("stats Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"total_api_keys":              99,
				"active_api_keys":             88,
				"total_requests":              150,
				"total_input_tokens":          50000,
				"total_output_tokens":         30000,
				"total_cache_creation_tokens": 4000,
				"total_cache_read_tokens":     10000,
				"total_tokens":                94000,
				"total_cost":                  2.50,
				"total_actual_cost":           2.00,
				"today_requests":              20,
				"today_input_tokens":          8000,
				"today_output_tokens":         5000,
				"today_cache_creation_tokens": 600,
				"today_cache_read_tokens":     700,
				"today_tokens":                14300,
				"today_cost":                  0.35,
				"today_actual_cost":           0.28,
				"average_duration_ms":         1250.5,
				"rpm":                         3,
				"tpm":                         4200,
				"by_platform":                 []map[string]any{{"platform": "openai", "total_actual_cost": 2.00}},
			},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("trend Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2026-06-01" || q.Get("end_date") != "2026-06-06" || q.Get("granularity") != "day" || q.Get("timezone") != "Asia/Shanghai" {
			t.Fatalf("trend query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"start_date":  "2026-06-01",
				"end_date":    "2026-06-06",
				"granularity": "day",
				"trend": []map[string]any{
					{
						"date":                  "2026-06-06",
						"requests":              20,
						"input_tokens":          8000,
						"output_tokens":         5000,
						"cache_creation_tokens": 600,
						"cache_read_tokens":     700,
						"total_tokens":          14300,
						"cost":                  0.35,
						"actual_cost":           0.28,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/models", func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer test-jwt-token" {
			t.Fatalf("models Authorization = %q, want user JWT", r.Header.Get("Authorization"))
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2026-06-01" || q.Get("end_date") != "2026-06-06" || q.Get("timezone") != "Asia/Shanghai" {
			t.Fatalf("models query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"start_date": "2026-06-01",
				"end_date":   "2026-06-06",
				"models": []map[string]any{
					{
						"model":                 "example-model",
						"requests":              12,
						"input_tokens":          10000,
						"output_tokens":         5000,
						"cache_creation_tokens": 200,
						"cache_read_tokens":     300,
						"total_tokens":          15500,
						"cost":                  0.75,
						"actual_cost":           0.60,
					},
				},
			},
		})
	})

	p := newTestProvider(t, mux)
	got, err := p.GetUserUsageDashboard(context.Background(), "alice@example.com", "test-password", relay.UserUsageDashboardParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-06",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUserUsageDashboard() unexpected error: %v", err)
	}
	if loginCount != 1 || meCount != 1 {
		t.Fatalf("loginCount=%d meCount=%d, want one login and one /me", loginCount, meCount)
	}
	wantPaths := []string{
		"/api/v1/usage/dashboard/stats",
		"/api/v1/usage/dashboard/trend?end_date=2026-06-06&granularity=day&start_date=2026-06-01&timezone=Asia%2FShanghai",
		"/api/v1/usage/dashboard/models?end_date=2026-06-06&start_date=2026-06-01&timezone=Asia%2FShanghai",
	}
	if diff := cmp.Diff(wantPaths, seenPaths); diff != "" {
		t.Fatalf("paths mismatch (-want +got):\n%s", diff)
	}
	if !got.Configured {
		t.Fatal("Configured = false, want true")
	}
	if got.Stats.TotalRequests != 150 || got.Stats.TotalCacheCreationTokens != 4000 || got.Stats.Rpm != 3 || got.Stats.Tpm != 4200 {
		t.Fatalf("unexpected stats: %+v", got.Stats)
	}
	if got.Stats.AverageDurationMs != 1250.5 {
		t.Fatalf("AverageDurationMs = %v, want 1250.5", got.Stats.AverageDurationMs)
	}
	if len(got.Trend) != 1 || got.Trend[0].CacheReadTokens != 700 {
		t.Fatalf("unexpected trend: %+v", got.Trend)
	}
	if len(got.Models) != 1 || got.Models[0].ActualCost != 0.60 {
		t.Fatalf("unexpected models: %+v", got.Models)
	}
	if got.Range.StartDate != "2026-06-01" || got.Range.EndDate != "2026-06-06" || got.Range.Granularity != "day" || got.Range.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected range: %+v", got.Range)
	}
}

func TestGetUserUsageDashboardFailsFastOnSub2APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "test-jwt-token"},
		})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"id": 1, "username": "alice", "email": "alice@example.com"},
		})
	})
	mux.HandleFunc("/api/v1/usage/dashboard/stats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 502, "message": "upstream failed"})
	})

	p := newTestProvider(t, mux)
	_, err := p.GetUserUsageDashboard(context.Background(), "alice@example.com", "test-password", relay.UserUsageDashboardParams{})
	if err == nil {
		t.Fatal("GetUserUsageDashboard() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "usage dashboard stats") {
		t.Fatalf("error = %v, want stats context", err)
	}
}
```

- [x] **Step 2: Run the new relay tests and verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && go test ./internal/relay -run 'TestGetUserUsageDashboard' -count=1
```

Expected: compile failure because `relay.Provider` does not yet define `GetUserUsageDashboard` and the old usage types still exist.

- [x] **Step 3: Replace relay usage types**

In `backend/internal/relay/types.go`, replace `UserUsageStats`, `UsageTrendParams`, `UsageTrendDataPoint`, `UsageTrendResponse`, `UsageModelParams`, `UsageModelStat`, and `UsageModelResponse` with:

```go
type UserUsageDashboardParams struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type UserUsageDashboardRange struct {
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Granularity string `json:"granularity"`
	Timezone    string `json:"timezone"`
}

type UserUsageDashboardResponse struct {
	Configured bool                      `json:"configured"`
	Range      UserUsageDashboardRange   `json:"range"`
	Stats      *UserUsageDashboardStats  `json:"stats"`
	Trend      []UserUsageTrendPoint     `json:"trend"`
	Models     []UserUsageModelStat      `json:"models"`
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
```

- [x] **Step 4: Replace Provider interface methods**

In `backend/internal/relay/provider.go`, replace:

```go
GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error)
GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error)
GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error)
```

with:

```go
GetUserUsageDashboard(ctx context.Context, login, password string, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
```

- [x] **Step 5: Implement the sub2api snapshot method**

In `backend/internal/relay/sub2api.go`, replace the three public `GetUserUsageStats`, `GetUserUsageTrend`, and `GetUserUsageModels` methods with:

```go
type userUsageTrendEnvelope struct {
	Trend       []UserUsageTrendPoint `json:"trend"`
	StartDate   string                `json:"start_date"`
	EndDate     string                `json:"end_date"`
	Granularity string                `json:"granularity"`
}

type userUsageModelsEnvelope struct {
	Models    []UserUsageModelStat `json:"models"`
	StartDate string               `json:"start_date"`
	EndDate   string               `json:"end_date"`
}

func (s *sub2apiRelay) GetUserUsageDashboard(ctx context.Context, login, password string, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage dashboard: %w", err)
	}

	stats, err := s.getUserUsageDashboardStats(ctx, token)
	if err != nil {
		return nil, err
	}
	trend, err := s.getUserUsageDashboardTrend(ctx, token, params)
	if err != nil {
		return nil, err
	}
	models, err := s.getUserUsageDashboardModels(ctx, token, params)
	if err != nil {
		return nil, err
	}

	return &UserUsageDashboardResponse{
		Configured: true,
		Range: UserUsageDashboardRange{
			StartDate:   firstNonEmpty(trend.StartDate, params.StartDate),
			EndDate:     firstNonEmpty(trend.EndDate, params.EndDate),
			Granularity: firstNonEmpty(trend.Granularity, params.Granularity, "day"),
			Timezone:    strings.TrimSpace(params.Timezone),
		},
		Stats:  stats,
		Trend:  trend.Trend,
		Models: models.Models,
	}, nil
}

func (s *sub2apiRelay) getUserUsageDashboardStats(ctx context.Context, token string) (*UserUsageDashboardStats, error) {
	var stats UserUsageDashboardStats
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/stats", nil, &stats); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard stats: %w", err)
	}
	return &stats, nil
}

func (s *sub2apiRelay) getUserUsageDashboardTrend(ctx context.Context, token string, params UserUsageDashboardParams) (*userUsageTrendEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, true)
	var trend userUsageTrendEnvelope
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/trend", query, &trend); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard trend: %w", err)
	}
	return &trend, nil
}

func (s *sub2apiRelay) getUserUsageDashboardModels(ctx context.Context, token string, params UserUsageDashboardParams) (*userUsageModelsEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, false)
	var models userUsageModelsEnvelope
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/models", query, &models); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard models: %w", err)
	}
	return &models, nil
}

func (s *sub2apiRelay) getUserDashboardJSON(ctx context.Context, token, path string, query url.Values, dst any) error {
	u, err := url.Parse(s.adminURL + path)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("request failed")
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("missing data")
	}
	if err := json.Unmarshal(result.Data, dst); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}

func addUserUsageDashboardQuery(query url.Values, params UserUsageDashboardParams, includeGranularity bool) {
	if v := strings.TrimSpace(params.StartDate); v != "" {
		query.Set("start_date", v)
	}
	if v := strings.TrimSpace(params.EndDate); v != "" {
		query.Set("end_date", v)
	}
	if includeGranularity {
		if v := strings.TrimSpace(params.Granularity); v != "" {
			query.Set("granularity", v)
		}
	}
	if v := strings.TrimSpace(params.Timezone); v != "" {
		query.Set("timezone", v)
	}
}
```

Use the existing package-local `firstNonEmpty` helper already present in `backend/internal/relay/sub2api.go`; do not add a duplicate helper.

- [x] **Step 6: Run relay tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && go test ./internal/relay -run 'TestGetUserUsageDashboard' -count=1
```

Expected: PASS.

- [x] **Step 7: Commit relay snapshot contract**

```bash
git add backend/internal/relay/types.go backend/internal/relay/provider.go backend/internal/relay/sub2api.go backend/internal/relay/sub2api_test.go
git commit -m "feat(relay): add user usage dashboard snapshot"
```

---

## Task 2: Backend Handler and Route

**Files:**
- Create: `backend/internal/handler/user_usage_test.go`
- Modify: `backend/internal/handler/user_usage.go`
- Modify: `backend/internal/handler/router.go`

- [x] **Step 1: Write handler tests**

Create `backend/internal/handler/user_usage_test.go`:

```go
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
```

- [x] **Step 2: Run handler tests and verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && go test ./internal/handler -run 'TestUserUsageDashboard' -count=1
```

Expected: compile failure because `NewUserUsageHandler` still takes `*ProviderHandler` and `Dashboard` does not exist.

- [x] **Step 3: Refactor handler dependency and implement Dashboard**

Replace `backend/internal/handler/user_usage.go` with the snapshot handler shape:

```go
package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type userUsageProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type UserUsageHandler struct {
	entClient        *ent.Client
	providerResolver userUsageProviderResolver
	encryptionKey    string
}

func NewUserUsageHandler(entClient *ent.Client, providerResolver userUsageProviderResolver, encryptionKey string) *UserUsageHandler {
	return &UserUsageHandler{
		entClient:        entClient,
		providerResolver: providerResolver,
		encryptionKey:    encryptionKey,
	}
}

func (h *UserUsageHandler) resolvePrimaryProvider(c *gin.Context) (relay.Provider, error) {
	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true)).
		All(c.Request.Context())
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return h.providerResolver.Resolve(c.Request.Context(), 1)
	}
	return h.providerResolver.Resolve(c.Request.Context(), providers[0].ID)
}

func (h *UserUsageHandler) Dashboard(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	params, ok := parseUserUsageDashboardParams(c)
	if !ok {
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), uc.UserID)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "fetch user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Success(c, &relay.UserUsageDashboardResponse{
			Configured: false,
			Range: relay.UserUsageDashboardRange{
				StartDate:   params.StartDate,
				EndDate:     params.EndDate,
				Granularity: params.Granularity,
				Timezone:    params.Timezone,
			},
			Trend:  []relay.UserUsageTrendPoint{},
			Models: []relay.UserUsageModelStat{},
		})
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "decrypt relay password: "+err.Error())
		return
	}

	relayProvider, err := h.resolvePrimaryProvider(c)
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "resolve relay provider: "+err.Error())
		return
	}

	login := firstNonEmptyString(u.Email, u.Username)
	if login == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "user has no email or username")
		return
	}

	snapshot, err := relayProvider.GetUserUsageDashboard(c.Request.Context(), login, password, params)
	if err != nil {
		if errors.Is(err, relay.ErrInvalidCredentials) {
			pkg.Error(c, http.StatusConflict, "Relay credentials need attention. Please update AI service configuration.")
			return
		}
		pkg.Error(c, http.StatusBadGateway, "get usage dashboard: "+err.Error())
		return
	}

	if snapshot == nil {
		pkg.Error(c, http.StatusBadGateway, "get usage dashboard: empty response")
		return
	}
	pkg.Success(c, snapshot)
}

func parseUserUsageDashboardParams(c *gin.Context) (relay.UserUsageDashboardParams, bool) {
	granularity := strings.TrimSpace(c.DefaultQuery("granularity", "day"))
	if granularity != "day" && granularity != "hour" {
		pkg.Error(c, http.StatusBadRequest, "granularity must be day or hour")
		return relay.UserUsageDashboardParams{}, false
	}

	params := relay.UserUsageDashboardParams{
		StartDate:   strings.TrimSpace(c.Query("start_date")),
		EndDate:     strings.TrimSpace(c.Query("end_date")),
		Granularity: granularity,
		Timezone:    strings.TrimSpace(c.Query("timezone")),
	}
	if params.StartDate == "" || params.EndDate == "" {
		start, end := defaultUserUsageRange(time.Now())
		if params.StartDate == "" {
			params.StartDate = start
		}
		if params.EndDate == "" {
			params.EndDate = end
		}
	}
	return params, true
}

func defaultUserUsageRange(now time.Time) (string, string) {
	today := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -6).Format("2006-01-02")
	return start, today
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
```

- [x] **Step 4: Update router registration**

In `backend/internal/handler/router.go`, replace:

```go
userUsageHandler := NewUserUsageHandler(entClient, providerHandler, encryptionKey)
userGroup.GET("/usage/stats", userUsageHandler.Stats)
userGroup.GET("/usage/trend", userUsageHandler.Trend)
userGroup.GET("/usage/models", userUsageHandler.Models)
```

with:

```go
userUsageHandler := NewUserUsageHandler(entClient, providerHandler, encryptionKey)
userGroup.GET("/usage/dashboard", userUsageHandler.Dashboard)
```

- [x] **Step 5: Run handler tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/handler -run 'TestUserUsageDashboard' -count=1
```

Expected: PASS.

- [x] **Step 6: Commit handler snapshot endpoint**

```bash
git add backend/internal/handler/user_usage.go backend/internal/handler/user_usage_test.go backend/internal/handler/router.go
git commit -m "feat(handler): expose user usage dashboard snapshot"
```

---

## Task 3: Backend Interface Cleanup Across Tests

**Files:**
- Modify: `backend/internal/auth/sso_test.go`
- Modify: `backend/internal/attribution/service_test.go`
- Modify: `backend/internal/usersetup/service_test.go`

- [x] **Step 1: Update fake relay providers**

In each fake or mock provider that currently implements the three old methods:

```go
func (f *fakeRelayProvider) GetUserUsageStats(ctx context.Context, login, password string) (*relay.UserUsageStats, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUserUsageTrend(ctx context.Context, login, password string, params relay.UsageTrendParams) (*relay.UsageTrendResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUserUsageModels(ctx context.Context, login, password string, params relay.UsageModelParams) (*relay.UsageModelResponse, error) {
	return nil, nil
}
```

replace them with:

```go
func (f *fakeRelayProvider) GetUserUsageDashboard(ctx context.Context, login, password string, params relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	return nil, nil
}
```

For `backend/internal/auth/sso_test.go`, the receiver is `m *mockRelayProvider`, so use:

```go
func (m *mockRelayProvider) GetUserUsageDashboard(_ context.Context, _, _ string, _ relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	return nil, nil
}
```

- [x] **Step 2: Run backend package tests that compile Provider fakes**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./internal/auth ./internal/attribution ./internal/usersetup -count=1
```

Expected: PASS.

- [x] **Step 3: Commit test fake cleanup**

```bash
git add backend/internal/auth/sso_test.go backend/internal/attribution/service_test.go backend/internal/usersetup/service_test.go
git commit -m "test(relay): update usage dashboard provider mocks"
```

---

## Task 4: Frontend Types, API, and Chart Dependencies

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/userUsage.ts`
- Create: `frontend/src/__tests__/user-usage-api.test.ts`

- [x] **Step 1: Add chart dependencies**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm add chart.js vue-chartjs
cd /Users/admin/ai-efficiency/frontend && npm install --package-lock-only --ignore-scripts
```

Expected: `frontend/package.json` contains `chart.js` and `vue-chartjs`, and `frontend/package-lock.json` is updated.

- [x] **Step 2: Write API test**

Create `frontend/src/__tests__/user-usage-api.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
  },
}))

import client from '@/api/client'
import { getUserUsageDashboard } from '@/api/userUsage'

const mockClient = client as unknown as {
  get: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('user usage API', () => {
  it('calls the dashboard snapshot endpoint with params', async () => {
    mockClient.get.mockResolvedValue({ data: { data: { configured: false, trend: [], models: [] } } })
    await getUserUsageDashboard({
      start_date: '2026-06-01',
      end_date: '2026-06-06',
      granularity: 'day',
      timezone: 'Asia/Shanghai',
    })
    expect(mockClient.get).toHaveBeenCalledWith('/user/usage/dashboard', {
      params: {
        start_date: '2026-06-01',
        end_date: '2026-06-06',
        granularity: 'day',
        timezone: 'Asia/Shanghai',
      },
    })
  })
})
```

- [x] **Step 3: Run API test and verify it fails**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm test -- src/__tests__/user-usage-api.test.ts
```

Expected: FAIL because `getUserUsageDashboard` is not exported yet.

- [x] **Step 4: Add frontend types**

In `frontend/src/types/index.ts`, replace the current user usage types with:

```ts
export interface UserUsageDashboardParams {
  start_date?: string
  end_date?: string
  granularity?: 'day' | 'hour'
  timezone?: string
}

export interface UserUsageDashboardRange {
  start_date: string
  end_date: string
  granularity: 'day' | 'hour' | string
  timezone?: string
}

export interface UserUsageDashboardStats {
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_tokens: number
  total_cost: number
  total_actual_cost: number
  today_requests: number
  today_input_tokens: number
  today_output_tokens: number
  today_cache_creation_tokens: number
  today_cache_read_tokens: number
  today_tokens: number
  today_cost: number
  today_actual_cost: number
  average_duration_ms: number
  rpm: number
  tpm: number
}

export interface UserUsageTrendPoint {
  date: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageModelStat {
  model: string
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  cost: number
  actual_cost: number
}

export interface UserUsageDashboardSnapshot {
  configured: boolean
  range: UserUsageDashboardRange
  stats: UserUsageDashboardStats | null
  trend: UserUsageTrendPoint[]
  models: UserUsageModelStat[]
}
```

- [x] **Step 5: Replace API module**

Replace `frontend/src/api/userUsage.ts` with:

```ts
import client from './client'
import type {
  ApiResponse,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
} from '@/types'

export function getUserUsageDashboard(params: UserUsageDashboardParams) {
  return client.get<ApiResponse<UserUsageDashboardSnapshot>>('/user/usage/dashboard', { params })
}
```

- [x] **Step 6: Run API test and verify it passes**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm test -- src/__tests__/user-usage-api.test.ts
```

Expected: PASS.

- [x] **Step 7: Commit frontend contract and dependencies**

```bash
git add frontend/package.json frontend/package-lock.json frontend/src/types/index.ts frontend/src/api/userUsage.ts frontend/src/__tests__/user-usage-api.test.ts
git commit -m "feat(frontend): add user usage dashboard snapshot API"
```

---

## Task 5: Frontend Usage Components

**Files:**
- Modify: `frontend/src/components/user/usage/UsageStatsCards.vue`
- Modify: `frontend/src/components/user/usage/UsageTrendChart.vue`
- Modify: `frontend/src/components/user/usage/UsageModelChart.vue`

- [x] **Step 1: Rewrite stats cards**

Replace `frontend/src/components/user/usage/UsageStatsCards.vue` with a component that accepts `stats: UserUsageDashboardStats | null` and renders four cards:

```vue
<script setup lang="ts">
import type { UserUsageDashboardStats } from '@/types'

defineProps<{
  stats: UserUsageDashboardStats | null
}>()

function formatNumber(n: number): string {
  return n.toLocaleString()
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

function formatCost(n: number): string {
  return n.toFixed(4)
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">Today Cost</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">${{ formatCost(stats?.today_actual_cost ?? 0) }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Standard: ${{ formatCost(stats?.today_cost ?? 0) }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">Today Requests</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ formatNumber(stats?.today_requests ?? 0) }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">Total: {{ formatNumber(stats?.total_requests ?? 0) }}</p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">Today Tokens</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ formatTokens(stats?.today_tokens ?? 0) }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        In {{ formatTokens(stats?.today_input_tokens ?? 0) }} · Out {{ formatTokens(stats?.today_output_tokens ?? 0) }} · Cache {{ formatTokens((stats?.today_cache_creation_tokens ?? 0) + (stats?.today_cache_read_tokens ?? 0)) }}
      </p>
    </section>

    <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
      <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">Avg Response</p>
      <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ formatDuration(stats?.average_duration_ms ?? 0) }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">RPM {{ formatTokens(stats?.rpm ?? 0) }} · TPM {{ formatTokens(stats?.tpm ?? 0) }}</p>
    </section>
  </div>
</template>
```

- [x] **Step 2: Rewrite trend chart**

Replace `frontend/src/components/user/usage/UsageTrendChart.vue` with:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import { Line } from 'vue-chartjs'
import type { UserUsageTrendPoint } from '@/types'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  data: UserUsageTrendPoint[]
  loading: boolean
}>()

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}

const chartData = computed(() => ({
  labels: props.data.map((point) => point.date),
  datasets: [
    { label: 'Input', data: props.data.map((point) => point.input_tokens), borderColor: '#2563eb', backgroundColor: '#2563eb22', fill: true, tension: 0.3 },
    { label: 'Output', data: props.data.map((point) => point.output_tokens), borderColor: '#16a34a', backgroundColor: '#16a34a22', fill: true, tension: 0.3 },
    { label: 'Cache Creation', data: props.data.map((point) => point.cache_creation_tokens), borderColor: '#d97706', backgroundColor: '#d9770622', fill: true, tension: 0.3 },
    { label: 'Cache Read', data: props.data.map((point) => point.cache_read_tokens), borderColor: '#0891b2', backgroundColor: '#0891b222', fill: true, tension: 0.3 },
  ],
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' as const },
  plugins: {
    legend: { position: 'top' as const },
    tooltip: {
      callbacks: {
        label: (context: any) => `${context.dataset.label}: ${formatTokens(Number(context.raw ?? 0))}`,
        footer: (items: any[]) => {
          const index = items[0]?.dataIndex
          const point = props.data[index]
          return point ? `Actual: $${point.actual_cost.toFixed(4)} | Standard: $${point.cost.toFixed(4)}` : ''
        },
      },
    },
  },
  scales: {
    y: {
      ticks: {
        callback: (value: string | number) => formatTokens(Number(value)),
      },
    },
  },
}))
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
    <div class="mb-4 flex items-center justify-between">
      <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">Token Trend</h2>
    </div>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">Loading trend...</div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">No trend data available</div>
    <div v-else class="h-72">
      <Line :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>
```

- [x] **Step 3: Rewrite model chart**

Replace `frontend/src/components/user/usage/UsageModelChart.vue` with:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import type { UserUsageModelStat } from '@/types'

ChartJS.register(ArcElement, Tooltip, Legend)

const props = defineProps<{
  data: UserUsageModelStat[]
  loading: boolean
}>()

const colors = ['#2563eb', '#16a34a', '#d97706', '#dc2626', '#7c3aed', '#db2777', '#0891b2', '#65a30d']

const chartData = computed(() => ({
  labels: props.data.map((model) => model.model),
  datasets: [{ data: props.data.map((model) => model.total_tokens), backgroundColor: colors }],
}))

const chartOptions = { responsive: true, maintainAspectRatio: false, plugins: { legend: { display: false } } }

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toLocaleString()
}
</script>

<template>
  <section class="rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-800">
    <h2 class="mb-4 text-base font-semibold text-gray-900 dark:text-gray-100">Model Distribution</h2>
    <div v-if="loading" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">Loading models...</div>
    <div v-else-if="data.length === 0" class="flex h-72 items-center justify-center text-sm text-gray-500 dark:text-gray-400">No model data available</div>
    <div v-else class="grid gap-4 lg:grid-cols-[180px_1fr]">
      <div class="h-44">
        <Doughnut :data="chartData" :options="chartOptions" />
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-gray-700 dark:text-gray-400">
              <th class="pb-2">Model</th>
              <th class="pb-2 text-right">Requests</th>
              <th class="pb-2 text-right">Tokens</th>
              <th class="pb-2 text-right">Actual</th>
              <th class="pb-2 text-right">Standard</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in data" :key="model.model" class="border-b border-gray-100 last:border-0 dark:border-gray-700">
              <td class="max-w-[12rem] truncate py-2 font-medium text-gray-900 dark:text-gray-100" :title="model.model">{{ model.model }}</td>
              <td class="py-2 text-right text-gray-600 dark:text-gray-300">{{ model.requests.toLocaleString() }}</td>
              <td class="py-2 text-right text-gray-600 dark:text-gray-300">{{ formatTokens(model.total_tokens) }}</td>
              <td class="py-2 text-right text-green-600 dark:text-green-400">${{ model.actual_cost.toFixed(4) }}</td>
              <td class="py-2 text-right text-gray-500 dark:text-gray-400">${{ model.cost.toFixed(4) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
```

- [x] **Step 4: Run typecheck for components**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm exec vue-tsc -b
```

Expected: PASS.

- [x] **Step 5: Commit usage components**

```bash
git add frontend/src/components/user/usage/UsageStatsCards.vue frontend/src/components/user/usage/UsageTrendChart.vue frontend/src/components/user/usage/UsageModelChart.vue
git commit -m "feat(frontend): render user usage dashboard components"
```

---

## Task 6: Frontend Usage View

**Files:**
- Create: `frontend/src/__tests__/user-usage-view.test.ts`
- Modify: `frontend/src/views/user/UsageView.vue`

- [x] **Step 1: Write UsageView tests**

Create `frontend/src/__tests__/user-usage-view.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import UsageView from '@/views/user/UsageView.vue'

vi.mock('@/api/userUsage', () => ({
  getUserUsageDashboard: vi.fn(),
}))

vi.mock('vue-chartjs', () => ({
  Line: { template: '<div data-test="line-chart" />' },
  Doughnut: { template: '<div data-test="doughnut-chart" />' },
}))

function createRouterForUsage() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/user/usage', component: UsageView },
      { path: '/user', component: { template: '<div>Setup</div>' } },
    ],
  })
}

const snapshot = {
  configured: true,
  range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day', timezone: 'Asia/Shanghai' },
  stats: {
    total_requests: 100,
    total_input_tokens: 10000,
    total_output_tokens: 5000,
    total_cache_creation_tokens: 200,
    total_cache_read_tokens: 300,
    total_tokens: 15500,
    total_cost: 2.5,
    total_actual_cost: 2,
    today_requests: 12,
    today_input_tokens: 1000,
    today_output_tokens: 500,
    today_cache_creation_tokens: 20,
    today_cache_read_tokens: 30,
    today_tokens: 1550,
    today_cost: 0.25,
    today_actual_cost: 0.2,
    average_duration_ms: 850,
    rpm: 2,
    tpm: 3000,
  },
  trend: [{ date: '2026-06-06', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
  models: [{ model: 'example-model', requests: 12, input_tokens: 1000, output_tokens: 500, cache_creation_tokens: 20, cache_read_tokens: 30, total_tokens: 1550, cost: 0.25, actual_cost: 0.2 }],
}

describe('UsageView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('shows setup empty state when dashboard is not configured', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({
      data: { data: { configured: false, range: { start_date: '2026-06-01', end_date: '2026-06-06', granularity: 'day' }, stats: null, trend: [], models: [] } },
    })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Complete AI service configuration')
    expect(wrapper.text()).toContain('Open My Setup')
  })

  it('renders snapshot cards and charts', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('My AI Usage')
    expect(wrapper.text()).toContain('Today Cost')
    expect(wrapper.text()).toContain('Today Requests')
    expect(wrapper.text()).toContain('Today Tokens')
    expect(wrapper.text()).toContain('Avg Response')
    expect(wrapper.text()).toContain('example-model')
    expect(wrapper.find('[data-test="line-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="doughnut-chart"]').exists()).toBe(true)
  })

  it('uses hour granularity for Today and day granularity for 7 Days', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockResolvedValue({ data: { data: snapshot } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    await wrapper.get('[data-test="range-today"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0].granularity).toBe('hour')
    await wrapper.get('[data-test="range-7d"]').trigger('click')
    await flushPromises()
    expect((getUserUsageDashboard as any).mock.calls.at(-1)[0].granularity).toBe('day')
  })

  it('shows credential repair message on 409', async () => {
    const { getUserUsageDashboard } = await import('@/api/userUsage')
    ;(getUserUsageDashboard as any).mockRejectedValue({ response: { status: 409 } })
    const router = createRouterForUsage()
    await router.push('/user/usage')
    await router.isReady()
    const wrapper = mount(UsageView, { global: { plugins: [createPinia(), router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Relay credentials need attention')
    expect(wrapper.text()).toContain('Open My Setup')
  })
})
```

- [x] **Step 2: Run UsageView tests and verify they fail**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm test -- src/__tests__/user-usage-view.test.ts
```

Expected: FAIL because `UsageView.vue` still imports the three old API functions and lacks the new test ids and states.

- [x] **Step 3: Rewrite UsageView**

Replace `frontend/src/views/user/UsageView.vue` with:

```vue
<template>
  <div class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
    <div class="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">My AI Usage</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">Usage and cost from your configured AI relay account.</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button data-test="range-today" type="button" :class="rangeButtonClass(selectedRange === 'today')" @click="selectRange('today')">Today</button>
        <button data-test="range-7d" type="button" :class="rangeButtonClass(selectedRange === '7d')" @click="selectRange('7d')">7 Days</button>
        <button data-test="range-30d" type="button" :class="rangeButtonClass(selectedRange === '30d')" @click="selectRange('30d')">30 Days</button>
        <button type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800" :disabled="loading" @click="loadDashboard">Refresh</button>
      </div>
    </div>

    <div v-if="loading && !snapshot" class="flex min-h-80 items-center justify-center text-sm text-gray-500 dark:text-gray-400">Loading usage dashboard...</div>

    <div v-else-if="setupRequired" class="rounded-lg border border-amber-200 bg-amber-50 p-6 dark:border-amber-800 dark:bg-amber-950/30">
      <h2 class="text-base font-semibold text-amber-900 dark:text-amber-100">Complete AI service configuration</h2>
      <p class="mt-2 text-sm text-amber-800 dark:text-amber-200">Usage data is available after your relay credentials are configured.</p>
      <router-link to="/user" class="mt-4 inline-flex rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700">Open My Setup</router-link>
    </div>

    <div v-else-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-6 dark:border-red-800 dark:bg-red-950/30">
      <h2 class="text-base font-semibold text-red-900 dark:text-red-100">{{ errorMessage }}</h2>
      <p class="mt-2 text-sm text-red-800 dark:text-red-200">Try refreshing after checking your setup.</p>
      <router-link v-if="credentialError" to="/user" class="mt-4 inline-flex rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">Open My Setup</router-link>
    </div>

    <div v-else class="space-y-6">
      <UsageStatsCards :stats="snapshot?.stats ?? null" />
      <div class="grid grid-cols-1 gap-6 xl:grid-cols-[1.35fr_1fr]">
        <UsageTrendChart :data="snapshot?.trend ?? []" :loading="loading" />
        <UsageModelChart :data="snapshot?.models ?? []" :loading="loading" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getUserUsageDashboard } from '@/api/userUsage'
import type { UserUsageDashboardParams, UserUsageDashboardSnapshot } from '@/types'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'

type RangeOption = 'today' | '7d' | '30d'

const selectedRange = ref<RangeOption>('7d')
const snapshot = ref<UserUsageDashboardSnapshot | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const credentialError = ref(false)

const setupRequired = computed(() => snapshot.value?.configured === false)

function rangeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-blue-600 text-white'
      : 'border border-gray-300 text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800',
  ]
}

function formatDate(date: Date): string {
  return date.toISOString().slice(0, 10)
}

function buildParams(range: RangeOption): UserUsageDashboardParams {
  const end = new Date()
  const start = new Date(end)
  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }
  }
  if (range === '7d') {
    start.setDate(end.getDate() - 6)
  } else {
    start.setDate(end.getDate() - 29)
  }
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''
  credentialError.value = false
  try {
    const res = await getUserUsageDashboard(buildParams(selectedRange.value))
    snapshot.value = res.data.data ?? null
  } catch (err: any) {
    snapshot.value = null
    credentialError.value = err?.response?.status === 409
    errorMessage.value = credentialError.value ? 'Relay credentials need attention' : 'Usage dashboard is temporarily unavailable'
  } finally {
    loading.value = false
  }
}

function selectRange(range: RangeOption) {
  selectedRange.value = range
  loadDashboard()
}

onMounted(() => {
  loadDashboard()
})
</script>
```

- [x] **Step 4: Run UsageView tests and verify they pass**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm test -- src/__tests__/user-usage-view.test.ts
```

Expected: PASS.

- [x] **Step 5: Commit UsageView rewrite**

```bash
git add frontend/src/views/user/UsageView.vue frontend/src/__tests__/user-usage-view.test.ts
git commit -m "feat(frontend): load user usage dashboard snapshot"
```

---

## Task 7: Full Verification and Documentation Check

**Files:**
- Verify: `docs/architecture.md`
- Verify: `docs/superpowers/specs/2026-06-06-user-usage-trend-design.md`
- Verify: `docs/superpowers/plans/2026-06-06-user-usage-trend.md`

- [x] **Step 1: Search for stale old endpoint references**

Run:

```bash
cd /Users/admin/ai-efficiency && rg -n "/user/usage/(stats|trend|models)|GetUserUsageStats|GetUserUsageTrend|GetUserUsageModels|UsageTrendParams|UsageModelParams|UserUsageStats" backend frontend docs
```

Expected: no matches, except historical context in committed diffs outside the current worktree is not searched by this command.

- [x] **Step 2: Run backend tests**

Run:

```bash
cd /Users/admin/ai-efficiency/backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./...
```

Expected: PASS.

- [x] **Step 3: Run frontend tests**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm test
```

Expected: PASS.

- [x] **Step 4: Run frontend build**

Run:

```bash
cd /Users/admin/ai-efficiency/frontend && pnpm run build
```

Expected: PASS.

- [x] **Step 5: Check architecture docs for required update**

Run:

```bash
cd /Users/admin/ai-efficiency && rg -n "user usage|/user/usage|usage dashboard|relay dashboard|dashboard snapshot" docs/architecture.md
```

Expected: if there are no matches, no architecture doc change is required. If the command finds text describing the old three-endpoint usage page, update that text to say the current user usage page uses `GET /api/v1/user/usage/dashboard` as a single AE snapshot endpoint backed by sub2api user dashboard APIs.

- [x] **Step 6: Commit final verification cleanup**

If Step 5 required a docs change:

```bash
git add docs/architecture.md
git commit -m "docs(architecture): document user usage dashboard snapshot"
```

If Step 5 did not require a docs change, skip this commit and record the no-op in the final implementation summary.

- [x] **Step 7: Final status audit**

Run:

```bash
cd /Users/admin/ai-efficiency && git status --short --branch
```

Expected: only unrelated pre-existing files such as `.qoder/` remain untracked. All implementation changes are committed.
