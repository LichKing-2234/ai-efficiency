# OAuth Device Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OAuth 2.0 device authorization flow to `ae-cli login` while preserving the existing browser PKCE path and keeping login state in the existing `~/.ae-cli/token.json`.

**Architecture:** The backend extends the existing in-memory OAuth handler with short-lived device entries, a new `/oauth/device/code` endpoint, device-grant support in `/oauth/token`, a browser-facing `/oauth/device` page route, and an authenticated `/oauth/device/verify` action. `ae-cli` gains an explicit `--device` mode plus a small headless-environment check for the default browser path, while the frontend adds a dedicated device approval page that reuses existing web login and posts approve/deny decisions back to the backend. The implementation deliberately keeps device entries ephemeral and session/bootstrap behavior unchanged.

**Tech Stack:** Go (`Gin`, `net/http`, `cobra`, `httptest`), Vue 3 (`vue-router`, `Pinia`, `Vitest`), existing JWT/token plumbing

---

## Scope Check

This remains one plan. Backend OAuth state, CLI login selection, and the frontend device page are separate code paths, but they all implement one user-visible feature and share the same contract from [`2026-04-15-oauth-device-login-design.md`](../specs/2026-04-15-oauth-device-login-design.md).

## File Structure

| Action | File | Responsibility |
| --- | --- | --- |
| Modify | `backend/internal/oauth/handler.go` | Extend handler state, add device-code issue/verify/token handlers, and route-aware embedded-page serving |
| Create | `backend/internal/oauth/device_internal_test.go` | White-box device-flow tests that can manipulate handler clock/state |
| Modify | `backend/internal/oauth/handler_test.go` | Black-box handler tests for `/oauth/device` page routing and public HTTP behavior |
| Modify | `backend/internal/handler/router.go` | Register `/oauth/device`, `/oauth/device/code`, and `/oauth/device/verify` |
| Modify | `backend/internal/handler/router_frontend_test.go` | Confirm router serves the embedded frontend at `/oauth/device` |
| Modify | `ae-cli/internal/auth/oauth.go` | Keep browser PKCE flow but add shared config hooks for output and HTTP client |
| Create | `ae-cli/internal/auth/device.go` | Device code request, polling loop, OAuth error parsing, and headless-environment helper |
| Create | `ae-cli/internal/auth/device_test.go` | Device flow and headless helper tests |
| Modify | `ae-cli/cmd/login.go` | Add `--device`, select browser vs. device flow, and emit headless guidance |
| Modify | `ae-cli/cmd/login_test.go` | Command-level tests for `--device` and headless hinting |
| Modify | `frontend/src/api/oauth.ts` | Add device verify request/response helpers |
| Modify | `frontend/src/router/index.ts` | Register `/oauth/device` as a public route |
| Create | `frontend/src/views/oauth/DevicePage.vue` | Device login page for code entry and approve/deny actions |
| Create | `frontend/src/__tests__/oauth-device-page.test.ts` | Page-level device login UX tests |
| Modify | `frontend/src/__tests__/router.test.ts` | Verify the new route exists and public routing still behaves correctly |
| Modify | `docs/architecture.md` | Update current runtime login flow after implementation lands |
| Modify | `docs/superpowers/specs/2026-04-15-oauth-device-login-design.md` | Flip status/implementation note from planned to current once code is done |

### Task 1: Backend OAuth Device Grant

**Files:**
- Modify: `backend/internal/oauth/handler.go`
- Create: `backend/internal/oauth/device_internal_test.go`
- Modify: `backend/internal/oauth/handler_test.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/router_frontend_test.go`

- [ ] **Step 1: Write the failing backend device-flow tests**

Create `backend/internal/oauth/device_internal_test.go` with:

