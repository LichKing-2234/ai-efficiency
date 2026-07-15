package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/ai-efficiency/backend/internal/webhook"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

	env := setupTestEnv(t)
	core, observed := observer.New(zap.InfoLevel)
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
