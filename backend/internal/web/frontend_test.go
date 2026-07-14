package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

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

func TestEmbeddedFrontendNegotiatesGzipAndCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	indexBody := []byte("<html><body>" + strings.Repeat("embedded-app-shell ", 32) + "</body></html>")
	files := map[string][]byte{
		"index.html":                indexBody,
		"assets/app-ABCDEFGH.js":    []byte(strings.Repeat("console.log('gzip');\n", 32)),
		"assets/app-IJKLMNOP.css":   []byte(strings.Repeat(".card { display: block; }\n", 32)),
		"assets/data-QRSTUVWX.json": []byte(`{"items":["alpha","beta","gamma"],"padding":"` + strings.Repeat("x", 256) + `"}`),
		"assets/icon-YZabcdef.svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>` + strings.Repeat("icon", 64) + `</text></svg>`),
		"assets/plain.js":           []byte(strings.Repeat("console.log('plain');\n", 16)),
		"assets/image-12345678.png": []byte("synthetic-png-bytes"),
	}
	router := frontendPolicyTestRouter(t, files)

	cases := []struct {
		path        string
		file        string
		contentType string
		cache       string
	}{
		{path: "/", file: "index.html", contentType: "text/html", cache: "no-cache"},
		{path: "/assets/app-ABCDEFGH.js", file: "assets/app-ABCDEFGH.js", contentType: "javascript", cache: "public, max-age=31536000, immutable"},
		{path: "/assets/app-IJKLMNOP.css", file: "assets/app-IJKLMNOP.css", contentType: "text/css", cache: "public, max-age=31536000, immutable"},
		{path: "/assets/data-QRSTUVWX.json", file: "assets/data-QRSTUVWX.json", contentType: "application/json", cache: "public, max-age=31536000, immutable"},
		{path: "/assets/icon-YZabcdef.svg", file: "assets/icon-YZabcdef.svg", contentType: "image/svg+xml", cache: "public, max-age=31536000, immutable"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			identity := performFrontendRequest(router, http.MethodGet, tc.path, "br, *;q=1, gzip;q=0")
			assertFrontendResponseHeaders(t, identity, tc.contentType, tc.cache, "", len(files[tc.file]))
			if got := identity.Body.Bytes(); string(got) != string(files[tc.file]) {
				t.Fatalf("identity body mismatch: got %d bytes want %d", len(got), len(files[tc.file]))
			}

			compressed := performFrontendRequest(router, http.MethodGet, tc.path, "br, GZip;q=0.8")
			assertFrontendResponseHeaders(t, compressed, tc.contentType, tc.cache, "gzip", compressed.Body.Len())
			if got := gunzipResponse(t, compressed); string(got) != string(files[tc.file]) {
				t.Fatalf("gzip body mismatch: got %d bytes want %d", len(got), len(files[tc.file]))
			}

			head := performFrontendRequest(router, http.MethodHead, tc.path, "gzip")
			assertFrontendResponseHeaders(t, head, tc.contentType, tc.cache, "gzip", compressed.Body.Len())
			if head.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q, want empty", head.Body.String())
			}
		})
	}
}