```go
package oauth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	authpkg "github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type deviceTokenGen struct{}

func (deviceTokenGen) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	return "device-access-token", "device-refresh-token", 7200, nil
}

func setupDeviceRouter(t *testing.T) (*gin.Engine, *Handler, *time.Time) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	handler := NewHandler(NewServer(), "http://localhost:5173", deviceTokenGen{})
	handler.now = func() time.Time { return now }
	handler.deviceCodeExpiry = 15 * time.Minute
	handler.devicePollInterval = 5 * time.Second

	r := gin.New()
	r.POST("/oauth/device/code", handler.DeviceCode)
	r.GET("/oauth/device", handler.DevicePage)
	r.POST("/oauth/token", handler.Token)
	verify := r.Group("/oauth")
	verify.Use(func(c *gin.Context) {
		c.Set(authpkg.ContextKeyUser, &authpkg.UserContext{
			UserID:   7,
			Username: "alice",
			Role:     "member",
		})
		c.Next()
	})
	verify.POST("/device/verify", handler.VerifyDevice)
	return r, handler, &now
}

func issueDeviceCode(t *testing.T, router *gin.Engine) (map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/device/code", strings.NewReader("client_id=ae-cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device code status=%d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode device code: %v", err)
	}
	return payload, payload["device_code"].(string)
}

func TestTokenDeviceGrantPendingApprovedConsumed(t *testing.T) {
	router, _, _ := setupDeviceRouter(t)
	payload, deviceCode := issueDeviceCode(t, router)

	tokenBody := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "authorization_pending") {
		t.Fatalf("pending body=%s", w.Body.String())
	}

	verifyBody := bytes.NewBufferString(`{"user_code":"` + strings.ToLower(payload["user_code"].(string)) + `","approved":true}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", verifyBody)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	router.ServeHTTP(verifyW, verifyReq)
	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyW.Code, verifyW.Body.String())
	}

	successW := httptest.NewRecorder()
	successReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	successReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(successW, successReq)
	if !strings.Contains(successW.Body.String(), "device-access-token") {
		t.Fatalf("success body=%s", successW.Body.String())
	}

	consumedW := httptest.NewRecorder()
	consumedReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenBody.Encode()))
	consumedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(consumedW, consumedReq)
	if !strings.Contains(consumedW.Body.String(), "invalid_grant") {
		t.Fatalf("consumed body=%s", consumedW.Body.String())
	}
}

func TestTokenDeviceGrantDeniedExpiredAndSlowDown(t *testing.T) {
	router, handler, now := setupDeviceRouter(t)
	payload, deviceCode := issueDeviceCode(t, router)

	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	firstReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	firstW := httptest.NewRecorder()
	router.ServeHTTP(firstW, firstReq)

	secondReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	secondReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondW := httptest.NewRecorder()
	router.ServeHTTP(secondW, secondReq)
	if !strings.Contains(secondW.Body.String(), "slow_down") {
		t.Fatalf("slow_down body=%s", secondW.Body.String())
	}

	*now = now.Add(6 * time.Second)
	denyBody := bytes.NewBufferString(`{"user_code":"` + payload["user_code"].(string) + `","approved":false}`)
	denyReq := httptest.NewRequest(http.MethodPost, "/oauth/device/verify", denyBody)
	denyReq.Header.Set("Content-Type", "application/json")
	denyW := httptest.NewRecorder()
	router.ServeHTTP(denyW, denyReq)
	if denyW.Code != http.StatusOK {
		t.Fatalf("deny status=%d body=%s", denyW.Code, denyW.Body.String())
	}

	deniedReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	deniedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deniedW := httptest.NewRecorder()
	router.ServeHTTP(deniedW, deniedReq)
	if !strings.Contains(deniedW.Body.String(), "access_denied") {
		t.Fatalf("denied body=%s", deniedW.Body.String())
	}

	expiringPayload, expiringCode := issueDeviceCode(t, router)
	_ = expiringPayload
	handler.devices[expiringCode].ExpiresAt = now.Add(-time.Second)
	expiredForm := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {expiringCode},
		"client_id":   {"ae-cli"},
	}
	expiredReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(expiredForm.Encode()))
	expiredReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	expiredW := httptest.NewRecorder()
	router.ServeHTTP(expiredW, expiredReq)
	if !strings.Contains(expiredW.Body.String(), "expired_token") {
		t.Fatalf("expired body=%s", expiredW.Body.String())
	}
}
```

Append to `backend/internal/handler/router_frontend_test.go`:

```go
func TestSetupRouterServesEmbeddedFrontendAtOAuthDevice(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>device-app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	restore := web.SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	env := setupTestEnv(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/device", nil)
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "device-app") {
		t.Fatalf("body=%q", w.Body.String())
	}
}
```

Append to `backend/internal/oauth/handler_test.go`:

```go
func TestDeviceCodeRejectsUnknownClient(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/oauth/device/code", strings.NewReader("client_id=unknown"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid_client") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestDevicePageRedirectsToFrontend(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/device", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "/oauth/device") {
		t.Fatalf("location=%q", loc)
	}
}
```

- [ ] **Step 2: Run the backend tests to verify they fail**

Run:

```bash
go test ./internal/oauth -run 'TestTokenDeviceGrant' -count=1
```

Expected: FAIL with missing `Handler.DeviceCode` / `Handler.VerifyDevice`, or `unsupported_grant_type` from `/oauth/token`.

- [ ] **Step 3: Write the minimal backend implementation**

Update `backend/internal/oauth/handler.go` and `backend/internal/handler/router.go` with:

```go
type deviceEntry struct {
	DeviceCode      string
	UserCode        string
	ClientID        string
	Status          string
	UserID          int
	Username        string
	Role            string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	LastPolledAt    time.Time
	PollIntervalSec int
}

