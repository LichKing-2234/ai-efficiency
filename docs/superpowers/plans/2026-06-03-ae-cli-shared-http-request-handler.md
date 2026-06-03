# ae-cli Shared HTTP Request Handler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a shared `ae-cli` HTTP request helper so OAuth form requests and authenticated JSON API requests use one consistent execution and error-reporting path.

**Architecture:** Add `ae-cli/internal/httpx` as a protocol-level helper with `DoJSON`, `DoForm`, and `StatusError`. Keep OAuth behavior in `internal/auth` and authenticated `/api/v1` behavior in `internal/client`; both packages depend downward on `httpx` without depending on each other.

**Tech Stack:** Go, `net/http`, `encoding/json`, `net/url`, standard `testing`, `httptest`, `errors.As`.

---

## Plan Tracking

During execution, update checkboxes in this file immediately after each step is actually completed. Do not wait until the final closeout task to backfill completed steps.

## File Structure

- Create: `ae-cli/internal/httpx/httpx.go`
  - Owns request construction, JSON/form body encoding, response decoding, limited non-2xx body reads, and structured status errors.
- Create: `ae-cli/internal/httpx/httpx_test.go`
  - Covers JSON success, form success, OAuth error summaries, ingress `message` errors, raw body fallback, empty body fallback, body limit, and header safety.
- Modify: `ae-cli/internal/auth/device.go`
  - Replaces direct `http.NewRequest` / `Do` / local error parsing with `httpx.DoForm`.
  - Keeps device polling control-flow decisions in `auth`.
- Modify: `ae-cli/internal/auth/oauth.go`
  - Replaces authorization-code token exchange request execution with `httpx.DoForm`.
- Modify: `ae-cli/internal/auth/device_test.go`
  - Keeps or adds device-flow ingress-message and OAuth-description regression tests.
- Modify: `ae-cli/internal/auth/oauth_test.go`
  - Keeps or adds token-exchange ingress-message regression tests.
- Modify: `ae-cli/internal/client/client.go`
  - Replaces `postJSON` internals with `httpx.DoJSON`.
  - Adds a local `headers()` helper for bearer-token headers.
- Modify: `ae-cli/internal/client/client_test.go`
  - Adds non-2xx status-error assertions for `postJSON` consumers.

---

### Task 1: Add Shared `httpx` Package

**Files:**
- Create: `ae-cli/internal/httpx/httpx_test.go`
- Create: `ae-cli/internal/httpx/httpx.go`

- [x] **Step 1: Write failing `httpx` tests**

Create `ae-cli/internal/httpx/httpx_test.go`:

