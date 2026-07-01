package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/gin-gonic/gin"
)

type stubUserSetupService struct {
	listResponse           *usersetup.ListProvidersResponse
	listErr                error
	createGroupResult      *usersetup.CreateGroupCredentialResult
	createGroupErr         error
	regenerateGroupResult  *usersetup.CreateGroupCredentialResult
	regenerateGroupErr     error
	lastListReq            usersetup.ListProvidersRequest
	lastCreateGroupReq     usersetup.CreateGroupCredentialRequest
	lastRegenerateGroupReq usersetup.RegenerateGroupCredentialRequest
}

func (s *stubUserSetupService) ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error) {
	s.lastListReq = req
	return s.listResponse, s.listErr
}

func (s *stubUserSetupService) CreateGroupCredential(ctx context.Context, req usersetup.CreateGroupCredentialRequest) (*usersetup.CreateGroupCredentialResult, error) {
	s.lastCreateGroupReq = req
	return s.createGroupResult, s.createGroupErr
}

func (s *stubUserSetupService) RegenerateGroupCredential(ctx context.Context, req usersetup.RegenerateGroupCredentialRequest) (*usersetup.CreateGroupCredentialResult, error) {
	s.lastRegenerateGroupReq = req
	return s.regenerateGroupResult, s.regenerateGroupErr
}

func TestUserProvidersRequiresAuth(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequestWithToken(env, http.MethodGet, "/api/v1/user/providers", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestUserProvidersReturnsGroupsPerProvider(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		listResponse: &usersetup.ListProvidersResponse{
			Providers: []usersetup.ProviderSummary{
				{
					ID:           7,
					Name:         "sub2api",
					DisplayName:  "sub2api",
					BaseURL:      "https://sub2api.agoraio.cn/",
					DefaultModel: "gpt-5.4",
					IsPrimary:    true,
					Groups: []usersetup.GroupCredentialSummary{
						{
							GroupID:   "42",
							GroupName: "Group One",
							Platform:  "openai",
							Credential: usersetup.GroupCredentialState{
								State:    "existing_hidden",
								APIKeyID: 44,
								Name:     "user_key",
								Status:   "active",
							},
						},
					},
				},
			},
		},
	}

	router := gin.New()
	router.GET("/api/v1/user/providers", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).ListProviders)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/providers", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastListReq.UserID != env.userID {
		t.Fatalf("user id = %d, want %d", stub.lastListReq.UserID, env.userID)
	}
	if !strings.Contains(w.Body.String(), "\"groups\"") || strings.Contains(w.Body.String(), "\"platforms\"") {
		t.Fatalf("response should contain groups and not platforms: %s", w.Body.String())
	}
}

func TestCreateGroupCredentialTranslatesRouteParams(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		createGroupResult: &usersetup.CreateGroupCredentialResult{
			APIKeyID: 77,
			Name:     "user_key",
			Status:   "active",
			Secret:   "sk-new",
		},
	}

	router := gin.New()
	router.POST("/api/v1/user/providers/:id/groups/:group_id/credential", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).CreateGroupCredential)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/groups/42/credential", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastCreateGroupReq.ProviderID != 7 || stub.lastCreateGroupReq.GroupID != "42" {
		t.Fatalf("unexpected request: %+v", stub.lastCreateGroupReq)
	}
}

func TestRegenerateGroupCredentialTranslatesRouteParams(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		regenerateGroupResult: &usersetup.CreateGroupCredentialResult{
			APIKeyID: 88,
			Name:     "user_key",
			Status:   "active",
			Secret:   "sk-regen",
		},
	}

	router := gin.New()
	router.POST("/api/v1/user/providers/:id/groups/:group_id/credential/regenerate", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).RegenerateGroupCredential)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/groups/42/credential/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastRegenerateGroupReq.UserID != env.userID || stub.lastRegenerateGroupReq.ProviderID != 7 || stub.lastRegenerateGroupReq.GroupID != "42" {
		t.Fatalf("got request %#v, want user=%d provider=7 group=42", stub.lastRegenerateGroupReq, env.userID)
	}
}
