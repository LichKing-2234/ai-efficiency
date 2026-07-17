package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type fakeTeamUsageService struct {
	scopeFn            func(context.Context, int) (*teamusage.ScopeResponse, error)
	subjectsFn         func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error)
	subjectDashboardFn func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error)
	summaryFn          func(context.Context, int, teamusage.OverviewParams) (*teamusage.SummaryResponse, error)
	trendFn            func(context.Context, int, teamusage.OverviewParams) (*teamusage.TrendResponse, error)
	membersFn          func(context.Context, int, teamusage.MembersParams) (*teamusage.MembersResponse, error)
	organizationFn     func(context.Context, int, teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error)
	overviewFn         func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error)
	updateMultiplierFn func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error)
	listAuditFn        func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error)
	listAdminAuditFn   func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error)
}

func (f *fakeTeamUsageService) Scope(ctx context.Context, actorUserID int) (*teamusage.ScopeResponse, error) {
	return f.scopeFn(ctx, actorUserID)
}

func (f *fakeTeamUsageService) Subjects(ctx context.Context, actorUserID int, q string, page, pageSize int) (*teamusage.SubjectsResponse, error) {
	return f.subjectsFn(ctx, actorUserID, q, page, pageSize)
}

func (f *fakeTeamUsageService) SubjectDashboard(ctx context.Context, actorUserID, targetUserID int, params relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
	return f.subjectDashboardFn(ctx, actorUserID, targetUserID, params)
}

func (f *fakeTeamUsageService) Summary(ctx context.Context, actorUserID int, params teamusage.OverviewParams) (*teamusage.SummaryResponse, error) {
	return f.summaryFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) Trend(ctx context.Context, actorUserID int, params teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
	return f.trendFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) Members(ctx context.Context, actorUserID int, params teamusage.MembersParams) (*teamusage.MembersResponse, error) {
	return f.membersFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) Organization(ctx context.Context, actorUserID int, params teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
	return f.organizationFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) Overview(ctx context.Context, actorUserID int, params teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
	return f.overviewFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) UpdateMultiplier(ctx context.Context, actorUserID, targetUserID int, groupID int64, req teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
	return f.updateMultiplierFn(ctx, actorUserID, targetUserID, groupID, req)
}

func (f *fakeTeamUsageService) ListAudit(ctx context.Context, actorUserID int, params teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
	return f.listAuditFn(ctx, actorUserID, params)
}

func (f *fakeTeamUsageService) ListAdminAudit(ctx context.Context, params teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
	return f.listAdminAuditFn(ctx, params)
}

type teamUsageTestEnv struct {
	router     *gin.Engine
	authSvc    *auth.Service
	token      string
	adminToken string
	userID     int
	adminID    int
}

func newTeamUsageTestRouter(t *testing.T, service *fakeTeamUsageService) *teamUsageTestEnv {
	t.Helper()

	client := testdb.Open(t)
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)

	user, err := client.User.Create().
		SetUsername("member").
		SetEmail("member@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	admin, err := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleAdmin).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	userPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: user.ID, Username: user.Username, Role: string(user.Role)})
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	adminPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: admin.ID, Username: admin.Username, Role: string(admin.Role)})
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestTelemetry(logger, "test-release"))
	userGroup := router.Group("/api/v1/user")
	userGroup.Use(authpkg.RequireAuth(authSvc))
	teamHandler := NewTeamUsageHandler(service)
	userGroup.GET("/team-usage/scope", teamHandler.Scope)
	userGroup.GET("/team-usage/subjects", teamHandler.Subjects)
	userGroup.GET("/team-usage/subjects/:user_id/usage/dashboard", teamHandler.SubjectDashboard)
	userGroup.PUT("/team-usage/subjects/:user_id/groups/:group_id/rate-multiplier", teamHandler.UpdateMultiplier)
	userGroup.GET("/team-usage/summary", teamHandler.Summary)
	userGroup.GET("/team-usage/trend", teamHandler.Trend)
	userGroup.GET("/team-usage/members", teamHandler.Members)
	userGroup.GET("/team-usage/organization", teamHandler.Organization)
	userGroup.GET("/team-usage/overview", teamHandler.Overview)
	userGroup.GET("/team-usage/audit", teamHandler.Audit)

	adminGroup := router.Group("/api/v1/admin/team-usage")
	adminGroup.Use(authpkg.RequireAuth(authSvc), authpkg.RequireAdmin())
	adminGroup.GET("/audit", teamHandler.AdminAudit)

	return &teamUsageTestEnv{
		router:     router,
		authSvc:    authSvc,
		token:      userPair.AccessToken,
		adminToken: adminPair.AccessToken,
		userID:     user.ID,
		adminID:    admin.ID,
	}
}

