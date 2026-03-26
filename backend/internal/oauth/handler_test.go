package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/oauth"
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
