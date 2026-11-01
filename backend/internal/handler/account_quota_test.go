package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/accountquota"
	"github.com/gin-gonic/gin"
)

// fakeAccountQuotaService records calls and returns canned service outcomes.
type fakeAccountQuotaService struct {
	listFn   func(ctx context.Context, providerID int, platform string) (*accountquota.Page, error)
	startFn  func(ctx context.Context, request accountquota.StartSweepRequest) (*accountquota.Sweep, error)
	getFn    func(ctx context.Context, id int) (*accountquota.Sweep, error)
	latestFn func(ctx context.Context, providerID int) (*accountquota.Sweep, error)

	started *accountquota.StartSweepRequest
}

func (s *fakeAccountQuotaService) List(ctx context.Context, providerID int, platform string) (*accountquota.Page, error) {
	if s.listFn == nil {
		return &accountquota.Page{ProviderID: providerID, Platform: platform}, nil
	}
	return s.listFn(ctx, providerID, platform)
}

func (s *fakeAccountQuotaService) StartSweep(ctx context.Context, request accountquota.StartSweepRequest) (*accountquota.Sweep, error) {
	s.started = &request
	if s.startFn == nil {
		return &accountquota.Sweep{ID: 1, Status: accountquota.SweepQueued, ProviderID: request.ProviderID, Platform: request.Platform}, nil
	}
	return s.startFn(ctx, request)
}

func (s *fakeAccountQuotaService) GetSweep(ctx context.Context, id int) (*accountquota.Sweep, error) {
	if s.getFn == nil {
		return &accountquota.Sweep{ID: id, Status: accountquota.SweepCompleted}, nil
	}
	return s.getFn(ctx, id)
}

func (s *fakeAccountQuotaService) LatestSweep(ctx context.Context, providerID int) (*accountquota.Sweep, error) {
	if s.latestFn == nil {
		return nil, nil
	}
	return s.latestFn(ctx, providerID)
}

func newAccountQuotaTestRouter(service accountQuotaService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountQuotaHandler(service)
	group := router.Group("/api/v1/admin/account-quotas")
	group.GET("", handler.List)
	group.POST("/sweeps", handler.StartSweep)
	group.GET("/sweeps/latest", handler.LatestSweep)
	group.GET("/sweeps/:id", handler.GetSweep)
	return router
}

func doAccountQuotaRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeAccountQuotaEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return envelope
}

func TestAccountQuotaListPassesScope(t *testing.T) {
	var gotProviderID int
	var gotPlatform string
	service := &fakeAccountQuotaService{
		listFn: func(_ context.Context, providerID int, platform string) (*accountquota.Page, error) {
			gotProviderID, gotPlatform = providerID, platform
			return &accountquota.Page{
				ProviderID: providerID,
				Platform:   platform,
				Total:      2,
				Items: []accountquota.Item{
					{Account: accountquota.AccountQuotaAccount{ID: 1, Name: "alice"}},
				},
			}, nil
		},
	}
	router := newAccountQuotaTestRouter(service)
	recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas?provider_id=3&platform=openai", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if gotProviderID != 3 || gotPlatform != accountquota.PlatformOpenAI {
		t.Fatalf("service got provider %d platform %q", gotProviderID, gotPlatform)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	data, _ := envelope["data"].(map[string]any)
	if data["total"] != float64(2) {
		t.Fatalf("data.total = %v, want 2", data["total"])
	}
}

func TestAccountQuotaListRequiresProviderID(t *testing.T) {
	router := newAccountQuotaTestRouter(&fakeAccountQuotaService{})
	recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas", "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	details, _ := envelope["details"].(map[string]any)
	if details["error_code"] != accountquota.CodeInvalidRequest {
		t.Fatalf("error code = %v, want %s", details["error_code"], accountquota.CodeInvalidRequest)
	}
}

