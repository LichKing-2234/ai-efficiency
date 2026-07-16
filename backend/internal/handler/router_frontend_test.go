package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const routerTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestSetupRouterPropagatesConfiguredRequestTimeout(t *testing.T) {
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
	const requestTimeout = 35 * time.Second

	env := setupTestEnv(t)
	router := SetupRouterWithOptions(
		env.client,
		nil,
		env.authSvc,
		repo.NewService(env.client, encryptionKey, zap.NewNop()),
		webhook.NewHandler(env.client, nil, zap.NewNop()),
		nil,
		nil,
		encryptionKey,
		"",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterOptions{RequestTimeout: requestTimeout},
	)

	var requestContext context.Context
	router.GET("/bounded-request", func(c *gin.Context) {
		requestContext = c.Request.Context()
		deadline, ok := requestContext.Deadline()
		if !ok {
			t.Fatal("router request context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > requestTimeout {
			t.Fatalf("router request deadline remaining = %s, want within (0, %s]", remaining, requestTimeout)
		}
		c.Status(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/bounded-request", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if requestContext == nil {
		t.Fatal("handler did not receive request context")
	}
	if requestContext.Err() != context.Canceled {
		t.Fatalf("request context error = %v, want context canceled after handler completion", requestContext.Err())
	}
}

func TestSetupRouterServesEmbeddedFrontendAtRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>router-app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	restore := web.SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	env := setupTestEnv(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(telemetry.HeaderRequestID, "embedded-request")
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "router-app") {
		t.Fatalf("body=%q", w.Body.String())
	}
	if got := w.Header().Get(telemetry.HeaderRequestID); got != "embedded-request" {
		t.Fatalf("%s = %q, want %q", telemetry.HeaderRequestID, got, "embedded-request")
	}
}

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

	oauthServer := oauth.NewServer()
	oauthHandler := oauth.NewHandler(oauthServer, "http://localhost:18081", nil)
	env := setupTestEnvWithOAuth(t, oauthHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://localhost:18081/oauth/device", nil)
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "device-app") {
		t.Fatalf("body=%q", w.Body.String())
	}
}

func TestSetupRouterFinalizesUnmatched404BeforeRequestTelemetry(t *testing.T) {
	env := setupTestEnv(t)
	core, observed := observer.New(zap.InfoLevel)
	router := SetupRouterWithOptions(
		env.client,
		nil,
		env.authSvc,
		repo.NewService(env.client, routerTestEncryptionKey, zap.NewNop()),
		webhook.NewHandler(env.client, nil, zap.NewNop()),
		nil,
		nil,
		routerTestEncryptionKey,
		"",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterOptions{RequestLogger: zap.New(core), Release: "test-release"},
	)

	request := httptest.NewRequest(http.MethodGet, "/private/raw-path?email=alice@example.com", nil)
	request.Header.Set(telemetry.HeaderRequestID, "unmatched-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if got, want := response.Body.String(), "404 page not found"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got := response.Header().Get(telemetry.HeaderRequestID); got != "unmatched-request" {
		t.Fatalf("%s = %q, want %q", telemetry.HeaderRequestID, got, "unmatched-request")
	}

	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("request events = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if got, ok := fields["route"].(string); !ok || got != "unmatched" {
		t.Fatalf("route = %#v, want %q", fields["route"], "unmatched")
	}
	if got, ok := fields["status_class"].(string); !ok || got != "4xx" {
		t.Fatalf("status_class = %#v, want %q", fields["status_class"], "4xx")
	}
	if got, ok := fields["request_id"].(string); !ok || got != "unmatched-request" {
		t.Fatalf("request_id = %#v, want %q", fields["request_id"], "unmatched-request")
	}
	if got, ok := fields["response_bytes"].(int64); !ok || got < 0 || got != int64(response.Body.Len()) {
		t.Fatalf("response_bytes = %#v, want exact non-negative body length %d", fields["response_bytes"], response.Body.Len())
	}
}

func TestSetupRouterCanonicalTrailingSlashRunsThroughRequestMiddleware(t *testing.T) {
	const (
		origin    = "https://app.example.com"
		requestID = "canonical-redirect-request"
	)

	core, observed := observer.New(zap.InfoLevel)
	router := setupRouterWithRequestLogger(t, middleware.CORS([]string{origin}), zap.New(core))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/health/?email=alice@example.com", nil)
	request.Header.Set("Origin", origin)
	request.Header.Set(telemetry.HeaderRequestID, requestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTemporaryRedirect)
	}
	if got, want := response.Header().Get("Location"), "/api/v1/health?email=alice@example.com"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
	if got := response.Header().Get(telemetry.HeaderRequestID); got != requestID {
		t.Fatalf("%s = %q, want %q", telemetry.HeaderRequestID, got, requestID)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := response.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, telemetry.HeaderRequestID) {
		t.Fatalf("Access-Control-Expose-Headers = %q, want it to include %s", got, telemetry.HeaderRequestID)
	}

	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("request events = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Message != "http_request" {
		t.Fatalf("event message = %q, want %q", entry.Message, "http_request")
	}
	fields := entry.ContextMap()
	if len(fields) != 8 {
		t.Fatalf("request fields = %#v, want exactly 8 normalized fields", fields)
	}
	assertRouterRequestField(t, fields, "event", "http_request")
	assertRouterRequestField(t, fields, "route", "unmatched")
	assertRouterRequestField(t, fields, "method", http.MethodGet)
	assertRouterRequestField(t, fields, "status_class", "3xx")
	assertRouterRequestField(t, fields, "release", "test-release")
	assertRouterRequestField(t, fields, "request_id", requestID)
	if got, ok := fields["response_bytes"].(int64); !ok || got < 0 || got != int64(response.Body.Len()) {
		t.Fatalf("response_bytes = %#v, want exact non-negative body length %d", fields["response_bytes"], response.Body.Len())
	}
	if got, ok := fields["duration_ms"].(int64); !ok || got < 0 {
		t.Fatalf("duration_ms = %#v, want non-negative int64", fields["duration_ms"])
	}
	serialized := fmt.Sprint(entry.Message, fields)
	for _, privateValue := range []string{"/api/v1/health/", "alice@example.com"} {
		if strings.Contains(serialized, privateValue) {
			t.Fatalf("request event contains private value %q: %s", privateValue, serialized)
		}
	}
}

