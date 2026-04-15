package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/gin-gonic/gin"
)

type mockTokenGen struct{}

func (m *mockTokenGen) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	return "test-access-token", "test-refresh-token", 7200, nil
}

func setupTestRouter() (*gin.Engine, *oauth.Handler) {
	gin.SetMode(gin.TestMode)
	oauthServer := oauth.NewServer()
	handler := oauth.NewHandler(oauthServer, "http://localhost:5173", &mockTokenGen{})

	r := gin.New()
	r.GET("/oauth/authorize", handler.Authorize)
	r.GET("/oauth/device", handler.DevicePage)
	r.POST("/oauth/device/code", handler.DeviceCode)
	r.POST("/oauth/token", handler.Token)
	return r, handler
}

func TestAuthorizeRedirectsToFrontend(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}
	if !strings.Contains(loc, "localhost:5173/oauth/authorize") {
		t.Fatalf("expected redirect to frontend, got: %s", loc)
	}
}

func TestAuthorizeServesEmbeddedFrontendWhenFrontendURLMatchesRequestOrigin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>oauth-app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	restore := web.SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	gin.SetMode(gin.TestMode)
	oauthServer := oauth.NewServer()
	handler := oauth.NewHandler(oauthServer, "http://localhost:18081", &mockTokenGen{})

	r := gin.New()
	r.GET("/oauth/authorize", handler.Authorize)

	req := httptest.NewRequest(http.MethodGet, "http://localhost:18081/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "oauth-app") {
		t.Fatalf("expected embedded frontend body, got: %s", w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no redirect location, got: %s", loc)
	}
}

func TestAuthorizeRejectsInvalidRedirectURI(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://evil.com/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthorizeRejectsUnknownClient(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=unknown&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthorizeRejectsUnsupportedResponseType(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=token&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&code_challenge=abc&code_challenge_method=S256&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthorizeRejectsMissingCodeChallenge(t *testing.T) {
	r, _ := setupTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?response_type=code&client_id=ae-cli&redirect_uri=http://localhost:18234/callback&state=xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

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