func TestAccountQuotaStartSweepAcceptsAndForwardsRequest(t *testing.T) {
	service := &fakeAccountQuotaService{}
	router := newAccountQuotaTestRouter(service)
	body := `{"provider_id":3,"platform":"openai","account_ids":[4,5],"include_auto_reset":true}`
	recorder := doAccountQuotaRequest(router, http.MethodPost, "/api/v1/admin/account-quotas/sweeps", body)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.started == nil {
		t.Fatalf("service was not called")
	}
	if service.started.ProviderID != 3 || len(service.started.AccountIDs) != 2 || service.started.AccountIDs[1] != 5 || !service.started.IncludeAutoReset {
		t.Fatalf("request = %#v", service.started)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	data, _ := envelope["data"].(map[string]any)
	if data["status"] != accountquota.SweepQueued {
		t.Fatalf("data.status = %v, want queued", data["status"])
	}
}

func TestAccountQuotaStartSweepRejectsInvalidBody(t *testing.T) {
	router := newAccountQuotaTestRouter(&fakeAccountQuotaService{})
	recorder := doAccountQuotaRequest(router, http.MethodPost, "/api/v1/admin/account-quotas/sweeps", "{")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAccountQuotaStartSweepMapsClassifiedError(t *testing.T) {
	service := &fakeAccountQuotaService{
		startFn: func(context.Context, accountquota.StartSweepRequest) (*accountquota.Sweep, error) {
			return nil, &accountquota.Error{
				Status:  http.StatusConflict,
				Code:    accountquota.CodeSweepTooSoon,
				Message: "account quota sweeps are rate limited",
				Details: map[string]any{"retry_after_seconds": 42},
			}
		},
	}
	router := newAccountQuotaTestRouter(service)
	recorder := doAccountQuotaRequest(router, http.MethodPost, "/api/v1/admin/account-quotas/sweeps", `{"provider_id":3}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	details, _ := envelope["details"].(map[string]any)
	if details["error_code"] != accountquota.CodeSweepTooSoon || details["retry_after_seconds"] != float64(42) {
		t.Fatalf("details = %#v", details)
	}
}

func TestAccountQuotaStartSweepHidesUnknownError(t *testing.T) {
	service := &fakeAccountQuotaService{
		startFn: func(context.Context, accountquota.StartSweepRequest) (*accountquota.Sweep, error) {
			return nil, errors.New("database exploded: secret detail")
		},
	}
	router := newAccountQuotaTestRouter(service)
	recorder := doAccountQuotaRequest(router, http.MethodPost, "/api/v1/admin/account-quotas/sweeps", `{"provider_id":3}`)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "secret detail") {
		t.Fatalf("internal error text leaked: %s", recorder.Body.String())
	}
}

func TestAccountQuotaGetSweepValidatesID(t *testing.T) {
	service := &fakeAccountQuotaService{
		getFn: func(_ context.Context, id int) (*accountquota.Sweep, error) {
			if id == 2 {
				return nil, &accountquota.Error{Status: http.StatusNotFound, Code: accountquota.CodeSweepNotFound, Message: "account quota sweep not found"}
			}
			return &accountquota.Sweep{ID: id, Status: accountquota.SweepCompleted, FinishedAt: ptrTime(time.Now().UTC())}, nil
		},
	}
	router := newAccountQuotaTestRouter(service)
	if recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas/sweeps/abc", ""); recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d, want 400", recorder.Code)
	}
	recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas/sweeps/2", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", recorder.Code)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	details, _ := envelope["details"].(map[string]any)
	if details["error_code"] != accountquota.CodeSweepNotFound {
		t.Fatalf("error code = %v", details["error_code"])
	}
	if recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas/sweeps/1", ""); recorder.Code != http.StatusOK {
		t.Fatalf("found status = %d, want 200", recorder.Code)
	}
}

func TestAccountQuotaLatestSweepOmitsDataWhenAbsent(t *testing.T) {
	router := newAccountQuotaTestRouter(&fakeAccountQuotaService{})
	recorder := doAccountQuotaRequest(router, http.MethodGet, "/api/v1/admin/account-quotas/sweeps/latest?provider_id=3", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	envelope := decodeAccountQuotaEnvelope(t, recorder)
	if _, present := envelope["data"]; present {
		t.Fatalf("empty latest sweep should omit data: %s", recorder.Body.String())
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
