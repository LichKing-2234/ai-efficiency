package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authsettings"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakeAuthSettingsStore struct {
	oidc        *authsettings.OIDC
	ldap        *authsettings.LDAP
	savedOIDC   *authsettings.OIDC
	savedLDAP   *authsettings.LDAP
	deletedOIDC bool
}

func (f *fakeAuthSettingsStore) LoadOIDC(context.Context) (*authsettings.OIDC, bool, error) {
	if f.oidc == nil {
		return nil, false, nil
	}
	value := *f.oidc
	return &value, true, nil
}

func (f *fakeAuthSettingsStore) SaveOIDC(_ context.Context, settings authsettings.OIDC) error {
	value := settings
	f.oidc = &value
	f.savedOIDC = &value
	return nil
}

func (f *fakeAuthSettingsStore) DeleteOIDC(context.Context) error {
	f.oidc = nil
	f.deletedOIDC = true
	return nil
}

func (f *fakeAuthSettingsStore) LoadLDAP(context.Context) (*authsettings.LDAP, bool, error) {
	if f.ldap == nil {
		return nil, false, nil
	}
	value := *f.ldap
	return &value, true, nil
}

func (f *fakeAuthSettingsStore) SaveLDAP(_ context.Context, settings authsettings.LDAP) error {
	value := settings
	f.ldap = &value
	f.savedLDAP = &value
	return nil
}

func newOIDCSettingsTestHandler(t *testing.T, store AuthSettingsStore, defaults config.OIDCConfig) (*AdminSettingsHandler, *auth.Service) {
	t.Helper()
	authService := auth.NewService(nil, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	var ldapConfig atomic.Pointer[config.LDAPConfig]
	ldapConfig.Store(&config.LDAPConfig{})
	handler := NewAdminSettingsHandler(store, authService, &ldapConfig, defaults, "https://app.example.test", nil)
	handler.buildOIDC = func(context.Context, auth.OIDCConfig) (*auth.OIDCService, error) {
		return &auth.OIDCService{}, nil
	}
	return handler, authService
}

func oidcSettingsTestRouter(handler *AdminSettingsHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/admin/settings/oidc")
	group.GET("", handler.GetOIDC)
	group.PUT("", handler.UpdateOIDC)
	group.DELETE("", handler.DeleteOIDC)
	group.POST("/test", handler.TestOIDC)
	return router
}

func performOIDCSettingsRequest(t *testing.T, router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeOIDCSettingsData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, recorder.Body.String())
	}
	return response.Data
}

func TestGetOIDCUsesDeploymentSettingsByDefault(t *testing.T) {
	store := &fakeAuthSettingsStore{}
	handler, _ := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{
		Enabled:      true,
		Issuer:       "https://idp.example.test/",
		ClientID:     "env-client",
		ClientSecret: "env-secret",
	})
	recorder := performOIDCSettingsRequest(t, oidcSettingsTestRouter(handler), http.MethodGet, "/api/v1/admin/settings/oidc", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	data := decodeOIDCSettingsData(t, recorder)
	if data["source"] != "environment" || data["enabled"] != true || data["client_id"] != "env-client" {
		t.Fatalf("data = %v", data)
	}
	if data["client_secret"] != maskedSecret {
		t.Fatalf("client_secret = %v", data["client_secret"])
	}
	if data["callback_path"] != "/auth/oidc/callback" {
		t.Fatalf("callback_path = %v", data["callback_path"])
	}
}

func TestUpdateOIDCPersistsAndKeepsMaskedSecret(t *testing.T) {
	store := &fakeAuthSettingsStore{}
	handler, authService := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{
		Issuer:       "https://idp.example.test/",
		ClientID:     "env-client",
		ClientSecret: "env-secret",
	})
	var builtConfig auth.OIDCConfig
	handler.buildOIDC = func(_ context.Context, cfg auth.OIDCConfig) (*auth.OIDCService, error) {
		builtConfig = cfg
		return &auth.OIDCService{}, nil
	}
	body := `{
		"enabled": true,
		"label": "Company SSO",
		"issuer": "https://idp.example.test/",
		"client_id": "admin-client",
		"client_secret": "***",
		"redirect_urls": ["https://app.example.test/auth/oidc/callback", " https://app.example.test/auth/oidc/callback "],
		"allow_email_link": true,
		"scopes": ["openid", "email", "email"]
	}`
	recorder := performOIDCSettingsRequest(t, oidcSettingsTestRouter(handler), http.MethodPut, "/api/v1/admin/settings/oidc", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.savedOIDC == nil {
		t.Fatal("expected OIDC settings to be saved")
	}
	if store.savedOIDC.ClientSecret != "env-secret" {
		t.Fatalf("saved client secret = %q", store.savedOIDC.ClientSecret)
	}
	if len(store.savedOIDC.RedirectURLs) != 1 || store.savedOIDC.RedirectURLs[0] != "https://app.example.test/auth/oidc/callback" {
		t.Fatalf("saved redirect URLs = %v", store.savedOIDC.RedirectURLs)
	}
	if len(store.savedOIDC.Scopes) != 2 {
		t.Fatalf("saved scopes = %v", store.savedOIDC.Scopes)
	}
	if !authService.OIDCEnabled() {
		t.Fatal("expected OIDC service to be active after save")
	}
	if builtConfig.Label != "Company SSO" {
		t.Fatalf("built label = %q", builtConfig.Label)
	}
	data := decodeOIDCSettingsData(t, recorder)
	if data["source"] != "database" || data["client_secret"] != maskedSecret {
		t.Fatalf("data = %v", data)
	}
}