func TestEmbeddedFrontendClassifiesTheResolvedFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	indexBody := []byte("<html><body>fallback</body></html>")
	files := map[string][]byte{
		"index.html":                indexBody,
		"assets/app-ABCDEFGH.js":    []byte("console.log('hashed')"),
		"assets/plain.js":           []byte("console.log('plain')"),
		"assets/image-12345678.png": []byte("synthetic-png-bytes"),
	}
	router := frontendPolicyTestRouter(t, files)

	cases := []struct {
		path        string
		contentType string
		cache       string
		body        []byte
		varyGzip    bool
	}{
		{path: "/assets/app-ABCDEFGH.js", contentType: "javascript", cache: "public, max-age=31536000, immutable", body: files["assets/app-ABCDEFGH.js"], varyGzip: true},
		{path: "/assets/plain.js", contentType: "javascript", cache: "no-cache", body: files["assets/plain.js"], varyGzip: true},
		{path: "/nested/route", contentType: "text/html", cache: "no-cache", body: indexBody, varyGzip: true},
		{path: "/assets/missing-ABCDEFGH.js", contentType: "text/html", cache: "no-cache", body: indexBody, varyGzip: true},
		{path: "/assets", contentType: "text/html", cache: "no-cache", body: indexBody, varyGzip: true},
		{path: "/assets/image-12345678.png", contentType: "image/png", cache: "public, max-age=31536000, immutable", body: files["assets/image-12345678.png"], varyGzip: false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := performFrontendRequest(router, http.MethodGet, tc.path, "gzip")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); !strings.Contains(got, tc.contentType) {
				t.Fatalf("Content-Type = %q, want like %q", got, tc.contentType)
			}
			if got := response.Header().Get("Cache-Control"); got != tc.cache {
				t.Fatalf("Cache-Control = %q, want %q", got, tc.cache)
			}
			if got := testHeaderHasToken(response.Header().Values("Vary"), "Accept-Encoding"); got != tc.varyGzip {
				t.Fatalf("Vary Accept-Encoding = %v, want %v; values=%q", got, tc.varyGzip, response.Header().Values("Vary"))
			}
			body := response.Body.Bytes()
			if response.Header().Get("Content-Encoding") == "gzip" {
				body = gunzipResponse(t, response)
			}
			if string(body) != string(tc.body) {
				t.Fatalf("body = %q, want %q", body, tc.body)
			}
		})
	}

	redirect := performFrontendRequest(router, http.MethodGet, "/index.html", "gzip")
	if redirect.Code != http.StatusMovedPermanently || redirect.Header().Get("Location") != "./" {
		t.Fatalf("/index.html = status %d location %q, want 301 ./", redirect.Code, redirect.Header().Get("Location"))
	}

	api := performFrontendRequest(router, http.MethodGet, "/api/v1/health", "gzip")
	if api.Code != http.StatusOK || api.Body.String() != "ok" {
		t.Fatalf("API response = status %d body %q", api.Code, api.Body.String())
	}
	if got := api.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("API Cache-Control = %q, want empty", got)
	}
	if testHeaderHasToken(api.Header().Values("Vary"), "Accept-Encoding") {
		t.Fatalf("API Vary = %q, must not include Accept-Encoding", api.Header().Values("Vary"))
	}
}

func TestFrontendServerResolveReportsFallback(t *testing.T) {
	server := newFrontendServer(fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte("<html>app</html>"), Mode: 0o444},
		"assets":                 &fstest.MapFile{Mode: fs.ModeDir | 0o555},
		"assets/app-ABCDEFGH.js": &fstest.MapFile{Data: []byte("console.log('app')"), Mode: 0o444},
	})

	cases := []struct {
		name         string
		requested    string
		wantFile     string
		wantFallback bool
	}{
		{name: "direct regular file", requested: "assets/app-ABCDEFGH.js", wantFile: "assets/app-ABCDEFGH.js"},
		{name: "direct index", requested: "index.html", wantFile: "index.html"},
		{name: "directory", requested: "assets", wantFile: "index.html", wantFallback: true},
		{name: "missing file", requested: "missing/route", wantFile: "index.html", wantFallback: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, fallback, err := server.resolve(tc.requested)
			if err != nil {
				t.Fatalf("resolve(%q): %v", tc.requested, err)
			}
			if file.name != tc.wantFile {
				t.Fatalf("resolve(%q) file = %q, want %q", tc.requested, file.name, tc.wantFile)
			}
			if fallback != tc.wantFallback {
				t.Fatalf("resolve(%q) fallback = %v, want %v", tc.requested, fallback, tc.wantFallback)
			}
		})
	}
}

func TestEmbeddedFrontendIndexRedirectPreservesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := frontendPolicyTestRouter(t, map[string][]byte{
		"index.html": []byte("<html><body>app</body></html>"),
	})

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := performFrontendRequest(router, method, "/index.html?source=bookmark&next=%2Frepos", "gzip")
			if response.Code != http.StatusMovedPermanently {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMovedPermanently)
			}
			if got, want := response.Header().Get("Location"), "./?source=bookmark&next=%2Frepos"; got != want {
				t.Fatalf("Location = %q, want %q", got, want)
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q, want empty", response.Body.String())
			}
		})
	}
}

