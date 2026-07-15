package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakeQuotaResetService struct {
	optionsFn                    func(context.Context, int) (*quotareset.OptionsResponse, error)
	createFn                     func(context.Context, quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error)
	approveFn                    func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	rejectFn                     func(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error)
	listApproverCandidatesFn     func(context.Context, int, string) (*quotareset.ApproverCandidateListResponse, error)
	listApproverConfigsFn        func(context.Context) (*quotareset.ApproverConfigListResponse, error)
	saveApproverConfigsFn        func(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error)
	listApprovalChainsFn         func(context.Context) (*quotareset.ApprovalChainListResponse, error)
	saveApprovalChainsFn         func(context.Context, int, []quotareset.ApprovalChainInput) (*quotareset.ApprovalChainListResponse, error)
	getNotificationSettingsFn    func(context.Context) (*quotareset.NotificationSettings, error)
	updateNotificationSettingsFn func(context.Context, quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error)
}

func (f *fakeQuotaResetService) Options(ctx context.Context, userID int) (*quotareset.OptionsResponse, error) {
	if f.optionsFn != nil {
		return f.optionsFn(ctx, userID)
	}
	return &quotareset.OptionsResponse{}, nil
}

func (f *fakeQuotaResetService) CreateRequest(ctx context.Context, input quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error) {
	return f.createFn(ctx, input)
}

func (f *fakeQuotaResetService) Cancel(context.Context, int, int) (*ent.QuotaResetRequest, error) {
	return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusCancelled}, nil
}

func (f *fakeQuotaResetService) Approve(ctx context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	return f.approveFn(ctx, input)
}

func (f *fakeQuotaResetService) Reject(ctx context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	return f.rejectFn(ctx, input)
}

func (f *fakeQuotaResetService) RetryReset(context.Context, quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
	return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
}

