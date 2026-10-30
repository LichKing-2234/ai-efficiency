package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/opsmonitoring"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
)

type opsMonitoringProviderStub struct {
	relay.Provider
	groups    []relay.Group
	overviews map[int64]json.RawMessage

	updateCalls        int
	updatedAccountID   int64
	updatedConcurrency int
	updateErr          error
}

func (p *opsMonitoringProviderStub) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return p.groups, nil
}

func (p *opsMonitoringProviderStub) ReadGroupOpsHealth(_ context.Context, groupID int64, _, _ time.Time) (*relay.GroupOpsHealth, error) {
	return &relay.GroupOpsHealth{GroupID: groupID, Overview: p.overviews[groupID]}, nil
}

func (p *opsMonitoringProviderStub) ListGroupAccounts(_ context.Context, _ int64) ([]relay.GroupAccount, error) {
	proxyID := int64(3)
	return []relay.GroupAccount{{
		AccountID:   50,
		Name:        "alice@example.com",
		Platform:    "openai",
		Type:        "oauth",
		Status:      "active",
		Schedulable: true,
		ProxyID:     &proxyID,
		Proxy:       &relay.GroupProxy{ID: 3, Name: "proxy-alpha", Protocol: "socks5h", Host: "198.51.100.7", Port: 1080, Status: "active"},
	}}, nil
}

func (p *opsMonitoringProviderStub) UpdateAccountConcurrency(_ context.Context, accountID int64, concurrency int) (*relay.AccountCapacity, error) {
	p.updateCalls++
	p.updatedAccountID = accountID
	p.updatedConcurrency = concurrency
	if p.updateErr != nil {
		return nil, p.updateErr
	}
	return &relay.AccountCapacity{AccountID: accountID, Concurrency: concurrency, CurrentConcurrency: 2}, nil
}

func (p *opsMonitoringProviderStub) TestProxy(_ context.Context, proxyID int64) (*relay.ProxyEgressCheck, error) {
	return &relay.ProxyEgressCheck{
		Success:     true,
		Message:     "Proxy is accessible",
		LatencyMS:   203,
		IPAddress:   "198.51.100.7",
		City:        "Example City",
		Country:     "Example Country",
		CountryCode: "US",
	}, nil
}

type opsMonitoringResolverStub struct {
	provider relay.Provider
	err      error
}

func (r opsMonitoringResolverStub) Resolve(context.Context, int) (relay.Provider, error) {
	return r.provider, r.err
}

// opsMonitoringReaderOnlyProvider exposes the ops membership read but not the
// account concurrency updater capability.
type opsMonitoringReaderOnlyProvider struct {
	relay.Provider
	accounts []relay.GroupAccount
}

func (p *opsMonitoringReaderOnlyProvider) ReadGroupOpsHealth(_ context.Context, groupID int64, _, _ time.Time) (*relay.GroupOpsHealth, error) {
	return &relay.GroupOpsHealth{GroupID: groupID}, nil
}

func (p *opsMonitoringReaderOnlyProvider) ListGroupAccounts(context.Context, int64) ([]relay.GroupAccount, error) {
	return p.accounts, nil
}

func (p *opsMonitoringReaderOnlyProvider) TestProxy(context.Context, int64) (*relay.ProxyEgressCheck, error) {
	return &relay.ProxyEgressCheck{}, nil
}

func newOpsMonitoringTestRouter(t *testing.T, provider relay.Provider) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	opsHandler := NewOpsMonitoringHandler(opsmonitoring.NewService(opsMonitoringResolverStub{provider: provider}, nil))
	group := engine.Group("/api/v1/admin/ops-monitoring")
	group.GET("/groups", opsHandler.ListGroups)
	group.GET("/groups/:group_id/accounts", opsHandler.ListGroupAccounts)
	group.PUT("/groups/:group_id/accounts/:account_id/concurrency", opsHandler.UpdateAccountConcurrency)
	group.POST("/proxies/:proxy_id/test", opsHandler.TestProxy)
	return engine
}

func newOpsMonitoringStubProvider() *opsMonitoringProviderStub {
	return &opsMonitoringProviderStub{
		groups: []relay.Group{{ID: 7, Name: "Group Alpha", Platform: "openai", AccountCount: 12, ActiveAccountCount: 11}},
		overviews: map[int64]json.RawMessage{
			7: json.RawMessage(`{"health_score":10,"upstream_error_rate":0.5,"request_count_total":100}`),
		},
	}
}

