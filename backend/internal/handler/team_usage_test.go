package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakeTeamUsageService struct {
	scopeFn            func(context.Context, int) (*teamusage.ScopeResponse, error)
	subjectsFn         func(context.Context, int, string, int, int) (*teamusage.SubjectsResponse, error)
	subjectDashboardFn func(context.Context, int, int, relay.UserUsageDashboardParams) (*teamusage.SubjectDashboardResponse, error)
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
	userGroup := router.Group("/api/v1/user")
	userGroup.Use(authpkg.RequireAuth(authSvc))
	teamHandler := NewTeamUsageHandler(service)
	userGroup.GET("/team-usage/scope", teamHandler.Scope)
	userGroup.GET("/team-usage/subjects", teamHandler.Subjects)
	userGroup.GET("/team-usage/subjects/:user_id/usage/dashboard", teamHandler.SubjectDashboard)
	userGroup.PUT("/team-usage/subjects/:user_id/groups/:group_id/rate-multiplier", teamHandler.UpdateMultiplier)
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
