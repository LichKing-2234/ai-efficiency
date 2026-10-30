package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encoding/json"
	"github.com/ai-efficiency/backend/internal/web"
)

func TestHCIBootstrapIsJSONWithEmbeddedFrontend(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "dist"), 0755)
	os.WriteFile(filepath.Join(root, "dist/index.html"), []byte("<html>platform-login-form</html>"), 0644)
	restore := web.SetFrontendFSForTest(os.DirFS(root))
	defer restore()
	env := setupFullTestEnv(t)
	if err := env.authSvc.ConfigureHCI(true, "", strings.Repeat("p", 32), "http://localhost:5173"); err != nil {
		t.Fatal(err)
	}
	r := env.router
	req := httptest.NewRequest(http.MethodPost, "/auth/hci/bootstrap", nil)
	req.Header.Set("x-oauth-email", "alice@example.com")
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AE-HCI-Proxy-Secret", strings.Repeat("p", 32))
	req.Header.Set("X-Consumer-Username", "alice@example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("bootstrap returned page instead of identity: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session response must not be cached")
	}
	if w.Code != 200 {
		t.Fatalf("bootstrap failed: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+payload.Data.Tokens.AccessToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alice@example.com") {
		t.Fatalf("minted session cannot read own identity: %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("X-Oauth-Email", "alice@example.com")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("ordinary API accepted HCI header without Bearer")
	}

}
