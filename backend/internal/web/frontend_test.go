package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHasEmbeddedFrontendAndMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist", "assets"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("WriteFile asset: %v", err)
	}

	restore := SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	if !HasEmbeddedFrontend() {
		t.Fatal("expected embedded frontend to be detected")
	}

	router := gin.New()
	router.Use(ServeEmbeddedFrontend())
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	cases := []struct {
		path       string
		wantCode   int
		wantBody   string
		wantCTLike string
	}{
		{path: "/", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{path: "/repos/1", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{path: "/assets/app.js", wantCode: http.StatusOK, wantBody: "console.log", wantCTLike: "text/javascript"},
		{path: "/assets", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{path: "/api", wantCode: http.StatusNotFound, wantBody: "404 page not found", wantCTLike: "text/plain"},
		{path: "/oauth", wantCode: http.StatusNotFound, wantBody: "404 page not found", wantCTLike: "text/plain"},
		{path: "/api/v1/health", wantCode: http.StatusOK, wantBody: "ok", wantCTLike: "text/plain"},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		router.ServeHTTP(w, req)
		if w.Code != tc.wantCode {
			t.Fatalf("%s: status=%d want=%d body=%s", tc.path, w.Code, tc.wantCode, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tc.wantBody) {
			t.Fatalf("%s: body=%q missing %q", tc.path, w.Body.String(), tc.wantBody)
		}
		if got := w.Header().Get("Content-Type"); !strings.Contains(got, tc.wantCTLike) {
			t.Fatalf("%s: content-type=%q want like %q", tc.path, got, tc.wantCTLike)
		}
	}
}

func TestHasEmbeddedFrontendFalseWithoutIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	restore := SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	if HasEmbeddedFrontend() {
		t.Fatal("expected embedded frontend detection to be false")
	}
}

func TestSetFrontendFSForTestRestoresPreviousFS(t *testing.T) {
	restore := SetFrontendFSForTest(os.DirFS(t.TempDir()))
	restore()

	if currentFrontendFS() == nil {
		t.Fatal("expected current frontend fs to be restored")
	}
}