func TestTeamUsageSplitEndpointsPreserveMiddlewareRequestID(t *testing.T) {
	const requestID = "client_Request-161.valid"
	assertContextRequestID := func(ctx context.Context) {
		t.Helper()
		if got := telemetry.RequestID(ctx); got != requestID {
			t.Fatalf("service context request ID = %q, want %q", got, requestID)
		}
	}

	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		summaryFn: func(ctx context.Context, _ int, _ teamusage.OverviewParams) (*teamusage.SummaryResponse, error) {
			assertContextRequestID(ctx)
			return &teamusage.SummaryResponse{}, nil
		},
		trendFn: func(ctx context.Context, _ int, _ teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
			assertContextRequestID(ctx)
			return &teamusage.TrendResponse{}, nil
		},
		membersFn: func(ctx context.Context, _ int, _ teamusage.MembersParams) (*teamusage.MembersResponse, error) {
			assertContextRequestID(ctx)
			return &teamusage.MembersResponse{}, nil
		},
		organizationFn: func(ctx context.Context, _ int, _ teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
			assertContextRequestID(ctx)
			return &teamusage.OrganizationResponse{}, nil
		},
	})

	for _, path := range []string{
		"/api/v1/user/team-usage/summary",
		"/api/v1/user/team-usage/trend",
		"/api/v1/user/team-usage/members",
		"/api/v1/user/team-usage/organization",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer "+env.token)
			request.Header.Set(telemetry.HeaderRequestID, requestID)
			response := httptest.NewRecorder()
			env.router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
			}
			if got := response.Header().Get(telemetry.HeaderRequestID); got != requestID {
				t.Fatalf("response request ID = %q, want %q", got, requestID)
			}
			if expected := `"request_id":"` + requestID + `"`; !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("response body = %s, want %s", response.Body.String(), expected)
			}
		})
	}
}

func TestTeamUsageSummaryReturnsFreshnessScopeAndUniqueRequestID(t *testing.T) {
	asOf := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		summaryFn: func(_ context.Context, actorID int, params teamusage.OverviewParams) (*teamusage.SummaryResponse, error) {
			if actorID != env.userID || params.StartDate != "2026-07-01" || params.EndDate != "2026-07-07" || params.Granularity != "day" || params.Timezone != "Asia/Shanghai" {
				t.Fatalf("unexpected summary request: actor=%d params=%+v", actorID, params)
			}
			rangeCost := 12.5
			return &teamusage.SummaryResponse{
				SnapshotFreshness: teamusage.SnapshotFreshness{
					AsOf: asOf, FreshUntil: asOf.Add(54 * time.Second), StaleUntil: asOf.Add(4*time.Minute + 30*time.Second),
					CacheStatus: "miss", SourceStatus: "ok",
				},
				ScopeVersion: "scope-version-1",
				Window: teamusage.OverviewWindow{
					StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai",
				},
				Summary: teamusage.OverviewSummary{MemberCount: 2, RangeActualCost: &rangeCost, UnitLabel: "USD"},
			}, nil
		},
	})

	path := "/api/v1/user/team-usage/summary?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=Asia%2FShanghai"
	first := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	second := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("summary responses = %d/%d, want 200", first.Code, second.Code)
	}
	firstRequestID := first.Header().Get("X-Request-ID")
	secondRequestID := second.Header().Get("X-Request-ID")
	if firstRequestID == "" || secondRequestID == "" || firstRequestID == secondRequestID {
		t.Fatalf("request IDs = %q/%q, want unique non-empty IDs", firstRequestID, secondRequestID)
	}
	for _, expected := range []string{
		`"scope_version":"scope-version-1"`, `"cache_status":"miss"`, `"source_status":"ok"`,
		`"request_id":"` + firstRequestID + `"`, `"range_actual_cost":12.5`,
	} {
		if !strings.Contains(first.Body.String(), expected) {
			t.Fatalf("summary body = %s, want %s", first.Body.String(), expected)
		}
	}
}

func TestTeamUsageSummaryMapsScopedAndInputFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "no representative scope", err: &teamusage.ForbiddenError{Reason: teamusage.ErrNotRepresentative.Error()}, status: http.StatusForbidden},
		{name: "invalid window", err: fmt.Errorf("%w: end date precedes start date", teamusage.ErrInvalidOverviewParams), status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				summaryFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.SummaryResponse, error) {
					return nil, tt.err
				},
			})
			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/summary?start_date=2026-07-08&end_date=2026-07-07&granularity=day&timezone=UTC", env.token, "")
			if rec.Code != tt.status {
				t.Fatalf("response = %d %s, want %d", rec.Code, rec.Body.String(), tt.status)
			}
		})
	}
}

