package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/owneraccount"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const ownerAccountTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
const ownerAccountTestAdminKey = "test-admin-key"

// ownerAccountTestEnv is the route-level integration seam: real router, real Ent
// test database, real Relay runtime manager and an httptest fake Sub2API.
type ownerAccountTestEnv struct {
	client    *ent.Client
	sqlDB     *sql.DB
	router    *gin.Engine
	auth      *auth.Service
	service   *owneraccount.Service
	runtime   *relayruntime.Manager
	upstream  *ownerAccountUpstream
	provider  *ent.RelayProvider
	alice     *ent.User
	bob       *ent.User
	admin     *ent.User
	aliceAuth string
	bobAuth   string
	adminAuth string
}

// setupOwnerAccountEnv builds the full test environment. configure may adjust the
// service options (mutation switch, heartbeat timing). managerOptions lets a test
// swap the long-running client factory.
func setupOwnerAccountEnv(t *testing.T, configure func(*owneraccount.Options)) *ownerAccountTestEnv {
	t.Helper()
	return setupOwnerAccountEnvWithRuntime(t, configure, relayruntime.Options{})
}

func setupOwnerAccountEnvWithRuntime(t *testing.T, configure func(*owneraccount.Options), managerOptions relayruntime.Options) *ownerAccountTestEnv {
	t.Helper()
	ctx := context.Background()

	client, dsn := testdb.OpenWithDSN(t)
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open owner account database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	upstream := newOwnerAccountUpstream(t)
	logger := zap.NewNop()

	encryptedKey, err := encryptAESGCM(ownerAccountTestAdminKey, ownerAccountTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt relay admin key: %v", err)
	}
	provider := client.RelayProvider.Create().
		SetName("relay-primary").
		SetDisplayName("Primary Relay").
		SetBaseURL(upstream.server.URL).
		SetAdminAPIKey(encryptedKey).
		SetDefaultModel("gpt-5.4").
		SetEnabled(true).
		SetIsPrimary(true).
		SaveX(ctx)

	runtime, err := relayruntime.NewManager(client, ownerAccountTestEncryptionKey, logger, managerOptions)
	if err != nil {
		t.Fatalf("initialize relay runtime: %v", err)
	}

	options := owneraccount.Options{MutationsEnabled: true}
	if configure != nil {
		configure(&options)
	}
	service, err := owneraccount.NewService(client, sqlDB, runtime, logger, options)
	if err != nil {
		t.Fatalf("initialize owner account service: %v", err)
	}

	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoService := repo.NewService(client, ownerAccountTestEncryptionKey, logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	providerHandler := newProviderHandlerForTest(t, client, ownerAccountTestEncryptionKey, logger, runtime)
	router := setupRouterForTest(t,
		client,
		sqlDB,
		authService,
		repoService,
		webhookHandler,
		nil,
		nil,
		ownerAccountTestEncryptionKey,
		"",
		middleware.CORS(nil),
		nil, providerHandler, nil, nil,
		nil,
		RouterOptions{OwnerAccounts: service},
	)

	env := &ownerAccountTestEnv{
		client: client, sqlDB: sqlDB, router: router, auth: authService,
		service: service, runtime: runtime, upstream: upstream, provider: provider,
	}
	env.alice = createOwnerAccountUser(t, client, "alice", "alice@example.com", "user")
	env.bob = createOwnerAccountUser(t, client, "bob", "bob@example.org", "user")
	env.admin = createOwnerAccountUser(t, client, "admin", "admin@example.com", "admin")
	env.aliceAuth = ownerAccountToken(t, authService, env.alice)
	env.bobAuth = ownerAccountToken(t, authService, env.bob)
	env.adminAuth = ownerAccountToken(t, authService, env.admin)
	return env
}

func createOwnerAccountUser(t *testing.T, client *ent.Client, username, email, role string) *ent.User {
	t.Helper()
	user, err := client.User.Create().
		SetUsername(username).
		SetEmail(email).
		SetAuthSource("sub2api_sso").
		SetRole(entuser.Role(role)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create owner account test user: %v", err)
	}
	return user
}

func ownerAccountToken(t *testing.T, authService *auth.Service, user *ent.User) string {
	t.Helper()
	pair, err := authService.GenerateTokenPairForUser(&auth.UserInfo{
		ID: user.ID, Username: user.Username, Role: string(user.Role),
	})
	if err != nil {
		t.Fatalf("generate owner account token: %v", err)
	}
	return pair.AccessToken
}

func (e *ownerAccountTestEnv) request(method, path string, body any, token string) *httptest.ResponseRecorder {
	var payload *bytes.Buffer
	if body != nil {
		encoded, _ := json.Marshal(body)
		payload = bytes.NewBuffer(encoded)
	} else {
		payload = bytes.NewBuffer(nil)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, payload)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	e.router.ServeHTTP(w, req)
	return w
}

func (e *ownerAccountTestEnv) data(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	response := parseResponse(t, w)
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data is not an object: %s", w.Body.String())
	}
	return data
}

