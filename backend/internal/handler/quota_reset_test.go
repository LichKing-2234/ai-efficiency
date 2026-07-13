package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	listAdminFn                  func(context.Context, int, quotareset.ListParams) (*quotareset.RequestListResponse, error)
	listApproverCandidatesFn     func(context.Context, quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error)
	listApproverConfigsFn        func(context.Context) (*quotareset.ApproverConfigListResponse, error)
	saveApproverConfigsFn        func(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error)
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

func (f *fakeQuotaResetService) ListAdmin(ctx context.Context, actorUserID int, params quotareset.ListParams) (*quotareset.RequestListResponse, error) {
	if f.listAdminFn != nil {
		return f.listAdminFn(ctx, actorUserID, params)
	}
	return &quotareset.RequestListResponse{}, nil
}

func (f *fakeQuotaResetService) ListApproverCandidates(ctx context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
	if f.listApproverCandidatesFn != nil {
		return f.listApproverCandidatesFn(ctx, params)
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

func TestQuotaResetListAdminForwardsAuthenticatedActorID(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listAdminFn: func(_ context.Context, actorUserID int, params quotareset.ListParams) (*quotareset.RequestListResponse, error) {
			if actorUserID != 2 || params.Page != 3 || params.PageSize != 7 || params.Status != "pending" {
				t.Fatalf("actor/params = %d / %+v", actorUserID, params)
			}
			return &quotareset.RequestListResponse{Page: 3, PageSize: 7}, nil
		},
	})
	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/requests?page=3&page_size=7&status=pending", env.adminToken, "")
	if rec.Code != http.StatusOK {
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

func TestQuotaResetSaveApproverConfigsMapsReferencedConflict(t *testing.T) {
	detail := `enabled approval chains reference departments without approver configs: provider_id=1 group_id=42 group_name="Group Alpha"`
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		saveApproverConfigsFn: func(context.Context, quotareset.SaveApproverConfigsInput) (*quotareset.ApproverConfigListResponse, error) {
			return nil, fmt.Errorf("save approver configs: %w: %s", quotareset.ErrApproverConfigReferenced, detail)
		},
	})

	rec := performQuotaResetRequest(env.router, http.MethodPut, "/api/v1/admin/quota-reset/approver-configs", env.adminToken, `{"mode":"replace_all","items":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	wantMessage := "save approver configs: " + quotareset.ErrApproverConfigReferenced.Error() + ": " + detail
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Message != wantMessage {
		t.Fatalf("message = %q, want %q", body.Message, wantMessage)
	}
}

func TestQuotaResetListApproverCandidatesPassesSearchAndPagination(t *testing.T) {
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			if params.SourceID != 3 || params.Query != "Alice" || params.Page != 2 || params.PageSize != 15 {
				t.Fatalf("params = %+v", params)
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
	rec := performQuotaResetRequest(env.router, http.MethodGet, "/api/v1/admin/quota-reset/approver-candidates?source_id=3&department_external_id=department-alpha&q=%20Alice%20&page=2&page_size=15", env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lead-alpha@example.com"`) {
		t.Fatalf("response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQuotaResetListApproverCandidatesForwardsMaxIntPage(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	env := newQuotaResetHandlerTestEnv(t, &fakeQuotaResetService{
		listApproverCandidatesFn: func(_ context.Context, params quotareset.ApproverCandidateParams) (*quotareset.ApproverCandidateListResponse, error) {
			if params.SourceID != 3 || params.Page != maxInt || params.PageSize != 20 {
				t.Fatalf("params = %+v", params)
			}
			return &quotareset.ApproverCandidateListResponse{
				Items:    []quotareset.ApproverCandidate{},
				Page:     params.Page,
				PageSize: params.PageSize,
				Total:    1,
			}, nil
		},
	})
	path := "/api/v1/admin/quota-reset/approver-candidates?source_id=3&page=" + strconv.Itoa(maxInt) + "&page_size=20"

	rec := performQuotaResetRequest(env.router, http.MethodGet, path, env.adminToken, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"page":`+strconv.Itoa(maxInt)) || !strings.Contains(rec.Body.String(), `"items":[]`) {
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
	adminGroup.GET("/requests", handler.ListAdmin)
	adminGroup.POST("/requests/:id/approve", handler.AdminApprove)
	adminGroup.GET("/approver-candidates", handler.ListApproverCandidates)
	adminGroup.PUT("/approver-configs", handler.SaveApproverConfigs)

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