type verifyDeviceRequest struct {
	UserCode string `json:"user_code" binding:"required"`
	Approved bool   `json:"approved"`
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (h *Handler) DeviceCode(c *gin.Context) {
	clientID := strings.TrimSpace(c.PostForm("client_id"))
	if !h.server.IsValidClient(clientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}

	now := h.now()
	deviceCode := generateCode()
	userCode := generateUserCode()
	entry := &deviceEntry{
		DeviceCode:      deviceCode,
		UserCode:        normalizeUserCode(userCode),
		ClientID:        clientID,
		Status:          "pending",
		CreatedAt:       now,
		ExpiresAt:       now.Add(h.deviceCodeExpiry),
		PollIntervalSec: int(h.devicePollInterval / time.Second),
	}

	h.mu.Lock()
	h.devices[deviceCode] = entry
	h.mu.Unlock()

	c.JSON(http.StatusOK, deviceCodeResponse{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationURI: strings.TrimRight(h.frontendURL, "/") + "/oauth/device",
		ExpiresIn:       int(h.deviceCodeExpiry / time.Second),
		Interval:        entry.PollIntervalSec,
	})
}

func (h *Handler) DevicePage(c *gin.Context) {
	if h.shouldServeEmbeddedPath(c, "/oauth/device") && web.ServeEmbeddedIndex(c) {
		return
	}
	c.Redirect(http.StatusFound, strings.TrimRight(h.frontendURL, "/")+"/oauth/device")
}

func (h *Handler) VerifyDevice(c *gin.Context) {
	var req verifyDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	uc := auth.GetUserContext(c)
	if uc == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing_user_context"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	entry := h.findDeviceByUserCodeLocked(req.UserCode)
	if entry == nil || h.isExpiredLocked(entry) || entry.Status == "consumed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
		return
	}

	if !req.Approved {
		entry.Status = "denied"
		c.JSON(http.StatusOK, gin.H{"status": "denied"})
		return
	}

	entry.Status = "approved"
	entry.UserID = uc.UserID
	entry.Username = uc.Username
	entry.Role = uc.Role
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

func (h *Handler) exchangeDeviceToken(c *gin.Context, clientID, deviceCode string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.devices[deviceCode]
	if !ok || entry.ClientID != clientID || entry.Status == "consumed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	if h.isExpiredLocked(entry) {
		entry.Status = "expired"
		c.JSON(http.StatusBadRequest, gin.H{"error": "expired_token"})
		return
	}
	if !entry.LastPolledAt.IsZero() && h.now().Sub(entry.LastPolledAt) < time.Duration(entry.PollIntervalSec)*time.Second {
		entry.LastPolledAt = h.now()
		c.JSON(http.StatusBadRequest, gin.H{"error": "slow_down"})
		return
	}
	entry.LastPolledAt = h.now()

	switch entry.Status {
	case "pending":
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
		return
	case "denied":
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
		return
	case "approved":
		entry.Status = "consumed"
	}

	accessToken, refreshToken, expiresIn, err := h.tokenGen.GenerateAccessToken(entry.UserID, entry.Username, entry.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.ReplaceAll(code, " ", "")
}
```

and in `backend/internal/handler/router.go`:

```go
if oauthHandler != nil {
	r.GET("/oauth/authorize", oauthHandler.Authorize)
	r.GET("/oauth/device", oauthHandler.DevicePage)
	r.POST("/oauth/device/code", oauthHandler.DeviceCode)
	r.POST("/oauth/token", oauthHandler.Token)

	oauthAuth := r.Group("/oauth")
	oauthAuth.Use(auth.RequireAuth(authService))
	oauthAuth.POST("/authorize/approve", oauthHandler.Approve)
	oauthAuth.POST("/device/verify", oauthHandler.VerifyDevice)
}
```

- [ ] **Step 4: Run the backend tests to verify they pass**

Run:

```bash
go test ./internal/oauth ./internal/handler -count=1
```

Expected: PASS for the new device-flow tests and existing router/frontend coverage.

- [ ] **Step 5: Commit the backend slice**

Run:

```bash
git add backend/internal/oauth/handler.go backend/internal/oauth/device_internal_test.go backend/internal/oauth/handler_test.go backend/internal/handler/router.go backend/internal/handler/router_frontend_test.go
git commit -m "feat(backend): add oauth device grant"
```

### Task 2: CLI OAuth Device Client

**Files:**
- Modify: `ae-cli/internal/auth/oauth.go`
- Create: `ae-cli/internal/auth/device.go`
- Create: `ae-cli/internal/auth/device_test.go`

- [ ] **Step 1: Write the failing device-client tests**

Create `ae-cli/internal/auth/device_test.go` with:

```go
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginDevicePollsUntilTokenIssued(t *testing.T) {
	polls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": server.URL + "/oauth/device",
				"expires_in":       900,
				"interval":         5,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			if polls == 2 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	var sleeps []time.Duration
	result, err := LoginDevice(context.Background(), OAuthConfig{
		ServerURL:  server.URL,
		ClientID:   "ae-cli",
		Timeout:    30 * time.Second,
		HTTPClient: server.Client(),
		Output:     &out,
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("LoginDevice() error = %v", err)
	}
	if result.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want access-token", result.AccessToken)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != 10*time.Second {
		t.Fatalf("sleeps = %v, want [5s 10s]", sleeps)
	}
	if !strings.Contains(out.String(), "ABCD-EFGH") {
		t.Fatalf("output = %q, want user code", out.String())
	}
}

func TestLoginDeviceReturnsServerErrors(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/device/code" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": server.URL + "/oauth/device",
				"expires_in":       900,
				"interval":         5,
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	}))
	defer server.Close()

	_, err := LoginDevice(context.Background(), OAuthConfig{
		ServerURL:  server.URL,
		ClientID:   "ae-cli",
		Timeout:    5 * time.Second,
		HTTPClient: server.Client(),
		Output:     &bytes.Buffer{},
		Sleep:      func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v, want access_denied", err)
	}
}

