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
	listResponse      *usersetup.ListProvidersResponse
	listErr           error
	createResult      *usersetup.CreateManagedKeyResult
	createErr         error
	regenerateResult  *usersetup.CreateManagedKeyResult
	regenerateErr     error
	lastListReq       usersetup.ListProvidersRequest
	lastCreateReq     usersetup.CreateManagedKeyRequest
	lastRegenerateReq usersetup.RegenerateManagedKeyRequest
}

func (s *stubUserSetupService) ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error) {
	s.lastListReq = req
	return s.listResponse, s.listErr
}

func (s *stubUserSetupService) CreateManagedKey(ctx context.Context, req usersetup.CreateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error) {
	s.lastCreateReq = req
	return s.createResult, s.createErr
}

func (s *stubUserSetupService) RegenerateManagedKey(ctx context.Context, req usersetup.RegenerateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error) {
	s.lastRegenerateReq = req
	return s.regenerateResult, s.regenerateErr
}

func TestUserProvidersRequiresAuth(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequestWithToken(env, http.MethodGet, "/api/v1/user/providers", nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestUserProvidersReturnsCurrentUserProviders(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		listResponse: &usersetup.ListProvidersResponse{
			Providers: []usersetup.ProviderSummary{
				{
					ID:           7,
					Name:         "sub2api-prod",
					DisplayName:  "sub2api Production",
					BaseURL:      "https://relay.example.com",
					DefaultModel: "claude-sonnet-4-20250514",
					IsPrimary:    true,
					ManagedKey: usersetup.ManagedKeySummary{
						State:    "existing_hidden",
						APIKeyID: 44,
						Name:     "ae-cli-auto",
						Status:   "active",
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
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response should not contain secret: %s", w.Body.String())
	}
}

func TestCreateManagedKeyReturnsSecretOnce(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		createResult: &usersetup.CreateManagedKeyResult{
			APIKeyID: 77,
			Name:     "ae-cli-auto",
			Status:   "active",
			Secret:   "sk-new",
		},
	}

	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).CreateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastCreateReq.ProviderID != 7 {
		t.Fatalf("provider id = %d, want 7", stub.lastCreateReq.ProviderID)
	}
	if !strings.Contains(w.Body.String(), "sk-new") {
		t.Fatalf("response missing secret: %s", w.Body.String())
	}
}

func TestCreateManagedKeyConflictsWhenManagedKeyExists(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{createErr: usersetup.ErrManagedKeyAlreadyExists}

	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).CreateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestRegenerateManagedKeyTranslatesProviderIDAndReturnsSecret(t *testing.T) {
	env := setupTestEnv(t)
	stub := &stubUserSetupService{
		regenerateResult: &usersetup.CreateManagedKeyResult{
			APIKeyID: 88,
			Name:     "ae-cli-auto",
			Status:   "active",
			Secret:   "sk-regen",
		},
	}

	router := gin.New()
	router.POST("/api/v1/user/providers/:id/managed-key/regenerate", authpkg.RequireAuth(env.authSvc), NewUserSetupHandler(stub).RegenerateManagedKey)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/providers/7/managed-key/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if stub.lastRegenerateReq.UserID != env.userID || stub.lastRegenerateReq.ProviderID != 7 {
		t.Fatalf("got request %#v, want user=%d provider=7", stub.lastRegenerateReq, env.userID)
	}
}