func TestTeamUsageSummaryRequiresAuthentication(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		summaryFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.SummaryResponse, error) {
			t.Fatal("summary service must not run without authentication")
			return nil, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/summary", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("response = %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageSummaryIndependentFromTrendOverRealHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := testdb.Open(t)
	client.RelayProvider.Create().
		SetName("summary-http-relay").
		SetDisplayName("Summary HTTP Relay").
		SetBaseURL("https://relay.example.com").
		SetRelayType("sub2api").
		SetAdminAPIKey("test-admin-api-key").
		SetDefaultModel("test-model").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(context.Background())

	rangeAlice, rangeBob := 15.0, 30.0
	tokensAlice, tokensBob := int64(1500), int64(4500)
	releaseTrend := make(chan struct{})
	defer close(releaseTrend)
	provider := &teamUsageHTTPRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {
				UserID: 1002, RangeActualCost: &rangeAlice, RangeTotalTokens: &tokensAlice,
				TodayActualCost: 1, TotalActualCost: 10,
			},
			1003: {
				UserID: 1003, RangeActualCost: &rangeBob, RangeTotalTokens: &tokensBob,
				TodayActualCost: 2, TotalActualCost: 99,
			},
		},
		trendStarted: make(chan struct{}, 1),
		trendRelease: releaseTrend,
	}
	scope := &representativescope.Scope{
		Version: "scope-http-summary", ActorUserID: 101, IsRepresentative: true,
		OverviewSubjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 102, DisplayName: "Alice", RelayUserID: handlerIntPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 103, DisplayName: "Bob", RelayUserID: handlerIntPtr(1003), Selectable: true},
		},
	}
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close test Redis client: %v", err)
		}
	})
	cache, err := teamusage.NewSnapshotCache(
		readcache.NewRedisStore(redisClient),
		teamusage.SnapshotCacheOptions{Namespace: "summary-http-independent"},
	)
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	service, err := teamusage.NewService(
		client,
		teamUsageHTTPScopeResolver{scope: scope},
		teamUsageHTTPProviderResolver{provider: provider},
		nil,
		teamusage.ServiceOptions{SnapshotCache: cache, CursorSecret: "test-summary-http-cursor-secret"},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	router := gin.New()
	router.Use(withAuthUser(101, "user"))
	router.GET("/api/v1/user/team-usage/summary", NewTeamUsageHandler(service).Summary)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/user/team-usage/summary?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=Asia%2FShanghai",
		nil,
	)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(response, request)
		close(done)
	}()

	select {
	case <-done:
	case <-provider.trendStarted:
		t.Fatal("summary HTTP request reached trend provider")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("summary HTTP request did not complete independently of trend provider")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want 200", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"member_count":2`, `"relay_member_count":2`, `"range_actual_cost":45`, `"range_total_tokens":6000`,
		`"today_actual_cost":3`, `"total_actual_cost":109`, `"unit_label":"USD"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("summary body = %s, want %s", response.Body.String(), expected)
		}
	}
	if provider.trendCalls.Load() != 0 {
		t.Fatalf("trend calls = %d, want 0", provider.trendCalls.Load())
	}

	incompleteBob := provider.summaryStats[1003]
	incompleteBob.RangeTotalTokens = nil
	provider.summaryStats[1003] = incompleteBob
	incompleteRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/user/team-usage/summary?start_date=2026-07-01&end_date=2026-07-08&granularity=day&timezone=Asia%2FShanghai",
		nil,
	)
	incompleteResponse := httptest.NewRecorder()
	router.ServeHTTP(incompleteResponse, incompleteRequest)
	if incompleteResponse.Code != http.StatusOK {
		t.Fatalf("incomplete range status = %d body = %s, want 200", incompleteResponse.Code, incompleteResponse.Body.String())
	}
	for _, expected := range []string{
		`"unavailable":true`, `"unavailable_reason":"range_aggregation_unavailable"`,
		`"range_actual_cost":null`, `"range_total_tokens":null`,
		`"member_count":2`, `"relay_member_count":2`, `"today_actual_cost":3`, `"total_actual_cost":109`,
	} {
		if !strings.Contains(incompleteResponse.Body.String(), expected) {
			t.Fatalf("incomplete range body = %s, want %s", incompleteResponse.Body.String(), expected)
		}
	}
}

type teamUsageHTTPScopeResolver struct {
	scope *representativescope.Scope
}

func (r teamUsageHTTPScopeResolver) Resolve(context.Context, int) (*representativescope.Scope, error) {
	return r.scope, nil
}

type teamUsageHTTPProviderResolver struct {
	provider relay.Provider
}

func (r teamUsageHTTPProviderResolver) Resolve(context.Context, int) (relay.Provider, error) {
	return r.provider, nil
}

type teamUsageHTTPRelayProvider struct {
	relay.Provider
	summaryStats map[int64]relay.TeamUserUsageStats
	trendCalls   atomic.Int32
	trendStarted chan struct{}
	trendRelease <-chan struct{}
}

func (p *teamUsageHTTPRelayProvider) GetBatchUserUsageStats(context.Context, []int64, relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	return p.summaryStats, nil
}

func (p *teamUsageHTTPRelayProvider) GetUsageTrendForUsers(ctx context.Context, _ []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	p.trendCalls.Add(1)
	select {
	case p.trendStarted <- struct{}{}:
	default:
	}
	select {
	case <-p.trendRelease:
		return map[int64][]relay.UsageTrendPoint{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func handlerIntPtr(value int) *int {
	return &value
}

func TestTeamUsageTrendReturnsBoundedFreshnessScopeAndUniqueRequestID(t *testing.T) {
	asOf := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		trendFn: func(_ context.Context, actorID int, params teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
			if actorID != env.userID || params.StartDate != "2026-07-01" || params.EndDate != "2026-07-07" || params.Granularity != "hour" || params.Timezone != "Asia/Shanghai" {
				t.Fatalf("unexpected trend request: actor=%d params=%+v", actorID, params)
			}
			return &teamusage.TrendResponse{
				SnapshotFreshness: teamusage.SnapshotFreshness{
					AsOf: asOf, FreshUntil: asOf.Add(54 * time.Second), StaleUntil: asOf.Add(4*time.Minute + 30*time.Second),
					CacheStatus: "hit", SourceStatus: "ok",
				},
				ScopeVersion: "scope-version-trend-1",
				Window: teamusage.OverviewWindow{
					StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "hour", Timezone: "Asia/Shanghai",
				},
				TopMembers: []teamusage.OverviewMember{{UserID: 101, DisplayName: "Alice", Rank: 1}},
				DepartmentTrend: teamusage.DepartmentTrendState{
					ComparisonTotalCount: 14,
					ComparisonTruncated:  true,
					Series: []teamusage.DepartmentTrendSeries{{
						SeriesType: "team_total", DisplayName: "Team total",
					}},
				},
			}, nil
		},
	})

	path := "/api/v1/user/team-usage/trend?start_date=2026-07-01&end_date=2026-07-07&granularity=hour&timezone=Asia%2FShanghai"
	first := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	second := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("trend responses = %d/%d, want 200", first.Code, second.Code)
	}
	firstRequestID := first.Header().Get("X-Request-ID")
	secondRequestID := second.Header().Get("X-Request-ID")
	if firstRequestID == "" || secondRequestID == "" || firstRequestID == secondRequestID {
		t.Fatalf("request IDs = %q/%q, want unique non-empty IDs", firstRequestID, secondRequestID)
	}
	if first.Header().Get("Deprecation") != "" || first.Header().Get("Sunset") != "" || first.Header().Get("Link") != "" {
		t.Fatalf("trend unexpectedly exposed compatibility headers: %+v", first.Header())
	}
	for _, expected := range []string{
		`"scope_version":"scope-version-trend-1"`, `"cache_status":"hit"`, `"source_status":"ok"`,
		`"request_id":"` + firstRequestID + `"`, `"comparison_total_count":14`, `"comparison_truncated":true`,
		`"series_type":"team_total"`, `"display_name":"Alice"`,
	} {
		if !strings.Contains(first.Body.String(), expected) {
			t.Fatalf("trend body = %s, want %s", first.Body.String(), expected)
		}
	}
}

func TestTeamUsageTrendMapsScopeAndInputFailures(t *testing.T) {
	t.Run("no representative scope", func(t *testing.T) {
		env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
			trendFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
				return nil, &teamusage.ForbiddenError{Reason: teamusage.ErrNotRepresentative.Error()}
			},
		})
		rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/trend?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=UTC", env.token, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("response = %d %s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid granularity", func(t *testing.T) {
		env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
			trendFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
				t.Fatal("trend service must not run for invalid input")
				return nil, nil
			},
		})
		rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/trend?granularity=week", env.token, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("response = %d %s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestTeamUsageTrendRequiresAuthentication(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		trendFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.TrendResponse, error) {
			t.Fatal("trend service must not run without authentication")
			return nil, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/trend", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("response = %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageMembersReturnsBoundedMetadataAndUniqueRequestID(t *testing.T) {
	asOf := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		membersFn: func(_ context.Context, actorID int, params teamusage.MembersParams) (*teamusage.MembersResponse, error) {
			if actorID != env.userID || params.StartDate != "2026-07-01" || params.EndDate != "2026-07-07" || params.Granularity != "day" || params.Timezone != "Asia/Shanghai" || params.Limit != 50 || params.Cursor != "cursor-page-2" {
				t.Fatalf("unexpected members request: actor=%d params=%+v", actorID, params)
			}
			tokens := int64(1234)
			return &teamusage.MembersResponse{
				SnapshotFreshness: teamusage.SnapshotFreshness{
					AsOf: asOf, FreshUntil: asOf.Add(54 * time.Second), StaleUntil: asOf.Add(4*time.Minute + 30*time.Second),
					CacheStatus: "fresh", SourceStatus: "ok",
				},
				ScopeVersion: "scope-version-members-1",
				Window: teamusage.OverviewWindow{
					StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai",
				},
				Items: []teamusage.OverviewMember{{
					Rank: 51, UserID: 101, DisplayName: "Alice", Email: "alice@example.com", TotalTokens: &tokens,
				}},
				TotalCount: 500,
				NextCursor: "cursor-page-3",
			}, nil
		},
	})

	path := "/api/v1/user/team-usage/members?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=Asia%2FShanghai&limit=50&cursor=cursor-page-2"
	first := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	second := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("members responses = %d/%d, want 200", first.Code, second.Code)
	}
	firstRequestID := first.Header().Get("X-Request-ID")
	secondRequestID := second.Header().Get("X-Request-ID")
	if firstRequestID == "" || secondRequestID == "" || firstRequestID == secondRequestID {
		t.Fatalf("request IDs = %q/%q, want unique non-empty IDs", firstRequestID, secondRequestID)
	}
	if first.Header().Get("Deprecation") != "" || first.Header().Get("Sunset") != "" || first.Header().Get("Link") != "" {
		t.Fatalf("members unexpectedly exposed compatibility headers: %+v", first.Header())
	}
	body := first.Body.String()
	for _, expected := range []string{
		`"scope_version":"scope-version-members-1"`, `"request_id":"` + firstRequestID + `"`,
		`"cache_status":"fresh"`, `"total_count":500`, `"next_cursor":"cursor-page-3"`, `"rank":51`, `"display_name":"Alice"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("members body = %s, want %s", body, expected)
		}
	}
	for _, forbidden := range []string{`"member_tree"`, `"members"`, `"top_members"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("members body = %s, must not contain %s", body, forbidden)
		}
	}
}

func TestTeamUsageMembersForwardsMissingLimitAsDefaultSelection(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		membersFn: func(_ context.Context, actorID int, params teamusage.MembersParams) (*teamusage.MembersResponse, error) {
			if actorID != env.userID || params.Limit != 0 || params.Cursor != "" {
				t.Fatalf("default members request: actor=%d params=%+v", actorID, params)
			}
			return &teamusage.MembersResponse{Items: []teamusage.OverviewMember{}}, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/members?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=UTC", env.token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageMembersMapsScopeCursorAndInputFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "no representative scope", err: &teamusage.ForbiddenError{Reason: teamusage.ErrNotRepresentative.Error()}, status: http.StatusForbidden, body: "not_representative"},
		{name: "invalid cursor", err: teamusage.ErrInvalidMemberCursor, status: http.StatusBadRequest, body: "invalid_cursor"},
		{name: "expired snapshot", err: teamusage.ErrMemberSnapshotExpired, status: http.StatusConflict, body: "snapshot_expired"},
		{name: "invalid limit", err: fmt.Errorf("%w: limit must be between 1 and 100", teamusage.ErrInvalidOverviewParams), status: http.StatusBadRequest, body: "limit must be between"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				membersFn: func(context.Context, int, teamusage.MembersParams) (*teamusage.MembersResponse, error) {
					return nil, tt.err
				},
			})
			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/members?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=UTC&limit=101", env.token, "")
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("response = %d %s, want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}

	t.Run("non-integer limit", func(t *testing.T) {
		env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
			membersFn: func(context.Context, int, teamusage.MembersParams) (*teamusage.MembersResponse, error) {
				t.Fatal("members service must not run for a non-integer limit")
				return nil, nil
			},
		})
		rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/members?limit=invalid", env.token, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("response = %d %s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestTeamUsageMembersRequiresAuthentication(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		membersFn: func(context.Context, int, teamusage.MembersParams) (*teamusage.MembersResponse, error) {
			t.Fatal("members service must not run without authentication")
			return nil, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/members", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("response = %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageOrganizationReturnsShallowCollectionsAndUniqueRequestID(t *testing.T) {
	asOf := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	parentID := "department-root"
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		organizationFn: func(_ context.Context, actorID int, params teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
			if actorID != env.userID || params.StartDate != "2026-07-01" || params.EndDate != "2026-07-07" || params.Granularity != "day" || params.Timezone != "Asia/Shanghai" ||
				params.ParentDepartmentExternalID != parentID || params.DepartmentCursor != "department-page-2" || params.DepartmentLimit != 25 || params.MemberCursor != "member-page-2" || params.MemberLimit != 50 {
				t.Fatalf("unexpected organization request: actor=%d params=%+v", actorID, params)
			}
			tokens := int64(1234)
			return &teamusage.OrganizationResponse{
				SnapshotFreshness: teamusage.SnapshotFreshness{
					AsOf: asOf, FreshUntil: asOf.Add(54 * time.Second), StaleUntil: asOf.Add(4*time.Minute + 30*time.Second), CacheStatus: "fresh", SourceStatus: "ok",
				},
				ScopeVersion: "scope-version-organization-1", ParentDepartmentExternalID: &parentID,
				Window:               teamusage.OverviewWindow{StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai"},
				Departments:          []teamusage.OrganizationDepartment{{DepartmentExternalID: "department-alpha", Name: "Alpha", ChildCount: 1, HasChildren: true, DirectMemberCount: 2, AggregateMemberCount: 3}},
				Members:              []teamusage.OverviewMember{{Rank: 51, UserID: 101, DisplayName: "Alice", Email: "alice@example.com", TotalTokens: &tokens}},
				NextDepartmentCursor: "department-page-3", NextMemberCursor: "member-page-3",
			}, nil
		},
	})

	path := "/api/v1/user/team-usage/organization?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=Asia%2FShanghai&parent_department_external_id=department-root&department_cursor=department-page-2&department_limit=25&member_cursor=member-page-2&member_limit=50"
	first := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	second := performTeamUsageRequest(env.router, http.MethodGet, path, env.token, "")
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("organization responses = %d/%d, want 200", first.Code, second.Code)
	}
	firstRequestID := first.Header().Get("X-Request-ID")
	secondRequestID := second.Header().Get("X-Request-ID")
	if firstRequestID == "" || secondRequestID == "" || firstRequestID == secondRequestID {
		t.Fatalf("request IDs = %q/%q, want unique non-empty IDs", firstRequestID, secondRequestID)
	}
	body := first.Body.String()
	for _, expected := range []string{`"request_id":"` + firstRequestID + `"`, `"parent_department_external_id":"department-root"`, `"departments":[`, `"members":[`, `"has_children":true`, `"next_department_cursor":"department-page-3"`, `"next_member_cursor":"member-page-3"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("organization body = %s, want %s", body, expected)
		}
	}
	for _, forbidden := range []string{`"children"`, `"member_tree"`, `"top_members"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("organization body = %s, must not contain %s", body, forbidden)
		}
	}
	if first.Header().Get("Deprecation") != "" || first.Header().Get("Sunset") != "" {
		t.Fatalf("organization unexpectedly exposed compatibility headers: %+v", first.Header())
	}
}

func TestTeamUsageOrganizationForwardsDefaultLimits(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		organizationFn: func(_ context.Context, actorID int, params teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
			if actorID != env.userID || params.DepartmentLimit != 0 || params.MemberLimit != 0 || params.ParentDepartmentExternalID != "" || params.DepartmentCursor != "" || params.MemberCursor != "" {
				t.Fatalf("default organization request: actor=%d params=%+v", actorID, params)
			}
			return &teamusage.OrganizationResponse{Departments: []teamusage.OrganizationDepartment{}, Members: []teamusage.OverviewMember{}}, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/organization?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=UTC", env.token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageOrganizationMapsScopedCursorAndInputFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "no representative scope", err: &teamusage.ForbiddenError{Reason: teamusage.ErrNotRepresentative.Error()}, status: http.StatusForbidden, body: "not_representative"},
		{name: "outside parent", err: teamusage.ErrOutOfScope, status: http.StatusNotFound, body: "target is not available"},
		{name: "invalid cursor", err: teamusage.ErrInvalidOrganizationCursor, status: http.StatusBadRequest, body: "invalid_cursor"},
		{name: "expired snapshot", err: teamusage.ErrOrganizationSnapshotExpired, status: http.StatusConflict, body: "snapshot_expired"},
		{name: "invalid limit", err: fmt.Errorf("%w: department_limit must be between 1 and 100", teamusage.ErrInvalidOverviewParams), status: http.StatusBadRequest, body: "department_limit must be between"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				organizationFn: func(context.Context, int, teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
					return nil, tt.err
				},
			})
			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/organization?department_limit=101", env.token, "")
			if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.body) {
				t.Fatalf("response = %d %s, want %d containing %q", rec.Code, rec.Body.String(), tt.status, tt.body)
			}
		})
	}

	for _, field := range []string{"department_limit", "member_limit"} {
		t.Run("non-integer "+field, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				organizationFn: func(context.Context, int, teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
					t.Fatal("organization service must not run for a non-integer limit")
					return nil, nil
				},
			})
			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/organization?"+field+"=invalid", env.token, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("response = %d %s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTeamUsageOrganizationRequiresAuthentication(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		organizationFn: func(context.Context, int, teamusage.OrganizationParams) (*teamusage.OrganizationResponse, error) {
			t.Fatal("organization service must not run without authentication")
			return nil, nil
		},
	})
	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/organization", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("response = %d %s, want 401", rec.Code, rec.Body.String())
	}
}

func TestTeamOverviewEmitsCompatibilityHeadersOnSuccessAndFailure(t *testing.T) {
	tests := []struct {
		name   string
		result *teamusage.OverviewResponse
		err    error
		status int
	}{
		{name: "success", result: &teamusage.OverviewResponse{Configured: true, IsRepresentative: true}, status: http.StatusOK},
		{name: "failure", err: errors.New("synthetic overview failure"), status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
					return tt.result, tt.err
				},
			})
			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/overview?start_date=2026-07-01&end_date=2026-07-07&granularity=day&timezone=UTC", env.token, "")
			if rec.Code != tt.status {
				t.Fatalf("response = %d %s, want %d", rec.Code, rec.Body.String(), tt.status)
			}
			if rec.Header().Get("Deprecation") != "@1783987200" || rec.Header().Get("Sunset") != "Tue, 15 Sep 2026 00:00:00 GMT" || rec.Header().Get("Link") != `</api/v1/user/team-usage/summary>; rel="successor-version"` {
				t.Fatalf("compatibility headers = Deprecation %q Sunset %q Link %q", rec.Header().Get("Deprecation"), rec.Header().Get("Sunset"), rec.Header().Get("Link"))
			}
		})
	}
}

func performTeamUsageRequest(router *gin.Engine, method, path, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestTeamUsageScopeReturnsRepresentativeDepartments(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn: func(_ context.Context, actorID int) (*teamusage.ScopeResponse, error) {
			if actorID != env.userID {
				t.Fatalf("actorID = %d, want %d", actorID, env.userID)
			}
			return &teamusage.ScopeResponse{
				IsRepresentative: true,
				Departments: []representativescope.DepartmentScope{{
					ExternalID: "department-alpha",
					Name:       "Department Alpha",
				}},
			}, nil
		},
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/scope", env.token, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Department Alpha") {
		t.Fatalf("response = %d %s, want representative department", rec.Code, rec.Body.String())
	}
}

func TestTeamUsageSubjectsIncludesMyUsageAndScopedMembers(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn: func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(_ context.Context, actorID int, q string, page, pageSize int) (*teamusage.SubjectsResponse, error) {
			if actorID != env.userID {
				t.Fatalf("actorID = %d, want %d", actorID, env.userID)
			}
			if q != "ali" || page != 2 || pageSize != 5 {
				t.Fatalf("query params = %q/%d/%d", q, page, pageSize)
			}
			return &teamusage.SubjectsResponse{Subjects: []representativescope.Subject{
				{SubjectType: "self", UserID: actorID, DisplayName: "Me", Email: "me@example.com", Selectable: true},
				{SubjectType: "member", UserID: 101, DisplayName: "Alice", Email: "alice@example.com", Selectable: true},
			}}, nil
		},
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/subjects?q=ali&page=2&page_size=5", env.token, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Alice") || !strings.Contains(rec.Body.String(), "self") {
		t.Fatalf("response = %d %s, want My Usage and Alice", rec.Code, rec.Body.String())
	}
}

func TestSelectedMemberDashboardRejectsOutOfScopeUser(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn: func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) {
			return nil, nil
		},
		subjectDashboardFn: func(_ context.Context, actorID, targetUserID int, params relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			if actorID != env.userID || targetUserID != 999 {
				t.Fatalf("actor/target = %d/%d", actorID, targetUserID)
			}
			if params.StartDate != "2026-06-01" || params.EndDate != "2026-06-06" || params.Granularity != "day" || params.Timezone != "Asia/Shanghai" {
				t.Fatalf("params = %+v", params)
			}
			return nil, teamusage.ErrOutOfScope
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/subjects/999/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai", env.token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for scoped target lookup", rec.Code)
	}
}

func TestSelectedMemberDashboardMapsForbiddenOutOfScopeToNotFound(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, &teamusage.ForbiddenError{Reason: teamusage.ErrOutOfScope.Error()}
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/subjects/999/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai", env.token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for forbidden out_of_scope", rec.Code)
	}
}

func TestSelectedMemberDashboardMapsForbiddenRelayStateConflictsToConflict(t *testing.T) {
	testCases := []struct {
		name   string
		reason string
	}{
		{name: "no relay mapping", reason: teamusage.ErrNoRelayMapping.Error()},
		{name: "inactive subscription", reason: teamusage.ErrInactiveSubscription.Error()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
				scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
				subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
				subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
					return nil, &teamusage.ForbiddenError{Reason: tc.reason}
				},
				overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
					return nil, nil
				},
				updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
					return nil, nil
				},
				listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
					return nil, nil
				},
				listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
					return nil, nil
				},
			})

			rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/subjects/999/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai", env.token, "")
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 for forbidden %q", rec.Code, tc.reason)
			}
		})
	}
}

func TestSelectedMemberDashboardMapsForbiddenProviderUnsupportedToServiceUnavailable(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, &teamusage.ForbiddenError{Reason: teamusage.ErrProviderUnsupported.Error()}
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/subjects/999/usage/dashboard?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai", env.token, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 for forbidden provider_unsupported", rec.Code)
	}
}

func TestUpdateMultiplierRejectsSelfAndDoesNotCallRelay(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(_ context.Context, actorID, targetUserID int, groupID int64, req teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			if actorID != env.userID || targetUserID != 100 || groupID != 42 || req.Mode != "set" || req.RateMultiplier == nil || *req.RateMultiplier != 2 {
				t.Fatalf("unexpected request: actor=%d target=%d group=%d req=%+v", actorID, targetUserID, groupID, req)
			}
			return nil, teamusage.ErrSelfEditForbidden
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodPut, "/api/v1/user/team-usage/subjects/100/groups/42/rate-multiplier", env.token, `{"mode":"set","rate_multiplier":2}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUpdateMultiplierRejectsPeerRepresentative(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, teamusage.ErrNotUpperLevelRepresentative
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodPut, "/api/v1/user/team-usage/subjects/101/groups/42/rate-multiplier", env.token, `{"mode":"reset"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUpdateMultiplierReturnsFailureForPartialFailedAudit(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, fmt.Errorf("%w: readback multiplier mismatch for subscription 42", teamusage.ErrPartialFailed)
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodPut, "/api/v1/user/team-usage/subjects/101/groups/42/rate-multiplier", env.token, `{"mode":"set","rate_multiplier":2}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for partial_failed audit: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "rate multiplier update could not be verified") {
		t.Fatalf("body = %s, want generic verification failure", body)
	}
	if strings.Contains(body, "readback") || strings.Contains(body, "mismatch") || strings.Contains(body, "subscription 42") {
		t.Fatalf("body = %s, want internal readback details hidden", body)
	}
}

func TestTeamOverviewNeverReturnsGroupQuotas(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(_ context.Context, actorID int, params teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			if actorID != env.userID || params.StartDate != "2026-06-01" || params.EndDate != "2026-06-06" || params.Granularity != "day" || params.Timezone != "Asia/Shanghai" {
				t.Fatalf("unexpected overview request: actor=%d params=%+v", actorID, params)
			}
			return &teamusage.OverviewResponse{
				Configured:       true,
				IsRepresentative: true,
				Members: []teamusage.OverviewMember{{
					UserID:      101,
					DisplayName: "Alice",
				}},
			}, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/overview?start_date=2026-06-01&end_date=2026-06-06&granularity=day&timezone=Asia%2FShanghai", env.token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 before leakage assertions", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "group_quotas") || strings.Contains(rec.Body.String(), "subject_subscription_groups") {
		t.Fatalf("team overview leaked quota payload: %s", rec.Body.String())
	}
}

func TestRepresentativeAuditRedactsOutOfScopeTarget(t *testing.T) {
	reason := "out_of_scope"
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(_ context.Context, actorID int, params teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			if actorID != env.userID || params.TargetUserID == nil || *params.TargetUserID != 101 || params.Page != 3 || params.PageSize != 7 {
				t.Fatalf("unexpected audit request: actor=%d params=%+v", actorID, params)
			}
			return &teamusage.AuditListResponse{
				Items: []teamusage.AuditRecord{{
					ID:              1,
					RejectionReason: &reason,
				}},
			}, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/user/team-usage/audit?target_user_id=101&page=3&page_size=7", env.token, "")
	if strings.Contains(rec.Body.String(), "target_user_id") || strings.Contains(rec.Body.String(), "alice@example.com") {
		t.Fatalf("audit response leaked target details: %s", rec.Body.String())
	}
}

func TestAdminTeamUsageAuditPassesFilters(t *testing.T) {
	var env *teamUsageTestEnv
	env = newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			return nil, nil
		},
		listAdminAuditFn: func(_ context.Context, params teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			if params.ActorUserID == nil || *params.ActorUserID != env.userID {
				t.Fatalf("actor filter = %#v", params.ActorUserID)
			}
			if params.TargetUserID == nil || *params.TargetUserID != 101 {
				t.Fatalf("target filter = %#v", params.TargetUserID)
			}
			if params.Status != "partial_failed" || params.Page != 2 || params.PageSize != 9 {
				t.Fatalf("params = %+v", params)
			}
			return &teamusage.AuditListResponse{
				Items: []teamusage.AuditRecord{{ID: 5}},
			}, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, fmt.Sprintf("/api/v1/admin/team-usage/audit?actor_user_id=%d&target_user_id=101&status=partial_failed&page=2&page_size=9", env.userID), env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":5`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminTeamUsageAuditRejectsNonAdminUser(t *testing.T) {
	env := newTeamUsageTestRouter(t, &fakeTeamUsageService{
		scopeFn:    func(context.Context, int) (*teamusage.ScopeResponse, error) { return nil, nil },
		subjectsFn: func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error) { return nil, nil },
		subjectDashboardFn: func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error) {
			t.Fatal("subject dashboard should not be called")
			return nil, nil
		},
		overviewFn: func(context.Context, int, teamusage.OverviewParams) (*teamusage.OverviewResponse, error) {
			t.Fatal("overview should not be called")
			return nil, nil
		},
		updateMultiplierFn: func(context.Context, int, int, int64, teamusage.UpdateMultiplierRequest) (*teamusage.UpdateMultiplierResponse, error) {
			t.Fatal("update multiplier should not be called")
			return nil, nil
		},
		listAuditFn: func(context.Context, int, teamusage.AuditListParams) (*teamusage.AuditListResponse, error) {
			t.Fatal("representative audit should not be called")
			return nil, nil
		},
		listAdminAuditFn: func(context.Context, teamusage.AdminAuditListParams) (*teamusage.AuditListResponse, error) {
			t.Fatal("admin audit service should not be called for non-admin user")
			return nil, nil
		},
	})

	rec := performTeamUsageRequest(env.router, http.MethodGet, "/api/v1/admin/team-usage/audit", env.token, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-admin user", rec.Code)
	}
}