func TestIsHeadlessLinux(t *testing.T) {
	if !IsHeadlessLinux(func(string) string { return "" }, "linux") {
		t.Fatal("expected linux with empty DISPLAY/WAYLAND_DISPLAY to be headless")
	}
	if IsHeadlessLinux(func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}, "linux") {
		t.Fatal("expected DISPLAY to disable headless hint")
	}
	if IsHeadlessLinux(func(string) string { return "" }, "darwin") {
		t.Fatal("expected non-linux OS not to trigger headless hint")
	}
}
```

- [ ] **Step 2: Run the device-client tests to verify they fail**

Run:

```bash
go test ./internal/auth -run 'TestLoginDevice|TestIsHeadlessLinux' -count=1
```

Expected: FAIL with undefined `LoginDevice` / `IsHeadlessLinux`.

- [ ] **Step 3: Write the minimal CLI auth implementation**

Create `ae-cli/internal/auth/device.go` and extend `ae-cli/internal/auth/oauth.go` with:

```go
type OAuthConfig struct {
	ServerURL  string
	ClientID   string
	Timeout    time.Duration
	HTTPClient *http.Client
	Output     io.Writer
	Sleep      func(time.Duration)
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func LoginDevice(ctx context.Context, cfg OAuthConfig) (*OAuthResult, error) {
	cfg = withOAuthDefaults(cfg)
	deviceResp, err := requestDeviceCode(ctx, cfg)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(cfg.Output, "Open this URL in a browser:\n%s\n\n", deviceResp.VerificationURI)
	fmt.Fprintf(cfg.Output, "Enter this code:\n%s\n\n", deviceResp.UserCode)
	fmt.Fprintf(cfg.Output, "This code expires in %d seconds.\n", deviceResp.ExpiresIn)

	interval := time.Duration(deviceResp.Interval) * time.Second
	for {
		cfg.Sleep(interval)
		token, oauthErr, err := pollDeviceToken(ctx, cfg, deviceResp.DeviceCode)
		if err != nil {
			return nil, err
		}
		switch oauthErr {
		case "":
			return token, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		default:
			return nil, fmt.Errorf("device token exchange failed: %s", oauthErr)
		}
	}
}

func IsHeadlessLinux(lookupEnv func(string) string, goos string) bool {
	if goos != "linux" {
		return false
	}
	return strings.TrimSpace(lookupEnv("DISPLAY")) == "" &&
		strings.TrimSpace(lookupEnv("WAYLAND_DISPLAY")) == ""
}

func withOAuthDefaults(cfg OAuthConfig) OAuthConfig {
	if cfg.ClientID == "" {
		cfg.ClientID = "ae-cli"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Minute
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	return cfg
}
```

Also update `Login` in `ae-cli/internal/auth/oauth.go` to call `cfg = withOAuthDefaults(cfg)` and write browser messages via `fmt.Fprintf(cfg.Output, ...)` instead of `fmt.Printf(...)`.

- [ ] **Step 4: Run the device-client tests to verify they pass**

Run:

```bash
go test ./internal/auth -run 'TestLoginDevice|TestIsHeadlessLinux' -count=1
```

Expected: PASS with the new device polling and headless tests green.

- [ ] **Step 5: Commit the auth-library slice**

Run:

```bash
git add ae-cli/internal/auth/oauth.go ae-cli/internal/auth/device.go ae-cli/internal/auth/device_test.go
git commit -m "feat(ae-cli): add oauth device client"
```

### Task 3: CLI Login Command Selection

**Files:**
- Modify: `ae-cli/cmd/login.go`
- Modify: `ae-cli/cmd/login_test.go`

- [ ] **Step 1: Write the failing login command tests**

Append to `ae-cli/cmd/login_test.go`:

```go
func TestLoginCommandUsesDeviceFlowWhenFlagSet(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldBrowser := loginFlow
	oldDeviceFlow := loginDeviceFlow
	oldHeadless := headlessBrowserEnv
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldBrowser
		loginDeviceFlow = oldDeviceFlow
		headlessBrowserEnv = oldHeadless
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginDevice = true
	loginForce = true
	headlessBrowserEnv = func(func(string) string, string) bool { return false }

	calledBrowser := false
	calledDevice := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		calledBrowser = true
		return nil, nil
	}
	loginDeviceFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		calledDevice = true
		return &auth.OAuthResult{
			AccessToken:  "device-access-token",
			RefreshToken: "device-refresh-token",
			ExpiresIn:    3600,
		}, nil
	}

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if calledBrowser {
		t.Fatal("browser flow should not run when --device is set")
	}
	if !calledDevice {
		t.Fatal("device flow should run when --device is set")
	}
}

