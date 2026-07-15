package handler

import (
	"context"
	"fmt"
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