func TestSetupRouterCanonicalMutationTrailingSlashPreservesReplayContract(t *testing.T) {
	const (
		origin    = "https://app.example.com"
		requestID = "canonical-mutation-request"
		payload   = "private-mutation-body"
	)

	core, observed := observer.New(zap.InfoLevel)
	router := setupRouterWithRequestLogger(t, middleware.CORS([]string{origin}), zap.New(core))
	var (
		handledMethod string
		handledQuery  string
		handledBody   string
		handledErr    error
	)
	router.POST("/api/v1/test-mutation", func(c *gin.Context) {
		handledMethod = c.Request.Method
		handledQuery = c.Query("email")
		body, err := io.ReadAll(c.Request.Body)
		handledErr = err
		handledBody = string(body)
		c.String(http.StatusOK, "mutation-handled")
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/test-mutation/?email=alice@example.com",
		strings.NewReader(payload),
	)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Origin", origin)
	request.Header.Set(telemetry.HeaderRequestID, requestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTemporaryRedirect)
	}
	location := response.Header().Get("Location")
	if want := "/api/v1/test-mutation?email=alice@example.com"; location != want {
		t.Fatalf("Location = %q, want %q", location, want)
	}
	if got := response.Header().Get(telemetry.HeaderRequestID); got != requestID {
		t.Fatalf("%s = %q, want %q", telemetry.HeaderRequestID, got, requestID)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := response.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, telemetry.HeaderRequestID) {
		t.Fatalf("Access-Control-Expose-Headers = %q, want it to include %s", got, telemetry.HeaderRequestID)
	}
	if handledMethod != "" || handledQuery != "" || handledBody != "" || handledErr != nil {
		t.Fatalf("canonical handler ran before redirect: method=%q query=%q body=%q err=%v", handledMethod, handledQuery, handledBody, handledErr)
	}

	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("redirect request events = %d, want 1", len(entries))
	}
	redirectEntry := entries[0]
	if redirectEntry.Message != "http_request" {
		t.Fatalf("redirect event message = %q, want %q", redirectEntry.Message, "http_request")
	}
	redirectFields := redirectEntry.ContextMap()
	if len(redirectFields) != 8 {
		t.Fatalf("redirect request fields = %#v, want exactly 8 normalized fields", redirectFields)
	}
	assertRouterRequestField(t, redirectFields, "event", "http_request")
	assertRouterRequestField(t, redirectFields, "route", "unmatched")
	assertRouterRequestField(t, redirectFields, "method", http.MethodPost)
	assertRouterRequestField(t, redirectFields, "status_class", "3xx")
	assertRouterRequestField(t, redirectFields, "release", "test-release")
	assertRouterRequestField(t, redirectFields, "request_id", requestID)
	if got, ok := redirectFields["response_bytes"].(int64); !ok || got < 0 || got != int64(response.Body.Len()) {
		t.Fatalf("redirect response_bytes = %#v, want exact non-negative body length %d", redirectFields["response_bytes"], response.Body.Len())
	}
	if got, ok := redirectFields["duration_ms"].(int64); !ok || got < 0 {
		t.Fatalf("redirect duration_ms = %#v, want non-negative int64", redirectFields["duration_ms"])
	}
	serialized := fmt.Sprint(redirectEntry.Message, redirectFields)
	for _, privateValue := range []string{"/api/v1/test-mutation/", "alice@example.com", payload} {
		if strings.Contains(serialized, privateValue) {
			t.Fatalf("redirect request event contains private value %q: %s", privateValue, serialized)
		}
	}

	replay := httptest.NewRequest(http.MethodPost, location, strings.NewReader(payload))
	replay.Header.Set("Content-Type", "text/plain")
	replay.Header.Set("Origin", origin)
	replay.Header.Set(telemetry.HeaderRequestID, requestID)
	replayResponse := httptest.NewRecorder()
	router.ServeHTTP(replayResponse, replay)

	if replayResponse.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d body=%q", replayResponse.Code, http.StatusOK, replayResponse.Body.String())
	}
	if got, want := replayResponse.Body.String(), "mutation-handled"; got != want {
		t.Fatalf("replay body = %q, want %q", got, want)
	}
	if handledErr != nil {
		t.Fatalf("canonical handler body read: %v", handledErr)
	}
	if handledMethod != http.MethodPost {
		t.Fatalf("canonical handler method = %q, want %q", handledMethod, http.MethodPost)
	}
	if handledQuery != "alice@example.com" {
		t.Fatalf("canonical handler query = %q, want %q", handledQuery, "alice@example.com")
	}
	if handledBody != payload {
		t.Fatalf("canonical handler body = %q, want %q", handledBody, payload)
	}
	replayEntries := observed.All()
	if len(replayEntries) != 2 {
		t.Fatalf("total request events after replay = %d, want 2", len(replayEntries))
	}
	replayFields := replayEntries[1].ContextMap()
	assertRouterRequestField(t, replayFields, "route", "/api/v1/test-mutation")
	assertRouterRequestField(t, replayFields, "method", http.MethodPost)
	assertRouterRequestField(t, replayFields, "status_class", "2xx")
	assertRouterRequestField(t, replayFields, "request_id", requestID)
}