func (e *ownerAccountTestEnv) errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	response := parseResponse(t, w)
	details, ok := response["details"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := details["error_code"].(string)
	return code
}

func (e *ownerAccountTestEnv) errorDetails(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	response := parseResponse(t, w)
	details, _ := response["details"].(map[string]any)
	return details
}

// seedOperation writes one operation row directly so database-time behaviors can
// be tested by seeding old timestamps instead of sleeping.
type ownerAccountSeed struct {
	AccountID   int64
	Action      owneraccountoperation.Action
	Status      owneraccountoperation.Status
	CreatedAt   time.Time
	HeartbeatAt *time.Time
	FinishedAt  *time.Time
	Upstream    string
	InviteEmail string
	Result      map[string]any
}

func (e *ownerAccountTestEnv) seedOperation(t *testing.T, actorID int, seed ownerAccountSeed) *ent.OwnerAccountOperation {
	t.Helper()
	builder := e.client.OwnerAccountOperation.Create().
		SetProviderID(e.provider.ID).
		SetAccountID(seed.AccountID).
		SetActorUserID(actorID).
		SetAction(seed.Action).
		SetStatus(seed.Status).
		SetCreatedAt(seed.CreatedAt).
		SetUpdatedAt(seed.CreatedAt).
		SetStartedAt(seed.CreatedAt)
	if seed.HeartbeatAt != nil {
		builder = builder.SetHeartbeatAt(*seed.HeartbeatAt)
	}
	if seed.FinishedAt != nil {
		builder = builder.SetFinishedAt(*seed.FinishedAt)
	}
	if seed.Upstream != "" {
		builder = builder.SetUpstreamCode(seed.Upstream)
	}
	if seed.InviteEmail != "" {
		builder = builder.SetInviteEmail(seed.InviteEmail)
	}
	if seed.Result != nil {
		builder = builder.SetResult(seed.Result)
	}
	row, err := builder.Save(context.Background())
	if err != nil {
		t.Fatalf("seed owner operation: %v", err)
	}
	return row
}

func (e *ownerAccountTestEnv) backdateRefreshes(t *testing.T, age time.Duration) {
	t.Helper()
	if _, err := e.sqlDB.Exec(`
		UPDATE owner_account_operations
		SET created_at = now() - $1::interval, updated_at = now() - $1::interval
		WHERE action IN ('refresh_quota', 'refresh_referral')`,
		ownerAccountInterval(age)); err != nil {
		t.Fatalf("backdate refresh operations: %v", err)
	}
}