func opsMonitoringJSON(t *testing.T, engine *gin.Engine, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	var decoded map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode %s %s response: %v (%s)", method, target, err, recorder.Body.String())
		}
	}
	return recorder, decoded
}

func opsMonitoringGet(t *testing.T, engine *gin.Engine, method, target string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	engine.ServeHTTP(recorder, request)
	var body map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s %s response: %v (%s)", method, target, err, recorder.Body.String())
		}
	}
	return recorder, body
}

func TestOpsMonitoringGroupsRequiresProviderID(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, _ := opsMonitoringGet(t, engine, http.MethodGet, "/api/v1/admin/ops-monitoring/groups")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOpsMonitoringGroupsRejectsUnsupportedWindow(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, _ := opsMonitoringGet(t, engine, http.MethodGet, "/api/v1/admin/ops-monitoring/groups?provider_id=1&window=3h")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOpsMonitoringGroupsReturnsVerbatimOverview(t *testing.T) {
	provider := newOpsMonitoringStubProvider()
	engine := newOpsMonitoringTestRouter(t, provider)
	recorder, body := opsMonitoringGet(t, engine, http.MethodGet, "/api/v1/admin/ops-monitoring/groups?provider_id=1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}

	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %T", body["data"])
	}
	if data["window"] != "1h" {
		t.Fatalf("window = %v, want 1h", data["window"])
	}
	groups, ok := data["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("groups = %#v", data["groups"])
	}
	row := groups[0].(map[string]any)
	if row["group_id"].(float64) != 7 || row["status"] != "ok" {
		t.Fatalf("row = %#v", row)
	}
	overview, ok := row["overview"].(map[string]any)
	if !ok {
		t.Fatalf("overview = %#v", row["overview"])
	}
	if overview["health_score"].(float64) != 10 || overview["request_count_total"].(float64) != 100 {
		t.Fatalf("overview = %#v", overview)
	}
	if row["active_account_count"].(float64) != 11 {
		t.Fatalf("active_account_count = %v", row["active_account_count"])
	}
}

func TestOpsMonitoringGroupAccountsRejectsInvalidGroupID(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, _ := opsMonitoringGet(t, engine, http.MethodGet, "/api/v1/admin/ops-monitoring/groups/0/accounts?provider_id=1")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOpsMonitoringGroupAccountsReturnsProxyBinding(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, body := opsMonitoringGet(t, engine, http.MethodGet, "/api/v1/admin/ops-monitoring/groups/7/accounts?provider_id=1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}
	data := body["data"].(map[string]any)
	accounts := data["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %#v", accounts)
	}
	account := accounts[0].(map[string]any)
	proxy, ok := account["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("account proxy = %#v", account["proxy"])
	}
	if proxy["host"] != "198.51.100.7" || proxy["protocol"] != "socks5h" {
		t.Fatalf("proxy = %#v", proxy)
	}
}

func TestOpsMonitoringProxyTestReturnsMeasuredEgress(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, body := opsMonitoringGet(t, engine, http.MethodPost, "/api/v1/admin/ops-monitoring/proxies/3/test?provider_id=1")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}
	data := body["data"].(map[string]any)
	if data["ip_address"] != "198.51.100.7" || data["country_code"] != "US" {
		t.Fatalf("egress = %#v", data)
	}
	if data["cached"].(bool) {
		t.Fatalf("uncached probe reported cached: %#v", data)
	}
}

func TestOpsMonitoringProxyTestRejectsInvalidProxyID(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())
	recorder, _ := opsMonitoringGet(t, engine, http.MethodPost, "/api/v1/admin/ops-monitoring/proxies/0/test?provider_id=1")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestOpsMonitoringAcceptsExplicitForceOnBothReadEndpoints(t *testing.T) {
	engine := newOpsMonitoringTestRouter(t, newOpsMonitoringStubProvider())

	for _, path := range []string{
		"/api/v1/admin/ops-monitoring/groups?provider_id=1&force=true",
		"/api/v1/admin/ops-monitoring/groups?provider_id=1&force=1",
		"/api/v1/admin/ops-monitoring/groups?provider_id=1&force=yes",
	} {
		recorder, _ := opsMonitoringGet(t, engine, http.MethodGet, path)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (%s)", path, recorder.Code, recorder.Body.String())
		}
	}

	recorder, _ := opsMonitoringGet(t, engine, http.MethodPost, "/api/v1/admin/ops-monitoring/proxies/3/test?provider_id=1&force=true")
	if recorder.Code != http.StatusOK {
		t.Fatalf("forced proxy test status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestOpsMonitoringUpdateAccountConcurrencyReturnsVerifiedCapacity(t *testing.T) {
	provider := newOpsMonitoringStubProvider()
	engine := newOpsMonitoringTestRouter(t, provider)
	recorder, body := opsMonitoringJSON(t, engine, http.MethodPut, "/api/v1/admin/ops-monitoring/groups/7/accounts/50/concurrency?provider_id=1", `{"concurrency":20}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body.String())
	}
	if provider.updateCalls != 1 || provider.updatedAccountID != 50 || provider.updatedConcurrency != 20 {
		t.Fatalf("updater calls = %d, args = %d/%d; want 1, 50/20", provider.updateCalls, provider.updatedAccountID, provider.updatedConcurrency)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %#v", body["data"])
	}
	if data["account_id"].(float64) != 50 {
		t.Fatalf("account_id = %v, want 50", data["account_id"])
	}
	if data["concurrency"].(float64) != 20 || data["current_concurrency"].(float64) != 2 {
		t.Fatalf("capacity = %v/%v, want 20/2", data["concurrency"], data["current_concurrency"])
	}
}

func TestOpsMonitoringUpdateAccountConcurrencyRejectsMissingConcurrency(t *testing.T) {
	provider := newOpsMonitoringStubProvider()
	engine := newOpsMonitoringTestRouter(t, provider)
	recorder, _ := opsMonitoringJSON(t, engine, http.MethodPut, "/api/v1/admin/ops-monitoring/groups/7/accounts/50/concurrency?provider_id=1", `{}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body.String())
	}
	if provider.updateCalls != 0 {
		t.Fatalf("updater calls = %d, want 0 for a missing value", provider.updateCalls)
	}
}

func TestOpsMonitoringUpdateAccountConcurrencyRejectsOutOfRangeConcurrency(t *testing.T) {
	for _, body := range []string{`{"concurrency":0}`, `{"concurrency":10001}`} {
		provider := newOpsMonitoringStubProvider()
		engine := newOpsMonitoringTestRouter(t, provider)
		recorder, _ := opsMonitoringJSON(t, engine, http.MethodPut, "/api/v1/admin/ops-monitoring/groups/7/accounts/50/concurrency?provider_id=1", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400 (%s)", body, recorder.Code, recorder.Body.String())
		}
		if provider.updateCalls != 0 {
			t.Fatalf("body %s: updater calls = %d, want 0", body, provider.updateCalls)
		}
	}
}

func TestOpsMonitoringUpdateAccountConcurrencyRejectsInvalidIDs(t *testing.T) {
	for _, target := range []string{
		"/api/v1/admin/ops-monitoring/groups/0/accounts/50/concurrency?provider_id=1",
		"/api/v1/admin/ops-monitoring/groups/7/accounts/0/concurrency?provider_id=1",
	} {
		provider := newOpsMonitoringStubProvider()
		engine := newOpsMonitoringTestRouter(t, provider)
		recorder, _ := opsMonitoringJSON(t, engine, http.MethodPut, target, `{"concurrency":20}`)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (%s)", target, recorder.Code, recorder.Body.String())
		}
		if provider.updateCalls != 0 {
			t.Fatalf("%s: updater calls = %d, want 0", target, provider.updateCalls)
		}
	}
}

func TestOpsMonitoringUpdateAccountConcurrencyRejectsProviderWithoutUpdater(t *testing.T) {
	provider := &opsMonitoringReaderOnlyProvider{accounts: []relay.GroupAccount{{AccountID: 50, Concurrency: 5}}}
	engine := newOpsMonitoringTestRouter(t, provider)
	recorder, body := opsMonitoringJSON(t, engine, http.MethodPut, "/api/v1/admin/ops-monitoring/groups/7/accounts/50/concurrency?provider_id=1", `{"concurrency":20}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", recorder.Code, recorder.Body.String())
	}
	message, _ := body["message"].(string)
	if !strings.Contains(message, "does not expose account concurrency updates") {
		t.Fatalf("message = %q, want missing capability error", message)
	}
}