func TestSetupRouterTelemetryReportsZeroBytesForStatusOnlyResponse(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	router := setupRouterWithRequestLogger(t, middleware.CORS(nil), zap.New(core))
	router.GET("/status-only", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/status-only", nil)
	request.Header.Set(telemetry.HeaderRequestID, "status-only-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", response.Body.String())
	}
	fields := singleRouterRequestEvent(t, observed)
	assertRouterRequestField(t, fields, "route", "/status-only")
	assertRouterRequestField(t, fields, "status_class", "2xx")
	if got, ok := fields["response_bytes"].(int64); !ok || got != 0 {
		t.Fatalf("response_bytes = %#v, want 0", fields["response_bytes"])
	}
}

func TestSetupRouterTelemetryReportsZeroBytesForEmbeddedHEAD(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>head-app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	restore := web.SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	core, observed := observer.New(zap.InfoLevel)
	router := setupRouterWithRequestLogger(t, middleware.CORS(nil), zap.New(core))
	request := httptest.NewRequest(http.MethodHead, "/repos/1", nil)
	request.Header.Set(telemetry.HeaderRequestID, "embedded-head-request")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", response.Body.String())
	}
	if got := response.Header().Get(telemetry.HeaderRequestID); got != "embedded-head-request" {
		t.Fatalf("%s = %q, want %q", telemetry.HeaderRequestID, got, "embedded-head-request")
	}
	fields := singleRouterRequestEvent(t, observed)
	assertRouterRequestField(t, fields, "route", "unmatched")
	assertRouterRequestField(t, fields, "method", http.MethodHead)
	assertRouterRequestField(t, fields, "status_class", "2xx")
	if got, ok := fields["response_bytes"].(int64); !ok || got != 0 {
		t.Fatalf("response_bytes = %#v, want 0", fields["response_bytes"])
	}
}

