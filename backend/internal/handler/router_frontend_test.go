package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/web"
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
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "router-app") {
		t.Fatalf("body=%q", w.Body.String())
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
	cases := []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		token       string
	}{
		{name: "invalid authorize", method: http.MethodGet, target: "/oauth/authorize?response_type=token"},
		{name: "token", method: http.MethodPost, target: "/oauth/token", body: "grant_type=unsupported", contentType: "application/x-www-form-urlencoded"},
		{name: "device code", method: http.MethodPost, target: "/oauth/device/code", body: "client_id=unknown", contentType: "application/x-www-form-urlencoded"},
		{name: "authorize approval", method: http.MethodPost, target: "/oauth/authorize/approve", body: `{}`, contentType: "application/json", token: env.token},
		{name: "device approval", method: http.MethodPost, target: "/oauth/device/verify", body: `{}`, contentType: "application/json", token: env.token},
		{name: "public API", method: http.MethodGet, target: "/api/v1/health"},
		{name: "authenticated API", method: http.MethodGet, target: "/api/v1/auth/me", token: env.token},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := performRouterFrontendRequest(env.router, tc.method, tc.target, strings.NewReader(tc.body), tc.contentType, tc.token)
			if response.Code == http.StatusNotFound {
				t.Fatalf("status = 404, route was not exercised; body=%s", response.Body.String())
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
	if got := response.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("Cache-Control = %q, want empty", got)
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