```go
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDoJSONSendsAndDecodesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["name"] != "alice" {
			t.Fatalf("request = %+v, want name alice", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	var out struct {
		Status string `json:"status"`
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")
	err := DoJSON(context.Background(), server.Client(), http.MethodPost, server.URL, map[string]string{"name": "alice"}, &out, Options{Headers: headers})
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("Status = %q, want ok", out.Status)
	}
}

func TestDoFormSendsAndDecodesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q, want form", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("client_id"); got != "ae-cli" {
			t.Fatalf("client_id = %q, want ae-cli", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"device_code": "device-123"})
	}))
	defer server.Close()

	var out struct {
		DeviceCode string `json:"device_code"`
	}
	err := DoForm(context.Background(), server.Client(), http.MethodPost, server.URL, url.Values{"client_id": {"ae-cli"}}, &out, Options{})
	if err != nil {
		t.Fatalf("DoForm() error = %v", err)
	}
	if out.DeviceCode != "device-123" {
		t.Fatalf("DeviceCode = %q, want device-123", out.DeviceCode)
	}
}

func TestStatusErrorSummarizesOAuthErrorDescription(t *testing.T) {
	err := runErrorServer(t, http.StatusBadRequest, `{"error":"access_denied","error_description":"authorization denied by user"}`, Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "access_denied: authorization denied by user" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
	if !strings.Contains(statusErr.Error(), "HTTP 400 Bad Request") ||
		!strings.Contains(statusErr.Error(), "access_denied: authorization denied by user") {
		t.Fatalf("Error() = %q", statusErr.Error())
	}
}

func TestStatusErrorSummarizesIngressMessage(t *testing.T) {
	err := runErrorServer(t, http.StatusForbidden, `{"message":"Your IP address is not allowed"}`, Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "Your IP address is not allowed" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
	if !strings.Contains(statusErr.Error(), "HTTP 403 Forbidden") {
		t.Fatalf("Error() = %q, want HTTP status", statusErr.Error())
	}
}

func TestStatusErrorFallsBackToRawBody(t *testing.T) {
	err := runErrorServer(t, http.StatusBadGateway, "upstream unavailable", Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "upstream unavailable" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
}

func TestStatusErrorReportsEmptyBody(t *testing.T) {
	err := runErrorServer(t, http.StatusInternalServerError, "", Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "empty response body" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
}

func TestStatusErrorBodyLimit(t *testing.T) {
	err := runErrorServer(t, http.StatusBadRequest, "abcdef", Options{ErrorBodyLimit: 3})
	statusErr := assertStatusError(t, err)
	if statusErr.Body != "abc" || statusErr.Summary != "abc" {
		t.Fatalf("StatusError = %+v, want capped body abc", statusErr)
	}
}

func TestStatusErrorDoesNotRenderAuthorizationHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token-value")
	err := runErrorServer(t, http.StatusForbidden, `{"message":"denied"}`, Options{Headers: headers})
	statusErr := assertStatusError(t, err)
	if strings.Contains(statusErr.Error(), "test-token-value") {
		t.Fatalf("Error() leaked authorization header: %q", statusErr.Error())
	}
}

func runErrorServer(t *testing.T, status int, body string, opts Options) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	var out map[string]any
	return DoJSON(context.Background(), server.Client(), http.MethodPost, server.URL, map[string]string{"request": "test"}, &out, opts)
}

func assertStatusError(t *testing.T, err error) *StatusError {
	t.Helper()
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %T %v, want *StatusError", err, err)
	}
	return statusErr
}
```

- [x] **Step 2: Run `httpx` tests to verify they fail**

Run:

```bash
cd ae-cli && go test ./internal/httpx
```

Expected: fail because `DoJSON`, `DoForm`, `Options`, and `StatusError` are undefined or the package does not yet exist.

- [x] **Step 3: Implement `httpx`**

Create `ae-cli/internal/httpx/httpx.go`:

```go
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultErrorBodyLimit int64 = 4096

type Options struct {
	Headers        http.Header
	ErrorBodyLimit int64
}

type StatusError struct {
	Method     string
	URL        string
	Status     string
	StatusCode int
	Summary    string
	Body       string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	status := strings.TrimSpace(e.Status)
	if status == "" && e.StatusCode != 0 {
		status = fmt.Sprintf("%d", e.StatusCode)
	}
	summary := strings.TrimSpace(e.Summary)
	if summary == "" {
		summary = "empty response body"
	}
	method := strings.TrimSpace(e.Method)
	target := strings.TrimSpace(e.URL)
	if method == "" {
		method = "request"
	}
	if target == "" {
		return fmt.Sprintf("%s failed (HTTP %s): %s", method, status, summary)
	}
	return fmt.Sprintf("%s %s failed (HTTP %s): %s", method, target, status, summary)
}

func DoJSON(ctx context.Context, client *http.Client, method, requestURL string, in any, out any, opts Options) error {
	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal JSON request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	applyHeaders(req, opts.Headers)
	return do(req, client, out, opts)
}

func DoForm(ctx context.Context, client *http.Client, method, requestURL string, form url.Values, out any, opts Options) error {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	applyHeaders(req, opts.Headers)
	return do(req, client, out, opts)
}

func do(req *http.Request, client *http.Client, out any, opts Options) error {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send %s %s: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readStatusError(req, resp, opts)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readStatusError(req *http.Request, resp *http.Response, opts Options) error {
	limit := opts.ErrorBodyLimit
	if limit <= 0 {
		limit = DefaultErrorBodyLimit
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}
	body := strings.TrimSpace(string(bodyBytes))
	return &StatusError{
		Method:     req.Method,
		URL:        req.URL.String(),
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Summary:    summarizeErrorBody(body),
		Body:       body,
	}
}

func summarizeErrorBody(body string) string {
	if body == "" {
		return "empty response body"
	}
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		switch {
		case payload.Error != "" && payload.ErrorDescription != "":
			return payload.Error + ": " + payload.ErrorDescription
		case payload.ErrorDescription != "":
			return payload.ErrorDescription
		case payload.Error != "":
			return payload.Error
		case payload.Message != "":
			return payload.Message
		}
	}
	return body
}

func applyHeaders(req *http.Request, headers http.Header) {
	for key, values := range headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
}
```