func TestLoginCommandSuggestsDeviceFlowInHeadlessLinux(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldHeadless := headlessBrowserEnv
	oldBrowser := loginFlow
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		headlessBrowserEnv = oldHeadless
		loginFlow = oldBrowser
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = true
	loginDevice = false
	headlessBrowserEnv = func(func(string) string, string) bool { return true }
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		t.Fatal("browser flow should not run in headless mode")
		return nil, nil
	}

	err := loginCmd.RunE(loginCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "ae-cli login --device") {
		t.Fatalf("err = %v, want device-flow guidance", err)
	}
}
```

- [ ] **Step 2: Run the login command tests to verify they fail**

Run:

```bash
go test ./cmd -run 'TestLoginCommandUsesDeviceFlowWhenFlagSet|TestLoginCommandSuggestsDeviceFlowInHeadlessLinux' -count=1
```

Expected: FAIL with undefined `loginDevice`, `loginDeviceFlow`, or `headlessBrowserEnv`.

- [ ] **Step 3: Write the minimal login command implementation**

Update `ae-cli/cmd/login.go` with:

```go
var (
	loginForce        bool
	loginDevice       bool
	loginFlow         = auth.Login
	loginDeviceFlow   = auth.LoginDevice
	headlessBrowserEnv = auth.IsHeadlessLinux
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to the AI Efficiency Platform",
	Long:  "Uses browser PKCE by default and supports OAuth device flow with --device.",
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL := resolveLoginServerURL(cfg, buildinfo.ServerURL)
		if serverURL == "" {
			return fmt.Errorf("server URL not configured")
		}

		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return fmt.Errorf("get token path: %w", err)
		}
		if !loginForce {
			if token, err := auth.ReadToken(tokenPath); err == nil && token.IsValid() {
				cmd.Println("Already logged in. Use --force to re-login.")
				return nil
			}
		}

		oauthCfg := auth.OAuthConfig{
			ServerURL: serverURL,
			ClientID:  "ae-cli",
			Timeout:   3 * time.Minute,
			Output:    cmd.OutOrStdout(),
		}

		var result *auth.OAuthResult
		switch {
		case loginDevice:
			result, err = loginDeviceFlow(context.Background(), oauthCfg)
		case headlessBrowserEnv(os.Getenv, runtime.GOOS):
			return fmt.Errorf("No browser environment detected. Use 'ae-cli login --device'.")
		default:
			result, err = loginFlow(context.Background(), oauthCfg)
		}
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		token := &auth.TokenFile{
			AccessToken:  result.AccessToken,
			RefreshToken: result.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
			ServerURL:    serverURL,
		}
		if err := auth.WriteToken(tokenPath, token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Login successful! Token saved to %s\n", tokenPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().BoolVar(&loginForce, "force", false, "Force re-login even if already logged in")
	loginCmd.Flags().BoolVar(&loginDevice, "device", false, "Use OAuth device authorization flow")
}
```

- [ ] **Step 4: Run the login command tests to verify they pass**

Run:

```bash
go test ./cmd -run 'TestLoginCommandUsesDeviceFlowWhenFlagSet|TestLoginCommandSuggestsDeviceFlowInHeadlessLinux' -count=1
```

Expected: PASS with `--device` selecting device flow and headless browser mode suggesting `ae-cli login --device`.

- [ ] **Step 5: Commit the command slice**

Run:

```bash
git add ae-cli/cmd/login.go ae-cli/cmd/login_test.go
git commit -m "feat(ae-cli): add device login flag"
```

### Task 4: Frontend Device Approval Page

**Files:**
- Modify: `frontend/src/api/oauth.ts`
- Modify: `frontend/src/router/index.ts`
- Create: `frontend/src/views/oauth/DevicePage.vue`
- Create: `frontend/src/__tests__/oauth-device-page.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`

- [ ] **Step 1: Write the failing frontend tests**

Create `frontend/src/__tests__/oauth-device-page.test.ts` with:

```ts
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import DevicePage from '@/views/oauth/DevicePage.vue'