func (f *fakeQuotaResetService) ListMine(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApprovals(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListAdmin(context.Context, quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApproverCandidates(ctx context.Context, sourceID int, departmentExternalID string) (*quotareset.ApproverCandidateListResponse, error) {
	if f.listApproverCandidatesFn != nil {
		return f.listApproverCandidatesFn(ctx, sourceID, departmentExternalID)
	}
	return &quotareset.ApproverCandidateListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApproverConfigs(ctx context.Context) (*quotareset.ApproverConfigListResponse, error) {
	if f.listApproverConfigsFn != nil {
		return f.listApproverConfigsFn(ctx)
	}
	return &quotareset.ApproverConfigListResponse{}, nil
}

func (f *fakeQuotaResetService) SaveApproverConfigs(ctx context.Context, input quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
	if f.saveApproverConfigsFn != nil {
		return f.saveApproverConfigsFn(ctx, input)
	}
	return &quotareset.ApproverConfigListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApprovalChains(ctx context.Context) (*quotareset.ApprovalChainListResponse, error) {
	if f.listApprovalChainsFn != nil {
		return f.listApprovalChainsFn(ctx)
	}
	return &quotareset.ApprovalChainListResponse{}, nil
}

func (f *fakeQuotaResetService) SaveApprovalChains(ctx context.Context, actorID int, items []quotareset.ApprovalChainInput) (*quotareset.ApprovalChainListResponse, error) {
	if f.saveApprovalChainsFn != nil {
		return f.saveApprovalChainsFn(ctx, actorID, items)
	}
	return &quotareset.ApprovalChainListResponse{}, nil
}

func (f *fakeQuotaResetService) GetNotificationSettings(ctx context.Context) (*quotareset.NotificationSettings, error) {
	if f.getNotificationSettingsFn != nil {
		return f.getNotificationSettingsFn(ctx)
	}
	return &quotareset.NotificationSettings{}, nil
}

func (f *fakeQuotaResetService) UpdateNotificationSettings(ctx context.Context, input quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error) {
	if f.updateNotificationSettingsFn != nil {
		return f.updateNotificationSettingsFn(ctx, input)
	}
	return &quotareset.NotificationSettings{}, nil
}

func (f *fakeQuotaResetService) TestNotificationSettings(context.Context, int) error {
	return nil
}

func TestQuotaResetCreateRequestPassesActorAndBody(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		createFn: func(_ context.Context, input quotareset.CreateRequestInput) (*ent.QuotaResetRequest, error) {
			if input.RequesterUserID != 1 || input.GroupID != "42" || input.Reason != "Need reset" {
				t.Fatalf("input = %+v", input)
			}
			return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusPending}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/user/quota-reset/requests", env.userToken, `{"group_id":"42","reason":"Need reset"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetAdminApproveUsesAdminFlag(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		approveFn: func(_ context.Context, input quotareset.DecisionInput) (*ent.QuotaResetRequest, error) {
			if input.ActorUserID != 2 || input.RequestID != 99 || !input.Admin {
				t.Fatalf("input = %+v", input)
			}
			return &ent.QuotaResetRequest{ID: 99, Status: quotaresetrequest.StatusApprovedResetSucceeded}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPost, "/api/v1/admin/quota-reset/requests/99/approve", env.adminToken, `{}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"approved_reset_succeeded"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetSaveApproverConfigsPassesMode(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		saveApproverConfigsFn: func(_ context.Context, input quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
			if input.ActorUserID != 2 || input.Mode != quotareset.ApproverConfigSaveModeReplaceAll || len(input.Items) != 1 {
				t.Fatalf("input = %+v", input)
			}
			return &quotareset.ApproverConfigListResponse{}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approver-configs", env.adminToken, `{"mode":"replace_all","items":[{"department_external_id":"dept-alpha","approver_user_id":7,"enabled":true}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetListApproverCandidatesPassesDepartmentSelection(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, sourceID int, departmentExternalID string) (*quotareset.ApproverCandidateListResponse, error) {
			if sourceID != 3 || departmentExternalID != "department-alpha" {
				t.Fatalf("sourceID/department = %d/%s", sourceID, departmentExternalID)
			}
			return &quotareset.ApproverCandidateListResponse{Items: []quotareset.ApproverCandidate{{
				UserID:                    12,
				Username:                  "lead-alpha",
				Email:                     "lead-alpha@example.com",
				DisplayName:               "Lead Alpha",
				DirectoryMemberExternalID: "member-alpha-lead",
			}}}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approver-candidates?source_id=3&department_external_id=department-alpha", env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lead-alpha@example.com"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetSaveApprovalChainsPassesAdminActorAndOrderedDepartments(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		saveApprovalChainsFn: func(_ context.Context, actorID int, items []quotareset.ApprovalChainInput) (*quotareset.ApprovalChainListResponse, error) {
			if actorID != 2 || len(items) != 1 || items[0].GroupID != "42" || len(items[0].Departments) != 2 {
				t.Fatalf("actor/items = %d/%+v", actorID, items)
			}
			if items[0].Departments[0].DepartmentExternalID != "dept-alpha" || items[0].Departments[1].DepartmentExternalID != "dept-beta" {
				t.Fatalf("department order = %+v", items[0].Departments)
			}
			return &quotareset.ApprovalChainListResponse{Items: []quotareset.ApprovalChain{{GroupID: "42"}}}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approval-chains", env.adminToken, `{
		"items":[{
			"provider_id":1,
			"group_id":"42",
			"group_name":"Group Alpha",
			"enabled":true,
			"departments":[
				{"directory_source_id":3,"department_external_id":"dept-alpha","department_display_path":"Company / Group Alpha"},
				{"directory_source_id":3,"department_external_id":"dept-beta","department_display_path":"Company / Group Beta"}
			]
		}]
	}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"group_id":"42"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetUpdateNotificationPassesExplicitChannel(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		updateNotificationSettingsFn: func(_ context.Context, input quotareset.UpdateNotificationSettingsInput) (*quotareset.NotificationSettings, error) {
			if input.ActorUserID != 2 || input.Channel != "wecom_group_robot" || input.URL != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=redacted-test-key" {
				t.Fatalf("input = %+v", input)
			}
			return &quotareset.NotificationSettings{Enabled: true, Channel: input.Channel, URL: input.URL, AuthType: "none"}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/notification-settings", env.adminToken, `{
		"enabled":true,
		"channel":"wecom_group_robot",
		"url":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=redacted-test-key",
		"auth_type":"none"
	}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"channel":"wecom_group_robot"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

type quotaResetHandlerTestEnv struct {
	router     *gin.Engine
	userToken  string
	adminToken string
}

func newQuotaResetHandlerTestEnv(t *testing.T, service *fakeQuotaResetService) *quotaResetHandlerTestEnv {
	t.Helper()
	client := testdb.Open(t)
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)

	user := client.User.Create().
		SetUsername("member").
		SetEmail("member@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleUser).
		SaveX(context.Background())
	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.RoleAdmin).
		SaveX(context.Background())

	userPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: user.ID, Username: user.Username, Role: string(user.Role)})
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}
	adminPair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: admin.ID, Username: admin.Username, Role: string(admin.Role)})
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	router := gin.New()
	handler := NewQuotaResetHandler(service)
	userGroup := router.Group("/api/v1/user")
	userGroup.Use(auth.RequireAuth(authSvc))
	userGroup.POST("/quota-reset/requests", handler.CreateRequest)
	userGroup.POST("/quota-reset/approvals/:id/approve", handler.Approve)

	adminGroup := router.Group("/api/v1/admin/quota-reset")
	adminGroup.Use(auth.RequireAuth(authSvc), auth.RequireAdmin())
	adminGroup.POST("/requests/:id/approve", handler.AdminApprove)
	adminGroup.GET("/approver-candidates", handler.ListApproverCandidates)
	adminGroup.PUT("/approver-configs", handler.SaveApproverConfigs)
	adminGroup.GET("/approval-chains", handler.ListApprovalChains)
	adminGroup.PUT("/approval-chains", handler.SaveApprovalChains)
	adminGroup.PUT("/notification-settings", handler.UpdateNotificationSettings)

	return &quotaResetHandlerTestEnv{
		router:     router,
		userToken:  userPair.AccessToken,
		adminToken: adminPair.AccessToken,
	}
}

func performQuotaResetRequest(router *gin.Engine, method, path, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