func setupRouterWithRequestLogger(t *testing.T, corsMiddleware gin.HandlerFunc, logger *zap.Logger) *gin.Engine {
	t.Helper()
	env := setupTestEnv(t)
	return SetupRouterWithOptions(
		env.client,
		nil,
		env.authSvc,
		repo.NewService(env.client, routerTestEncryptionKey, zap.NewNop()),
		webhook.NewHandler(env.client, nil, zap.NewNop()),
		nil,
		nil,
		routerTestEncryptionKey,
		"",
		corsMiddleware,
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterOptions{RequestLogger: logger, Release: "test-release"},
	)
}

func singleRouterRequestEvent(t *testing.T, observed *observer.ObservedLogs) map[string]interface{} {
	t.Helper()
	entries := observed.All()
	if len(entries) != 1 {
		t.Fatalf("request events = %d, want 1", len(entries))
	}
	if entries[0].Message != "http_request" {
		t.Fatalf("event message = %q, want %q", entries[0].Message, "http_request")
	}
	return entries[0].ContextMap()
}

func assertRouterRequestField(t *testing.T, fields map[string]interface{}, key, want string) {
	t.Helper()
	if got, ok := fields[key].(string); !ok || got != want {
		t.Fatalf("field %s = %#v, want %q", key, fields[key], want)
	}
}

func TestSetupRouterOAuthBrowserEntriesGzipAndHead(t *testing.T) {
	indexBody := []byte("<html><body>" + strings.Repeat("oauth browser app ", 64) + "</body></html>")
	restore := installRouterFrontendFixture(t, indexBody)
	defer restore()

	oauthServer := oauth.NewServer()
	oauthHandler := oauth.NewHandler(oauthServer, "http://localhost:18081", nil)
	env := setupTestEnvWithOAuth(t, oauthHandler)
	targets := []string{
		"http://localhost:18081/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&code_challenge=test-challenge&code_challenge_method=S256&state=test-state",
		"http://localhost:18081/oauth/device",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			get := performRouterFrontendRequest(env.router, http.MethodGet, target, nil, "", "")
			assertOAuthBrowserResponse(t, get, indexBody, false)

			head := performRouterFrontendRequest(env.router, http.MethodHead, target, nil, "", "")
			assertOAuthBrowserResponse(t, head, indexBody, true)
			if head.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
				t.Fatalf("HEAD Content-Length = %q, GET = %q", head.Header().Get("Content-Length"), get.Header().Get("Content-Length"))
			}
			if head.Header().Get("Content-Type") != get.Header().Get("Content-Type") {
				t.Fatalf("HEAD Content-Type = %q, GET = %q", head.Header().Get("Content-Type"), get.Header().Get("Content-Type"))
			}
		})
	}
}