func TestUpdateOIDCRejectsInvalidInputBeforeWriting(t *testing.T) {
	cases := map[string]string{
		"plain-http-issuer": `{"enabled": true, "issuer": "http://idp.example.test/", "client_id": "c", "client_secret": "s"}`,
		"missing-secret":    `{"enabled": true, "issuer": "https://idp.example.test/", "client_id": "c"}`,
		"missing-issuer":    `{"enabled": true, "client_id": "c", "client_secret": "s"}`,
		"query-redirect":    `{"enabled": true, "issuer": "https://idp.example.test/", "client_id": "c", "client_secret": "s", "redirect_urls": ["https://app.example.test/cb?x=1"]}`,
		"long-label":        `{"enabled": true, "issuer": "https://idp.example.test/", "client_id": "c", "client_secret": "s", "label": "` + strings.Repeat("x", 65) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeAuthSettingsStore{}
			handler, _ := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{})
			recorder := performOIDCSettingsRequest(t, oidcSettingsTestRouter(handler), http.MethodPut, "/api/v1/admin/settings/oidc", body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if store.savedOIDC != nil {
				t.Fatal("invalid settings must not be saved")
			}
		})
	}
}

func TestDeleteOIDCRestoresDeploymentSettings(t *testing.T) {
	stored := authsettings.OIDC{Enabled: true, Issuer: "https://stored.example.test/", ClientID: "stored-client"}
	store := &fakeAuthSettingsStore{oidc: &stored}
	handler, authService := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{})
	authService.SetOIDCService(&auth.OIDCService{})

	recorder := performOIDCSettingsRequest(t, oidcSettingsTestRouter(handler), http.MethodDelete, "/api/v1/admin/settings/oidc", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !store.deletedOIDC {
		t.Fatal("expected stored OIDC settings to be deleted")
	}
	if authService.OIDCEnabled() {
		t.Fatal("expected deployment (disabled) OIDC configuration to be restored")
	}
	data := decodeOIDCSettingsData(t, recorder)
	if data["source"] != "environment" || data["enabled"] != false {
		t.Fatalf("data = %v", data)
	}
}

func TestTestOIDCReturnsDiscoverySummary(t *testing.T) {
	store := &fakeAuthSettingsStore{}
	handler, _ := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{})
	body := `{"issuer": "https://idp.example.test/", "client_id": "c", "client_secret": "s"}`
	recorder := performOIDCSettingsRequest(t, oidcSettingsTestRouter(handler), http.MethodPost, "/api/v1/admin/settings/oidc/test", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.savedOIDC != nil {
		t.Fatal("test must not persist settings")
	}
}

func TestRefreshOIDCReloadsDatabaseChanges(t *testing.T) {
	store := &fakeAuthSettingsStore{}
	handler, authService := newOIDCSettingsTestHandler(t, store, config.OIDCConfig{})
	builds := 0
	handler.buildOIDC = func(context.Context, auth.OIDCConfig) (*auth.OIDCService, error) {
		builds++
		return &auth.OIDCService{}, nil
	}
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stored := authsettings.OIDC{Enabled: true, Issuer: "https://idp.example.test/", ClientID: "first", UpdatedAt: first}
	store.oidc = &stored

	handler.oidcRefreshAt = time.Time{}
	handler.RefreshOIDC(context.Background())
	if builds != 1 || !authService.OIDCEnabled() {
		t.Fatalf("builds = %d, enabled = %v", builds, authService.OIDCEnabled())
	}

	// A second refresh inside the throttle window does not rebuild.
	handler.RefreshOIDC(context.Background())
	if builds != 1 {
		t.Fatalf("builds = %d, want throttled build count 1", builds)
	}

	// Another replica writes a new row; the next interval reloads it.
	second := stored
	second.ClientID = "second"
	second.UpdatedAt = first.Add(time.Minute)
	store.oidc = &second
	handler.oidcRefreshAt = time.Time{}
	handler.RefreshOIDC(context.Background())
	if builds != 2 {
		t.Fatalf("builds = %d, want 2", builds)
	}

	// Dropping the row restores deployment configuration.
	store.oidc = nil
	handler.oidcRefreshAt = time.Time{}
	handler.RefreshOIDC(context.Background())
	if builds != 2 {
		t.Fatalf("builds = %d, want 2 (disabled deployment configuration)", builds)
	}
	if authService.OIDCEnabled() {
		t.Fatal("expected OIDC to be disabled after the database row is removed")
	}
}