func (e *ownerAccountTestEnv) waitForOperation(t *testing.T, token string, operationID int, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		w := e.request(http.MethodGet, "/api/v1/user/owner-accounts/operations/"+strconv.Itoa(operationID), nil, token)
		if w.Code != http.StatusOK {
			t.Fatalf("poll operation status = %d, body: %s", w.Code, w.Body.String())
		}
		last = e.data(t, w)
		if last["status"] == want {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("operation %d never reached %q, last: %#v", operationID, want, last)
	return nil
}

func ownerAccountInterval(duration time.Duration) string {
	return strconv.FormatInt(duration.Milliseconds(), 10) + " milliseconds"
}

// ownerAccountUpstream is an httptest Sub2API implementing exactly the endpoints
// the owner-account capabilities and the owner-safe directory read call.
type ownerAccountUpstream struct {
	t      *testing.T
	server *httptest.Server

	mu           sync.Mutex
	accounts     []map[string]any
	usage        map[int64]map[string]any
	usageErrors  map[int64]string
	quotaPayload map[int64]map[string]any
	quotaStatus  map[int64]int
	resetPayload map[int64]map[string]any
	resetStatus  map[int64]int
	referralData map[int64]map[string]any
	referralCode map[int64]int
	inviteStatus int
	inviteData   map[string]any

	resetBlock  chan struct{}
	resetGate   chan struct{}
	resetCalls  int
	inviteCalls int
	calls       []string
	usageBatch  [][]int64
	usageForce  []bool
}

func newOwnerAccountUpstream(t *testing.T) *ownerAccountUpstream {
	t.Helper()
	upstream := &ownerAccountUpstream{
		t:            t,
		usage:        map[int64]map[string]any{},
		usageErrors:  map[int64]string{},
		quotaPayload: map[int64]map[string]any{},
		quotaStatus:  map[int64]int{},
		resetPayload: map[int64]map[string]any{},
		resetStatus:  map[int64]int{},
		referralData: map[int64]map[string]any{},
		referralCode: map[int64]int{},
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.handle))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *ownerAccountUpstream) handle(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.calls = append(u.calls, r.Method+" "+r.URL.Path)
	u.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/accounts":
		u.handleAccounts(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/accounts/usage/batch":
		u.handleUsageBatch(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/admin/openai/accounts/"):
		u.handleOpenAIAccount(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (u *ownerAccountUpstream) handleAccounts(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	u.mu.Lock()
	items := make([]map[string]any, 0, len(u.accounts))
	for _, account := range u.accounts {
		if platform != "" && account["platform"] != platform {
			continue
		}
		items = append(items, account)
	}
	u.mu.Unlock()
	writeOwnerAccountEnvelope(w, http.StatusOK, map[string]any{"items": items, "pages": 1})
}

func (u *ownerAccountUpstream) handleUsageBatch(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AccountIDs []int64 `json:"account_ids"`
		Force      bool    `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "decode", http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.usageBatch = append(u.usageBatch, append([]int64(nil), request.AccountIDs...))
	u.usageForce = append(u.usageForce, request.Force)
	usage := make(map[string]any, len(request.AccountIDs))
	errs := map[string]string{}
	for _, accountID := range request.AccountIDs {
		key := strconv.FormatInt(accountID, 10)
		if message, ok := u.usageErrors[accountID]; ok && message != "" {
			errs[key] = message
			continue
		}
		if payload, ok := u.usage[accountID]; ok {
			usage[key] = payload
			continue
		}
		usage[key] = ownerAccountUsagePayload(int64(10 + accountID%50))
	}
	u.mu.Unlock()
	writeOwnerAccountEnvelope(w, http.StatusOK, map[string]any{"usage": usage, "errors": errs})
}

func (u *ownerAccountUpstream) handleOpenAIAccount(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/openai/accounts/")
	parts := strings.SplitN(rest, "/", 2)
	accountID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	u.mu.Lock()
	switch parts[1] {
	case "quota/refresh":
		if status := u.quotaStatus[accountID]; status != 0 && status != http.StatusOK {
			u.mu.Unlock()
			writeOwnerAccountStatus(w, status)
			return
		}
		payload, ok := u.quotaPayload[accountID]
		if !ok {
			payload = ownerAccountQuotaPayload(1, "2999-01-01T00:00:00Z")
		}
		u.mu.Unlock()
		writeOwnerAccountEnvelope(w, http.StatusOK, payload)
	case "reset-quota":
		u.resetCalls++
		block := u.resetBlock
		gate := u.resetGate
		status := u.resetStatus[accountID]
		payload, ok := u.resetPayload[accountID]
		if !ok {
			payload = ownerAccountResetPayload(2)
		}
		u.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-time.After(10 * time.Second):
			}
		}
		if gate != nil {
			select {
			case <-gate:
			case <-time.After(10 * time.Second):
			}
		}
		if status != 0 && status != http.StatusOK {
			writeOwnerAccountStatus(w, status)
			return
		}
		writeOwnerAccountEnvelope(w, http.StatusOK, payload)
	case "referrals/refresh":
		if status := u.referralCode[accountID]; status != 0 && status != http.StatusOK {
			u.mu.Unlock()
			writeOwnerAccountStatus(w, status)
			return
		}
		referral := u.referralData[accountID]
		if referral == nil {
			referral = ownerAccountReferralSnapshot("program-1", 3, true)
		}
		u.mu.Unlock()
		writeOwnerAccountEnvelope(w, http.StatusOK, map[string]any{"eligibility": referral})
	case "referrals/invite":
		u.inviteCalls++
		status := u.inviteStatus
		data := u.inviteData
		u.mu.Unlock()
		if data == nil {
			data = map[string]any{
				"email":       "bob@example.org",
				"sent":        true,
				"eligibility": ownerAccountReferralSnapshot("program-1", 2, true),
			}
		}
		if status != 0 && status != http.StatusOK {
			if status == http.StatusBadGateway {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			writeOwnerAccountStatus(w, status)
			return
		}
		writeOwnerAccountEnvelope(w, http.StatusOK, data)
	default:
		u.mu.Unlock()
		http.NotFound(w, r)
	}
}

func writeOwnerAccountEnvelope(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func writeOwnerAccountStatus(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	if status == http.StatusOK {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "message": "upstream failure"})
}

func (u *ownerAccountUpstream) setAccounts(accounts ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accounts = accounts
}

func (u *ownerAccountUpstream) addAccounts(accounts ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accounts = append(u.accounts, accounts...)
}

func (u *ownerAccountUpstream) setUsage(accountID int64, payload map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.usage[accountID] = payload
}

func (u *ownerAccountUpstream) setUsageError(accountID int64, message string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.usageErrors[accountID] = message
}

func (u *ownerAccountUpstream) setQuota(accountID int64, payload map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.quotaPayload[accountID] = payload
}

func (u *ownerAccountUpstream) setQuotaStatus(accountID int64, status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.quotaStatus[accountID] = status
}

func (u *ownerAccountUpstream) setReset(accountID int64, payload map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetPayload[accountID] = payload
}

func (u *ownerAccountUpstream) setResetStatus(accountID int64, status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetStatus[accountID] = status
}

func (u *ownerAccountUpstream) setReferral(accountID int64, payload map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.referralData[accountID] = payload
}

func (u *ownerAccountUpstream) setReferralStatus(accountID int64, status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.referralCode[accountID] = status
}

func (u *ownerAccountUpstream) setInviteStatus(status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inviteStatus = status
}

func (u *ownerAccountUpstream) setInviteData(data map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.inviteData = data
}

func (u *ownerAccountUpstream) blockReset(release chan struct{}) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetBlock = release
}

func (u *ownerAccountUpstream) gatedReset(gate chan struct{}) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetGate = gate
}

func (u *ownerAccountUpstream) snapshot() ([]string, [][]int64, []bool, int, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	calls := append([]string(nil), u.calls...)
	batches := make([][]int64, len(u.usageBatch))
	for index := range u.usageBatch {
		batches[index] = append([]int64(nil), u.usageBatch[index]...)
	}
	forces := append([]bool(nil), u.usageForce...)
	return calls, batches, forces, u.resetCalls, u.inviteCalls
}

func (u *ownerAccountUpstream) resetCallCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.resetCalls
}

// ownerAccountWire builds one owner-safe directory account. The injected secret
// fields prove the HTTP surface never forwards them.
func ownerAccountWire(accountID int64, name, email, platform, accountType string) map[string]any {
	return map[string]any{
		"id":                  accountID,
		"name":                name,
		"platform":            platform,
		"type":                accountType,
		"status":              "active",
		"schedulable":         true,
		"parent_account_id":   nil,
		"rate_limit_reset_at": "2026-10-23T20:00:00Z",
		"groups": []any{
			map[string]any{"id": 5, "name": "Group Alpha"},
		},
		"account_groups": []any{
			map[string]any{"group_id": 5, "priority": 1, "group": map[string]any{"id": 5, "name": "Group Alpha"}},
		},
		"credentials": map[string]any{
			"email":         email,
			"access_token":  "must-not-surface-credential",
			"refresh_token": "must-not-surface-credential",
		},
		"extra": map[string]any{
			"codex_reset_credit_snapshot": map[string]any{
				"available_count": 1,
				"credits": []any{
					map[string]any{"expires_at": "2999-01-01T00:00:00Z"},
				},
			},
			"codex_credits_snapshot": map[string]any{
				"credits":    map[string]any{"has_credits": true, "unlimited": false, "balance": "12.5"},
				"fetched_at": 1760000000,
			},
			"codex_referral_snapshot": map[string]any{
				"should_show":                    true,
				"program_id":                     "program-1",
				"available_invites":              3,
				"requires_explicit_confirmation": true,
				"fetched_at":                     1760000000,
			},
			"codex_usage_updated_at":    "2026-10-23T04:00:00Z",
			"auto_reset_credit_enabled": true,
			"fingerprint_seed":          "must-not-surface-fingerprint",
		},
		"notes":    "must-not-surface-notes",
		"proxy_id": 4242,
		"proxy":    map[string]any{"url": "must-not-surface-proxy"},
	}
}

func ownerAccountWithPlatform(account map[string]any, platform string) map[string]any {
	account["platform"] = platform
	return account
}

func ownerAccountWithType(account map[string]any, accountType string) map[string]any {
	account["type"] = accountType
	return account
}

func ownerAccountShadow(account map[string]any, parentID int64) map[string]any {
	account["parent_account_id"] = parentID
	return account
}

func ownerAccountResetCredits(account map[string]any, count int, expiry string) map[string]any {
	credits := make([]any, 0, count)
	for index := 0; index < count; index++ {
		credits = append(credits, map[string]any{"expires_at": expiry})
	}
	extra := account["extra"].(map[string]any)
	extra["codex_reset_credit_snapshot"] = map[string]any{
		"available_count": count,
		"credits":         credits,
	}
	return account
}

func ownerAccountReferral(account map[string]any, programID string, invites int) map[string]any {
	extra := account["extra"].(map[string]any)
	extra["codex_referral_snapshot"] = ownerAccountReferralSnapshot(programID, invites, true)
	return account
}

func ownerAccountCreditsBalance(account map[string]any, balance string, unlimited bool) map[string]any {
	extra := account["extra"].(map[string]any)
	extra["codex_credits_snapshot"] = map[string]any{
		"credits":    map[string]any{"has_credits": !unlimited, "unlimited": unlimited, "balance": balance},
		"fetched_at": 1760000000,
	}
	return account
}

func ownerAccountReferralSnapshot(programID string, invites int, consent bool) map[string]any {
	return map[string]any{
		"should_show":                    true,
		"program_id":                     programID,
		"available_invites":              invites,
		"requires_explicit_confirmation": consent,
		"fetched_at":                     time.Now().Unix(),
	}
}

func ownerAccountQuotaPayload(credits int, expiry string) map[string]any {
	creditList := make([]any, 0, credits)
	for index := 0; index < credits; index++ {
		creditList = append(creditList, map[string]any{"expires_at": expiry})
	}
	return map[string]any{
		"plan_type": "plus",
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"used_percent": 42.5, "limit_window_seconds": 18000, "reset_at": time.Now().Add(2 * time.Hour).Unix(),
			},
			"secondary_window": map[string]any{
				"used_percent": 77.0, "limit_window_seconds": 604800, "reset_at": time.Now().Add(48 * time.Hour).Unix(),
			},
		},
		"rate_limit_reset_credits": map[string]any{"available_count": credits, "credits": creditList},
		"credits":                  map[string]any{"has_credits": true, "unlimited": false, "balance": "9.5"},
		"fetched_at":               time.Now().Unix(),
	}
}

func ownerAccountResetPayload(windowsReset int) map[string]any {
	return map[string]any{
		"code":          "ok",
		"windows_reset": windowsReset,
		"warning_code":  "account_state_refresh_failed",
		"credit":        map[string]any{"expires_at": "2027-01-01T00:00:00Z"},
		"quota":         ownerAccountQuotaPayload(0, "2999-01-01T00:00:00Z"),
	}
}

func ownerAccountUsagePayload(utilization int64) map[string]any {
	return map[string]any{
		"updated_at":   "2026-10-23T04:00:00Z",
		"needs_reauth": true,
		"is_banned":    false,
		"is_forbidden": false,
		"five_hour": map[string]any{
			"utilization": float64(utilization),
			"resets_at":   "2026-10-23T09:00:00Z",
			"window_stats": map[string]any{
				"requests": 3, "tokens": 1200, "cost": 0.75, "standard_cost": 1.25, "user_cost": 0.5,
			},
		},
		"seven_day": map[string]any{
			"utilization": 62.5,
			"resets_at":   "2026-10-29T09:00:00Z",
		},
	}
}

// ownerAccountCapability is a provider wrapper used to make the async worker
// panic, which must become an unknown operation rather than a crash.
type ownerAccountCapability struct {
	relay.Provider
	inner        relay.Provider
	panicOnReset bool
}

func (p *ownerAccountCapability) ListOwnerAccounts(ctx context.Context, platform string) ([]relay.OwnerAccount, error) {
	return p.inner.(relay.OwnerAccountDirectoryReader).ListOwnerAccounts(ctx, platform)
}

func (p *ownerAccountCapability) ReadOwnerAccountUsage(ctx context.Context, accountIDs []int64, force bool) ([]relay.OwnerAccountUsage, error) {
	return p.inner.(relay.OwnerAccountUsageReader).ReadOwnerAccountUsage(ctx, accountIDs, force)
}

func (p *ownerAccountCapability) RefreshOpenAIQuota(ctx context.Context, accountID int64) (*relay.OpenAIQuota, error) {
	return p.inner.(relay.OpenAIQuotaManager).RefreshOpenAIQuota(ctx, accountID)
}

func (p *ownerAccountCapability) ConsumeOpenAIResetCredit(ctx context.Context, accountID int64) (*relay.OpenAIResetCreditResult, error) {
	if p.panicOnReset {
		panic("owner account worker panic")
	}
	return p.inner.(relay.OpenAIQuotaManager).ConsumeOpenAIResetCredit(ctx, accountID)
}

func (p *ownerAccountCapability) RefreshOpenAIReferral(ctx context.Context, accountID int64) (*relay.OpenAIReferral, error) {
	return p.inner.(relay.OpenAIReferralManager).RefreshOpenAIReferral(ctx, accountID)
}

func (p *ownerAccountCapability) SendOpenAIReferralInvite(ctx context.Context, accountID int64, request relay.OpenAIReferralInviteRequest) (*relay.OpenAIReferralInviteResult, error) {
	return p.inner.(relay.OpenAIReferralManager).SendOpenAIReferralInvite(ctx, accountID, request)
}