- [x] **Step 4: Run `httpx` tests to verify they pass**

Run:

```bash
cd ae-cli && go test ./internal/httpx
```

Expected: `ok github.com/ai-efficiency/ae-cli/internal/httpx`.

- [x] **Step 5: Commit Task 1**

Run:

```bash
git add ae-cli/internal/httpx/httpx.go ae-cli/internal/httpx/httpx_test.go
git commit -m "feat(ae-cli): add shared HTTP request helper"
```

Expected: commit succeeds with only the new `httpx` package files.

---

### Task 2: Migrate OAuth Login Requests To `httpx`

**Files:**
- Modify: `ae-cli/internal/auth/device.go`
- Modify: `ae-cli/internal/auth/oauth.go`
- Modify: `ae-cli/internal/auth/device_test.go`
- Modify: `ae-cli/internal/auth/oauth_test.go`

- [x] **Step 1: Add or keep OAuth error regression tests**

Ensure `ae-cli/internal/auth/device_test.go` contains these tests:

```go
func TestLoginDeviceShowsIngressMessageErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/device/code" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Your IP address is not allowed"})
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
	if err == nil {
		t.Fatal("LoginDevice() error = nil, want ingress message")
	}
	if !strings.Contains(err.Error(), "HTTP 403 Forbidden") ||
		!strings.Contains(err.Error(), "Your IP address is not allowed") {
		t.Fatalf("err = %v, want status and ingress message", err)
	}
}

func TestLoginDeviceTokenErrorIncludesDescription(t *testing.T) {
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
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "access_denied",
				"error_description": "authorization denied by user",
			})
		default:
			http.NotFound(w, r)
		}
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
	if err == nil ||
		!strings.Contains(err.Error(), "access_denied: authorization denied by user") {
		t.Fatalf("err = %v, want OAuth error description", err)
	}
}
```

Ensure `ae-cli/internal/auth/oauth_test.go` contains this test:

```go
func TestExchangeCodeShowsIngressMessageErrors(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Your IP address is not allowed"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	_, err := auth.ExchangeCode(context.Background(), backend.URL, "test-code", "http://localhost:12345/callback", "test-verifier")
	if err == nil {
		t.Fatal("ExchangeCode() error = nil, want ingress message")
	}
	if !strings.Contains(err.Error(), "HTTP 403 Forbidden") ||
		!strings.Contains(err.Error(), "Your IP address is not allowed") {
		t.Fatalf("err = %v, want status and ingress message", err)
	}
}
```

- [x] **Step 2: Run auth tests before migration**

Run:

```bash
cd ae-cli && go test ./internal/auth
```

Expected: tests may pass if a temporary auth-local parser exists, but this does not complete the task. The implementation must still replace direct request execution with `httpx`.

- [x] **Step 3: Replace device-code request execution**