func TestEmbeddedFrontendGzipRangeUsesSelectedRepresentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(strings.Repeat("console.log('range');\n", 64))
	router := frontendPolicyTestRouter(t, map[string][]byte{
		"index.html":             []byte("<html><body>app</body></html>"),
		"assets/app-ABCDEFGH.js": body,
	})

	full := performFrontendRequest(router, http.MethodGet, "/assets/app-ABCDEFGH.js", "gzip")
	if full.Code != http.StatusOK || full.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("full response = status %d encoding %q", full.Code, full.Header().Get("Content-Encoding"))
	}
	const rangeStart, rangeEnd = 3, 17
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/app-ABCDEFGH.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Range", "bytes=3-17")
	router.ServeHTTP(response, request)

	if response.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusPartialContent, response.Body.String())
	}
	if got := response.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got, want := response.Header().Get("Content-Length"), strconv.Itoa(rangeEnd-rangeStart+1); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got, want := response.Header().Get("Content-Range"), "bytes 3-17/"+strconv.Itoa(full.Body.Len()); got != want {
		t.Fatalf("Content-Range = %q, want %q", got, want)
	}
	if got, want := response.Body.Bytes(), full.Body.Bytes()[rangeStart:rangeEnd+1]; !bytes.Equal(got, want) {
		t.Fatalf("partial body = %x, want %x", got, want)
	}
}

func TestEmbeddedFrontendSelectsGzipOnlyWhenAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(strings.Repeat("console.log('encoding');\n", 32))
	router := frontendPolicyTestRouter(t, map[string][]byte{
		"index.html":             []byte("<html><body>app</body></html>"),
		"assets/app-ABCDEFGH.js": body,
	})

	cases := []struct {
		name           string
		acceptEncoding string
		wantGzip       bool
	}{
		{name: "case insensitive", acceptEncoding: "br, GZip;q=0.8", wantGzip: true},
		{name: "wildcard", acceptEncoding: "br, *;q=0.5", wantGzip: true},
		{name: "explicit exclusion beats wildcard", acceptEncoding: "*;q=1, gzip;q=0", wantGzip: false},
		{name: "no accepted encoding", acceptEncoding: "br", wantGzip: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := performFrontendRequest(router, http.MethodGet, "/assets/app-ABCDEFGH.js", tc.acceptEncoding)
			gotGzip := response.Header().Get("Content-Encoding") == "gzip"
			if gotGzip != tc.wantGzip {
				t.Fatalf("Content-Encoding = %q, want gzip=%v", response.Header().Get("Content-Encoding"), tc.wantGzip)
			}
			gotBody := response.Body.Bytes()
			if gotGzip {
				gotBody = gunzipResponse(t, response)
			}
			if string(gotBody) != string(body) {
				t.Fatalf("body mismatch: got %d bytes want %d", len(gotBody), len(body))
			}
		})
	}
}

func frontendPolicyTestRouter(t *testing.T, files map[string][]byte) *gin.Engine {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		fullPath := filepath.Join(root, "dist", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(fullPath, body, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	restore := SetFrontendFSForTest(os.DirFS(root))
	t.Cleanup(restore)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("Vary", "Origin")
		c.Next()
	})
	router.Use(ServeEmbeddedFrontend())
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

func performFrontendRequest(router http.Handler, method, target, acceptEncoding string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	router.ServeHTTP(response, request)
	return response
}

func assertFrontendResponseHeaders(t *testing.T, response *httptest.ResponseRecorder, contentType, cache, encoding string, contentLength int) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, contentType) {
		t.Fatalf("Content-Type = %q, want like %q", got, contentType)
	}
	if got := response.Header().Get("Cache-Control"); got != cache {
		t.Fatalf("Cache-Control = %q, want %q", got, cache)
	}
	if got := response.Header().Get("Content-Encoding"); got != encoding {
		t.Fatalf("Content-Encoding = %q, want %q", got, encoding)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(contentLength) {
		t.Fatalf("Content-Length = %q, want %d", got, contentLength)
	}
	if got := response.Header().Get("Last-Modified"); got != "" {
		t.Fatalf("Last-Modified = %q, want empty", got)
	}
	if !testHeaderHasToken(response.Header().Values("Vary"), "Origin") || !testHeaderHasToken(response.Header().Values("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Origin and Accept-Encoding", response.Header().Values("Vary"))
	}
}

func gunzipResponse(t *testing.T, response *httptest.ResponseRecorder) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll gzip: %v", err)
	}
	return body
}