vi.mock('@/api/oauth', () => ({
  verifyDeviceAuthorization: vi.fn(),
}))

function createTestRouter(initialPath = '/oauth/device') {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/oauth/device', component: DevicePage },
    ],
  })
}

describe('DevicePage', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('redirects unauthenticated users to login with redirect query', async () => {
    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    mount(DevicePage, {
      global: { plugins: [createPinia(), router] },
    })
    await flushPromises()

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/oauth/device')
  })

  it('submits approval and shows success state', async () => {
    const { verifyDeviceAuthorization } = await import('@/api/oauth')
    ;(verifyDeviceAuthorization as any).mockResolvedValue({ status: 'approved' })

    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.token = 'jwt-token'

    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    const wrapper = mount(DevicePage, {
      global: { plugins: [pinia, router] },
    })

    await wrapper.find('input#user-code').setValue('abcd-efgh')
    await wrapper.find('button[data-action="approve"]').trigger('click')
    await flushPromises()

    expect(verifyDeviceAuthorization).toHaveBeenCalledWith({
      user_code: 'abcd-efgh',
      approved: true,
    })
    expect(wrapper.text()).toContain('Approved. You can return to the terminal.')
  })

  it('shows server validation errors for invalid codes', async () => {
    const { verifyDeviceAuthorization } = await import('@/api/oauth')
    ;(verifyDeviceAuthorization as any).mockRejectedValue({
      response: { data: { message: 'Code invalid or expired' } },
    })

    const pinia = createPinia()
    setActivePinia(pinia)
    const auth = useAuthStore()
    auth.token = 'jwt-token'

    const router = createTestRouter()
    await router.push('/oauth/device')
    await router.isReady()

    const wrapper = mount(DevicePage, {
      global: { plugins: [pinia, router] },
    })

    await wrapper.find('input#user-code').setValue('bad-code')
    await wrapper.find('button[data-action="deny"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Code invalid or expired')
  })
})
```

Append to `frontend/src/__tests__/router.test.ts`:

```ts
it('includes oauth device route in the router', async () => {
  const oauthDevice = router.getRoutes().find((r) => r.name === 'OAuthDevice')
  expect(oauthDevice?.path).toBe('/oauth/device')
  expect(oauthDevice?.meta.public).toBe(true)
})
```

- [ ] **Step 2: Run the frontend tests to verify they fail**

Run:

```bash
pnpm exec vitest --run src/__tests__/oauth-device-page.test.ts src/__tests__/router.test.ts
```

Expected: FAIL with missing `DevicePage`, missing `verifyDeviceAuthorization`, or missing `OAuthDevice` route.

- [ ] **Step 3: Write the minimal frontend implementation**

Update `frontend/src/api/oauth.ts`, `frontend/src/router/index.ts`, and create `frontend/src/views/oauth/DevicePage.vue`:

```ts
export interface DeviceVerifyRequest {
  user_code: string
  approved: boolean
}