In `ae-cli/internal/auth/device.go`, add imports:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/httpx"
)
```

Replace `requestDeviceCode` with:

```go
func requestDeviceCode(ctx context.Context, cfg OAuthConfig) (*deviceCodeResponse, error) {
	data := url.Values{
		"client_id": {cfg.ClientID},
	}

	var payload deviceCodeResponse
	if err := httpx.DoForm(ctx, cfg.HTTPClient, http.MethodPost, cfg.ServerURL+"/oauth/device/code", data, &payload, httpx.Options{}); err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}

	return &payload, nil
}
```

- [x] **Step 4: Replace device-token polling execution**

In `ae-cli/internal/auth/device.go`, replace `pollDeviceToken` with:

```go
func pollDeviceToken(ctx context.Context, cfg OAuthConfig, deviceCode string) (*OAuthResult, string, string, error) {
	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {cfg.ClientID},
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	err := httpx.DoForm(ctx, cfg.HTTPClient, http.MethodPost, cfg.ServerURL+"/oauth/token", data, &tokenResp, httpx.Options{})
	if err != nil {
		var statusErr *httpx.StatusError
		if errors.As(err, &statusErr) {
			oauthErr := decodeOAuthErrorBody(statusErr.Body)
			if oauthErr.Error != "" {
				return nil, oauthErr.Error, statusErr.Summary, nil
			}
		}
		return nil, "", "", fmt.Errorf("device token request failed: %w", err)
	}

	return &OAuthResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, "", "", nil
}
```

Add this helper below `pollDeviceToken`:

```go
func decodeOAuthErrorBody(body string) oauthErrorResponse {
	var errResp oauthErrorResponse
	if strings.TrimSpace(body) == "" {
		return errResp
	}
	_ = json.Unmarshal([]byte(body), &errResp)
	return errResp
}
```

Keep `oauthErrorResponse` with these fields:

```go
type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Message          string `json:"message"`
}
```

Remove any auth-local helpers named `readOAuthErrorSummary`, `readOAuthErrorResponse`, or `oauthErrorResponse.summary`. Error summaries now come from `httpx.StatusError.Summary`.

- [x] **Step 5: Replace authorization-code token exchange execution**

In `ae-cli/internal/auth/oauth.go`, add the `httpx` import:

```go
import "github.com/ai-efficiency/ae-cli/internal/httpx"
```

Replace the direct `http.NewRequestWithContext`, `client.Do`, non-2xx handling, and token decode section inside `exchangeCodeWithClient` with:

```go
var tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}
if err := httpx.DoForm(ctx, client, http.MethodPost, serverURL+"/oauth/token", data, &tokenResp, httpx.Options{}); err != nil {
	return nil, fmt.Errorf("token exchange failed: %w", err)
}

return &OAuthResult{
	AccessToken:  tokenResp.AccessToken,
	RefreshToken: tokenResp.RefreshToken,
	ExpiresIn:    tokenResp.ExpiresIn,
}, nil
```

Remove unused imports from `oauth.go`, especially `encoding/json` and any request-body imports that are no longer needed by that file.

- [x] **Step 6: Format and run auth tests**

Run:

```bash
cd ae-cli && gofmt -w internal/auth/device.go internal/auth/device_test.go internal/auth/oauth.go internal/auth/oauth_test.go
cd ae-cli && go test ./internal/auth
```

Expected: `ok github.com/ai-efficiency/ae-cli/internal/auth`.

- [x] **Step 7: Commit Task 2**

Run:

```bash
git add ae-cli/internal/auth/device.go ae-cli/internal/auth/device_test.go ae-cli/internal/auth/oauth.go ae-cli/internal/auth/oauth_test.go
git commit -m "fix(ae-cli): use shared HTTP errors for OAuth login"
```

Expected: commit succeeds with only auth package files.

---

### Task 3: Migrate `client.postJSON` To `httpx`

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`

- [x] **Step 1: Add client status-error tests**

Append these tests to `ae-cli/internal/client/client_test.go`:

```go
func TestResolveRepoFromRemoteReturnsHTTPXStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("path = %s, want /api/v1/repos/resolve-remote", r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Your IP address is not allowed"})
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").ResolveRepoFromRemote(context.Background(), ResolveRepoRequest{
		RemoteURL:          "https://git.example.com/org/repo.git",
		ClientCacheVersion: RepoEligibilityVersion,
	})
	var statusErr *httpx.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %T %v, want *httpx.StatusError", err, err)
	}
	if statusErr.StatusCode != http.StatusForbidden ||
		statusErr.Summary != "Your IP address is not allowed" {
		t.Fatalf("statusErr = %+v", statusErr)
	}
}

func TestBatchHookEligibleReturnsHTTPXStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/hook-eligible" {
			t.Fatalf("path = %s, want /api/v1/repos/hook-eligible", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "tok").BatchHookEligible(context.Background(), []HookEligibleRepoRequest{{
		RepoKey:   "org/repo",
		RemoteURL: "https://git.example.com/org/repo.git",
	}})
	var statusErr *httpx.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %T %v, want *httpx.StatusError", err, err)
	}
	if statusErr.StatusCode != http.StatusBadGateway ||
		statusErr.Summary != "upstream unavailable" {
		t.Fatalf("statusErr = %+v", statusErr)
	}
}
```

Update the import block in `client_test.go` to include:

```go
import (
	"errors"

	"github.com/ai-efficiency/ae-cli/internal/httpx"
)
```

Keep existing imports such as `context`, `encoding/json`, `net/http`, `net/http/httptest`, `sync/atomic`, `testing`, and `time`.

- [x] **Step 2: Run client tests to verify the new tests fail**

Run:

```bash
cd ae-cli && go test ./internal/client
```

Expected: fail because `postJSON` still returns plain formatted errors rather than `*httpx.StatusError`.

- [x] **Step 3: Add `httpx` import and header helper**

In `ae-cli/internal/client/client.go`, add:

```go
import "github.com/ai-efficiency/ae-cli/internal/httpx"
```

Add this method near `setHeaders`:

```go
func (c *Client) headers() http.Header {
	headers := http.Header{}
	if c.token != "" {
		headers.Set("Authorization", "Bearer "+c.token)
	}
	return headers
}
```

Keep the existing `setHeaders` method because several manually implemented methods still use it.

- [x] **Step 4: Replace `postJSON` internals**

Replace `postJSON` in `ae-cli/internal/client/client.go` with:

```go
func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	if err := httpx.DoJSON(ctx, c.httpClient, http.MethodPost, c.baseURL+path, in, out, httpx.Options{Headers: c.headers()}); err != nil {
		return err
	}
	return nil
}
```

Do not change `ResolveRepoFromRemote` or `BatchHookEligible` payload shapes.

- [x] **Step 5: Format and run client tests**

Run:

```bash
cd ae-cli && gofmt -w internal/client/client.go internal/client/client_test.go
cd ae-cli && go test ./internal/client
```

Expected: `ok github.com/ai-efficiency/ae-cli/internal/client`.

- [x] **Step 6: Commit Task 3**

Run:

```bash
git add ae-cli/internal/client/client.go ae-cli/internal/client/client_test.go
git commit -m "refactor(ae-cli): route API JSON posts through shared HTTP helper"
```

Expected: commit succeeds with only client package files.

---

### Task 4: Final Verification And Plan Closeout

**Files:**
- Modify: `docs/superpowers/plans/2026-06-03-ae-cli-shared-http-request-handler.md`

- [x] **Step 1: Run targeted tests**

Run:

```bash
cd ae-cli && go test ./internal/httpx ./internal/auth ./internal/client
```

Expected: all three packages pass.

- [x] **Step 2: Run full ae-cli test suite**

Run:

```bash
cd ae-cli && go test ./...
```

Expected: all packages pass.

- [x] **Step 3: Inspect request-handler adoption**

Run:

```bash
rg -n "readOAuthErrorSummary|readOAuthErrorResponse|oauthErrorResponse\\.summary|device code request failed: %s|token exchange failed: %s" ae-cli/internal/auth ae-cli/internal/client
```

Expected: no matches.

Run:

```bash
rg -n "httpx\\.DoForm|httpx\\.DoJSON|type StatusError" ae-cli/internal
```

Expected: matches in `ae-cli/internal/httpx`, `ae-cli/internal/auth`, and `ae-cli/internal/client`.

- [x] **Step 4: Confirm this plan's checkboxes are current**

Review `docs/superpowers/plans/2026-06-03-ae-cli-shared-http-request-handler.md` and confirm every step completed in this run is already marked `- [x]`. Mark only steps that were actually completed. Leave any skipped or unrun step unchecked.

- [x] **Step 5: Commit plan closeout**

Run:

```bash
git add docs/superpowers/plans/2026-06-03-ae-cli-shared-http-request-handler.md
git commit -m "docs(ae-cli): track shared HTTP handler implementation"
```

Expected: commit succeeds if the plan file changed during execution. If no checkbox changed because the implementation was executed by another mechanism that already updated the plan, `git commit` may report nothing to commit; in that case, record that in the final response.

- [x] **Step 6: Final status audit**

Run:

```bash
git status --short
```

Expected: no unexpected modified files. If unrelated user changes remain, list them separately and do not revert them.

Confirm final behavior:

- `ae-cli login --device` surfaces gateway JSON `message` errors with HTTP status.
- OAuth device polling still handles `authorization_pending` and `slow_down`.
- Authenticated JSON `postJSON` consumers return `*httpx.StatusError` for non-2xx responses.
- Full `ae-cli` tests pass.