func testHeaderHasToken(values []string, token string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func TestHasEmbeddedFrontendForReleaseBuilds(t *testing.T) {
	if os.Getenv("AE_ASSERT_EMBEDDED_FRONTEND") != "1" {
		t.Skip("set AE_ASSERT_EMBEDDED_FRONTEND=1 to assert embedded frontend presence")
	}

	if !HasEmbeddedFrontend() {
		t.Fatal("expected embedded frontend bundle to include index.html")
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

func TestServeEmbeddedIndexReusesRepresentationsAcrossRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte("<html><body>" + strings.Repeat("oauth-app-shell ", 32) + "</body></html>")
	counted := &countingFS{
		FS: fstest.MapFS{
			"dist/index.html": &fstest.MapFile{Data: body, Mode: 0o444},
		},
		opens: make(map[string]int),
	}
	restore := SetFrontendFSForTest(counted)
	defer restore()

	router := gin.New()
	router.GET("/oauth/authorize", func(c *gin.Context) {
		if !ServeEmbeddedIndex(c) {
			c.String(http.StatusNotFound, "missing")
		}
	})

	identity := performFrontendRequest(router, http.MethodGet, "/oauth/authorize", "")
	if identity.Code != http.StatusOK || !bytes.Equal(identity.Body.Bytes(), body) {
		t.Fatalf("identity = status %d body %q", identity.Code, identity.Body.String())
	}
	if got := identity.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("identity Content-Length = %q, want %d", got, len(body))
	}
	firstOpenCount := counted.openCount("dist/index.html")
	if firstOpenCount == 0 {
		t.Fatal("expected the first request to open dist/index.html")
	}

	compressed := performFrontendRequest(router, http.MethodGet, "/oauth/authorize", "gzip")
	if compressed.Code != http.StatusOK || compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip = status %d encoding %q", compressed.Code, compressed.Header().Get("Content-Encoding"))
	}
	if got := gunzipResponse(t, compressed); !bytes.Equal(got, body) {
		t.Fatalf("gzip body = %q, want %q", got, body)
	}
	if got := counted.openCount("dist/index.html"); got != firstOpenCount {
		t.Fatalf("dist/index.html open count = %d after second request, want cached count %d", got, firstOpenCount)
	}
}

func TestServeEmbeddedIndexTracksFrontendFSTestReplacement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	first := fstest.MapFS{
		"dist/index.html": &fstest.MapFile{Data: []byte("<html>first</html>"), Mode: 0o444},
	}
	second := fstest.MapFS{
		"dist/index.html": &fstest.MapFile{Data: []byte("<html>second</html>"), Mode: 0o444},
	}
	restoreFirst := SetFrontendFSForTest(first)
	defer restoreFirst()

	serve := func() string {
		router := gin.New()
		router.GET("/oauth/device", func(c *gin.Context) {
			if !ServeEmbeddedIndex(c) {
				c.String(http.StatusNotFound, "missing")
			}
		})
		return performFrontendRequest(router, http.MethodGet, "/oauth/device", "").Body.String()
	}

	if got := serve(); got != "<html>first</html>" {
		t.Fatalf("first body = %q", got)
	}
	restoreSecond := SetFrontendFSForTest(second)
	if got := serve(); got != "<html>second</html>" {
		t.Fatalf("second body = %q", got)
	}
	restoreSecond()
	if got := serve(); got != "<html>first</html>" {
		t.Fatalf("restored body = %q", got)
	}
}

type countingFS struct {
	fs.FS
	mu    sync.Mutex
	opens map[string]int
}

func (f *countingFS) Open(name string) (fs.File, error) {
	f.mu.Lock()
	f.opens[name]++
	f.mu.Unlock()
	return f.FS.Open(name)
}

func (f *countingFS) openCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opens[name]
}

func TestCanonicalPathRedirectsExtraSlashBrowserRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.RemoveExtraSlash = true
	router.Use(RedirectCanonicalBrowserPath())
	router.GET("/oauth/authorize", func(c *gin.Context) {
		c.String(http.StatusOK, "oauth-authorize")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"//oauth/authorize?response_type=code&client_id=ae-cli",
		nil,
	)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d want=%d body=%s", w.Code, http.StatusTemporaryRedirect, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/oauth/authorize?response_type=code&client_id=ae-cli" {
		t.Fatalf("location=%q", loc)
	}
}

func TestServeEmbeddedFrontendBypassesNormalizedOAuthTokenPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("<html><body>app</body></html>"), 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	restore := SetFrontendFSForTest(os.DirFS(root))
	defer restore()

	router := gin.New()
	router.RemoveExtraSlash = true
	router.Use(ServeEmbeddedFrontend())
	router.POST("/oauth/token", func(c *gin.Context) {
		c.String(http.StatusOK, "token")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "//oauth/token", strings.NewReader("grant_type=authorization_code"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != "token" {
		t.Fatalf("body=%q", w.Body.String())
	}
}