export interface DeviceVerifyResponse {
  status: 'approved' | 'denied'
}

export async function verifyDeviceAuthorization(req: DeviceVerifyRequest): Promise<DeviceVerifyResponse> {
  const { data } = await client.post('/oauth/device/verify', req)
  return data.data ?? data
}
```

```ts
{
  path: '/oauth/device',
  name: 'OAuthDevice',
  component: () => import('@/views/oauth/DevicePage.vue'),
  meta: { public: true },
},
```

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { verifyDeviceAuthorization } from '@/api/oauth'

const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()

const userCode = ref('')
const loading = ref(false)
const error = ref('')
const result = ref('')

onMounted(async () => {
  if (!authStore.isAuthenticated) {
    await router.replace({ path: '/login', query: { redirect: route.fullPath } })
  }
})

async function submit(approved: boolean) {
  loading.value = true
  error.value = ''
  result.value = ''
  try {
    const resp = await verifyDeviceAuthorization({
      user_code: userCode.value,
      approved,
    })
    result.value = resp.status === 'approved'
      ? 'Approved. You can return to the terminal.'
      : 'Access denied.'
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'Code invalid or expired'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="w-full max-w-md rounded-lg bg-white p-8 shadow">
      <h1 class="mb-4 text-2xl font-bold text-gray-900">Device Login</h1>
      <p class="mb-4 text-sm text-gray-600">Enter the code shown by <code>ae-cli login --device</code>.</p>

      <input
        id="user-code"
        v-model="userCode"
        type="text"
        class="mb-4 w-full rounded border border-gray-300 px-3 py-2"
        placeholder="ABCD-EFGH"
      />

      <p v-if="error" class="mb-4 text-sm text-red-600">{{ error }}</p>
      <p v-if="result" class="mb-4 text-sm text-green-700">{{ result }}</p>

      <div class="flex gap-3">
        <button data-action="deny" class="flex-1 rounded border border-gray-300 px-4 py-2" :disabled="loading" @click="submit(false)">
          {{ loading ? 'Working...' : 'Deny' }}
        </button>
        <button data-action="approve" class="flex-1 rounded bg-blue-600 px-4 py-2 text-white" :disabled="loading" @click="submit(true)">
          {{ loading ? 'Working...' : 'Authorize' }}
        </button>
      </div>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run the frontend tests to verify they pass**

Run:

```bash
pnpm exec vitest --run src/__tests__/oauth-device-page.test.ts src/__tests__/router.test.ts
```

Expected: PASS with the new page tests and route test green.

- [ ] **Step 5: Commit the frontend slice**

Run:

```bash
git add frontend/src/api/oauth.ts frontend/src/router/index.ts frontend/src/views/oauth/DevicePage.vue frontend/src/__tests__/oauth-device-page.test.ts frontend/src/__tests__/router.test.ts
git commit -m "feat(frontend): add oauth device approval page"
```

### Task 5: Documentation And Full Verification

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-04-15-oauth-device-login-design.md`

