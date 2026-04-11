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
		method     string
		path       string
		wantCode   int
		wantBody   string
		wantCTLike string
	}{
		{method: http.MethodGet, path: "/", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{method: http.MethodGet, path: "/repos/1", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{method: http.MethodHead, path: "/repos/1", wantCode: http.StatusOK, wantBody: "", wantCTLike: "text/html"},
		{method: http.MethodGet, path: "/assets/app.js", wantCode: http.StatusOK, wantBody: "console.log", wantCTLike: "javascript"},
		{method: http.MethodGet, path: "/assets", wantCode: http.StatusOK, wantBody: "<html>", wantCTLike: "text/html"},
		{method: http.MethodGet, path: "/api", wantCode: http.StatusNotFound, wantBody: "404 page not found", wantCTLike: "text/plain"},
		{method: http.MethodGet, path: "/oauth", wantCode: http.StatusNotFound, wantBody: "404 page not found", wantCTLike: "text/plain"},
		{method: http.MethodGet, path: "/api/v1/health", wantCode: http.StatusOK, wantBody: "ok", wantCTLike: "text/plain"},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		router.ServeHTTP(w, req)
		if w.Code != tc.wantCode {
			t.Fatalf("%s %s: status=%d want=%d body=%s", tc.method, tc.path, w.Code, tc.wantCode, w.Body.String())
		}
		if tc.wantBody == "" {
			if w.Body.Len() != 0 {
				t.Fatalf("%s %s: expected empty body, got %q", tc.method, tc.path, w.Body.String())
			}
		} else if !strings.Contains(w.Body.String(), tc.wantBody) {
			t.Fatalf("%s %s: body=%q missing %q", tc.method, tc.path, w.Body.String(), tc.wantBody)
		}
		if got := w.Header().Get("Content-Type"); !strings.Contains(got, tc.wantCTLike) {
			t.Fatalf("%s %s: content-type=%q want like %q", tc.method, tc.path, got, tc.wantCTLike)
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

func TestServeEmbeddedIndexUsesEmbeddedFrontendRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>index-root</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	restore := SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	router := gin.New()
	router.GET("/oauth/authorize", func(c *gin.Context) {
		if !ServeEmbeddedIndex(c) {
			c.String(http.StatusNotFound, "missing")
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "index-root") {
		t.Fatalf("body=%q", w.Body.String())
	}
}