func TestSetupRouterOAuthProtocolAndAPICacheIsolation(t *testing.T) {
	restore := installRouterFrontendFixture(t, []byte("<html><body>embedded app</body></html>"))
	defer restore()

	oauthServer := oauth.NewServer()
	oauthHandler := oauth.NewHandler(oauthServer, "http://localhost:18081", nil)
	env := setupTestEnvWithOAuth(t, oauthHandler)
	env.router.GET("/api/v1/frontend-policy-sentinel", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"policy": "handler"})
	})
	cases := []struct {
		name             string
		method           string
		target           string
		body             string
		contentType      string
		token            string
		wantStatus       int
		wantContentType  string
		wantBodyContains []string
		wantCacheControl string
	}{
		{name: "invalid authorize GET", method: http.MethodGet, target: "/oauth/authorize?response_type=token", wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, `"unsupported_response_type"`}},
		{name: "invalid authorize HEAD", method: http.MethodHead, target: "/oauth/authorize?response_type=token", wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, `"unsupported_response_type"`}},
		{name: "token", method: http.MethodPost, target: "/oauth/token", body: "grant_type=unsupported", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, `"unsupported_grant_type"`}},
		{name: "device code", method: http.MethodPost, target: "/oauth/device/code", body: "client_id=unknown", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, `"invalid_client"`}},
		{name: "authorize approval", method: http.MethodPost, target: "/oauth/authorize/approve", body: `{}`, contentType: "application/json", token: env.token, wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, "required"}},
		{name: "device approval", method: http.MethodPost, target: "/oauth/device/verify", body: `{}`, contentType: "application/json", token: env.token, wantStatus: http.StatusBadRequest, wantContentType: "application/json", wantBodyContains: []string{`"error"`, `"invalid_request"`}},
		{name: "public API", method: http.MethodGet, target: "/api/v1/health", wantStatus: http.StatusOK, wantContentType: "application/json", wantBodyContains: []string{`"status"`, `"ok"`}},
		{name: "authenticated API", method: http.MethodGet, target: "/api/v1/auth/me", token: env.token, wantStatus: http.StatusOK, wantContentType: "application/json", wantBodyContains: []string{`"code":200`, `"data"`}},
		{name: "handler cache policy", method: http.MethodGet, target: "/api/v1/frontend-policy-sentinel", wantStatus: http.StatusOK, wantContentType: "application/json", wantBodyContains: []string{`"policy":"handler"`}, wantCacheControl: "no-store"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := performRouterFrontendRequest(env.router, tc.method, tc.target, strings.NewReader(tc.body), tc.contentType, tc.token)
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.wantStatus, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); !strings.Contains(got, tc.wantContentType) {
				t.Fatalf("Content-Type = %q, want %q; body=%s", got, tc.wantContentType, response.Body.String())
			}
			if !json.Valid(response.Body.Bytes()) {
				t.Fatalf("body = %q, want valid JSON", response.Body.String())
			}
			for _, want := range tc.wantBodyContains {
				if !strings.Contains(response.Body.String(), want) {
					t.Fatalf("body = %q, want substring %q", response.Body.String(), want)
				}
			}
			if tc.wantCacheControl != "" {
				if got := response.Header().Get("Cache-Control"); got != tc.wantCacheControl {
					t.Fatalf("Cache-Control = %q, want preserved handler policy %q", got, tc.wantCacheControl)
				}
			}
			assertNoEmbeddedFrontendPolicy(t, response)
		})
	}
}

func installRouterFrontendFixture(t *testing.T, indexBody []byte) func() {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), indexBody, 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	return web.SetFrontendFSForTest(os.DirFS(root))
}

func performRouterFrontendRequest(router http.Handler, method, target string, body io.Reader, contentType, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Origin", "http://localhost:5173")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(response, request)
	return response
}

func assertOAuthBrowserResponse(t *testing.T, response *httptest.ResponseRecorder, wantBody []byte, head bool) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if got := response.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if !routerHeaderHasToken(response.Header().Values("Vary"), "Origin") || !routerHeaderHasToken(response.Header().Values("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Origin and Accept-Encoding", response.Header().Values("Vary"))
	}
	if response.Header().Get("Content-Length") == "" {
		t.Fatal("Content-Length is empty")
	}
	if head {
		if response.Body.Len() != 0 {
			t.Fatalf("HEAD body = %q, want empty", response.Body.String())
		}
		return
	}
	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decompressed, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll gzip: %v", err)
	}
	if !bytes.Equal(decompressed, wantBody) {
		t.Fatalf("decompressed body = %q, want %q", decompressed, wantBody)
	}
}

func assertNoEmbeddedFrontendPolicy(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if routerHeaderHasToken(response.Header().Values("Cache-Control"), "no-cache") || routerHeaderHasToken(response.Header().Values("Cache-Control"), "immutable") {
		t.Fatalf("Cache-Control = %q, must not contain embedded frontend policy", response.Header().Values("Cache-Control"))
	}
	if routerHeaderHasToken(response.Header().Values("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, must not contain Accept-Encoding", response.Header().Values("Vary"))
	}
}

func routerHeaderHasToken(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