- [ ] **Step 1: Update the docs to reflect the implemented runtime**

Update `docs/architecture.md` to describe the new login split:

```md
- `ae-cli login` now supports two login paths:
  - browser PKCE on machines with a browser/local callback path
  - OAuth device flow for headless or cross-machine login
- The backend OAuth handler now manages both short-lived authorization codes and short-lived device entries in memory.
```

and replace the runtime sequence block lines with:

```md
Dev->>CLI: ae-cli login / start
CLI->>BE: browser PKCE or device-code login
note over Dev,BE: In device flow, the user completes /oauth/device verification in a browser
CLI->>BE: session bootstrap
```

Update `docs/superpowers/specs/2026-04-15-oauth-device-login-design.md` header from:

```md
**Status:** Review Requested
**Implementation Note:** 当前代码中的 `ae-cli login` 只支持浏览器 + 本地回调的 Authorization Code Flow with PKCE，不支持 Device Authorization Flow。
```

to:

```md
**Status:** Current contract for OAuth device login
**Implementation Note:** Device Authorization Flow 已在 `backend/internal/oauth`、`ae-cli login --device` 和前端 `/oauth/device` 页面中落地；普通浏览器 PKCE 登录仍是默认路径。
```

- [ ] **Step 2: Run the full project verification**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS

Run:

```bash
cd ae-cli && go test ./...
```

Expected: PASS

Run:

```bash
cd frontend && pnpm test
```

Expected: PASS

- [ ] **Step 3: Commit the docs/final verification slice**

Run:

```bash
git add docs/architecture.md docs/superpowers/specs/2026-04-15-oauth-device-login-design.md
git commit -m "docs(architecture): reflect oauth device login flow"
```
