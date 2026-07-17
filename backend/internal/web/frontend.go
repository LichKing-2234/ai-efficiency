package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var embeddedFrontendFS embed.FS

var frontendState = struct {
	sync.Mutex
	fsys   fs.FS
	server *frontendServer
}{fsys: embeddedFrontendFS}

var hashedFrontendAssetRE = regexp.MustCompile(`^assets/.+-[A-Za-z0-9_-]{8,}\.[^/]+$`)

type frontendServer struct {
	dist  fs.FS
	mu    sync.Mutex
	files map[string]*frontendFile
}

type frontendFile struct {
	name        string
	raw         []byte
	contentType string
	compress    bool
	gzipOnce    sync.Once
	gzipBytes   []byte
	gzipErr     error
}

func currentFrontendFS() fs.FS {
	frontendState.Lock()
	defer frontendState.Unlock()
	return frontendState.fsys
}

func SetFrontendFSForTest(fsys fs.FS) func() {
	frontendState.Lock()
	prevFS := frontendState.fsys
	prevServer := frontendState.server
	frontendState.fsys = fsys
	frontendState.server = nil
	frontendState.Unlock()
	return func() {
		frontendState.Lock()
		frontendState.fsys = prevFS
		frontendState.server = prevServer
		frontendState.Unlock()
	}
}

func HasEmbeddedFrontend() bool {
	dist, err := distFS()
	if err != nil {
		return false
	}
	f, err := dist.Open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func RedirectCanonicalBrowserPath() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil || c.Request.URL == nil {
			c.Next()
			return
		}

		canonicalPath := canonicalRequestPath(c.Request.URL.Path)
		if canonicalPath != c.Request.URL.Path &&
			(c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
			target := canonicalPath
			if c.Request.URL.RawQuery != "" {
				target += "?" + c.Request.URL.RawQuery
			}
			c.Redirect(http.StatusTemporaryRedirect, target)
			c.Abort()
			return
		}

		c.Next()
	}
}

func ServeEmbeddedFrontend() gin.HandlerFunc {
	server, err := currentFrontendServer()
	if err != nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		requestPath := canonicalRequestPath(c.Request.URL.Path)
		if shouldBypassEmbeddedFrontend(requestPath) {
			c.Next()
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		if requestPath == "/index.html" && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) {
			location := "./"
			if c.Request.URL.RawQuery != "" {
				location += "?" + c.Request.URL.RawQuery
			}
			c.Header("Location", location)
			c.Status(http.StatusMovedPermanently)
			c.Abort()
			return
		}

		file, _, err := server.resolve(cleanPath)
		if err == nil {
			server.serve(c, file)
			c.Abort()
			return
		}
		c.Next()
	}
}

func ServeEmbeddedIndex(c *gin.Context) bool {
	server, err := currentFrontendServer()
	if err != nil {
		return false
	}
	file, err := server.file("index.html")
	if err != nil {
		return false
	}
	server.serve(c, file)
	return true
}

func distFS() (fs.FS, error) {
	return fs.Sub(currentFrontendFS(), "dist")
}

func currentFrontendServer() (*frontendServer, error) {
	frontendState.Lock()
	defer frontendState.Unlock()
	if frontendState.server != nil {
		return frontendState.server, nil
	}
	dist, err := fs.Sub(frontendState.fsys, "dist")
	if err != nil {
		return nil, err
	}
	frontendState.server = newFrontendServer(dist)
	return frontendState.server, nil
}

func newFrontendServer(dist fs.FS) *frontendServer {
	return &frontendServer{dist: dist, files: make(map[string]*frontendFile)}
}

func (s *frontendServer) resolve(name string) (*frontendFile, bool, error) {
	file, err := s.file(name)
	if err == nil {
		return file, false, nil
	}
	file, err = s.file("index.html")
	return file, true, err
}

func (s *frontendServer) file(name string) (*frontendFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if file := s.files[name]; file != nil {
		return file, nil
	}

	info, err := fs.Stat(s.dist, name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	raw, err := fs.ReadFile(s.dist, name)
	if err != nil {
		return nil, err
	}
	contentType := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if contentType == "" {
		contentType = http.DetectContentType(raw)
	}
	file := &frontendFile{
		name:        name,
		raw:         raw,
		contentType: contentType,
		compress:    compressibleFrontendFile(name),
	}
	s.files[name] = file
	return file, nil
}

func (s *frontendServer) serve(c *gin.Context, file *frontendFile) {
	selected := file.raw
	encoding := ""
	if file.compress {
		appendVary(c.Writer.Header(), "Accept-Encoding")
		if acceptsGzip(strings.Join(c.Request.Header.Values("Accept-Encoding"), ",")) {
			if compressed, err := file.gzipRepresentation(); err == nil {
				selected = compressed
				encoding = "gzip"
			}
		}
	}

	header := c.Writer.Header()
	header.Set("Content-Type", file.contentType)
	header.Set("Content-Length", strconv.Itoa(len(selected)))
	header.Set("Cache-Control", frontendCacheControl(file.name))
	header.Del("Last-Modified")
	if encoding == "" {
		header.Del("Content-Encoding")
	} else {
		header.Set("Content-Encoding", encoding)
	}
	http.ServeContent(c.Writer, c.Request, path.Base(file.name), time.Time{}, bytes.NewReader(selected))
}

func (f *frontendFile) gzipRepresentation() ([]byte, error) {
	f.gzipOnce.Do(func() {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write(f.raw); err != nil {
			f.gzipErr = err
			return
		}
		if err := writer.Close(); err != nil {
			f.gzipErr = err
			return
		}
		f.gzipBytes = buffer.Bytes()
	})
	return f.gzipBytes, f.gzipErr
}

func compressibleFrontendFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".js", ".mjs", ".css", ".json", ".svg":
		return true
	default:
		return false
	}
}

func frontendCacheControl(name string) string {
	if hashedFrontendAssetRE.MatchString(name) {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

func acceptsGzip(value string) bool {
	gzipQuality := -1.0
	wildcardQuality := -1.0
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name == "" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, raw, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil || parsed < 0 || parsed > 1 {
				quality = 0
			} else {
				quality = parsed
			}
		}
		switch name {
		case "gzip":
			gzipQuality = quality
		case "*":
			wildcardQuality = quality
		}
	}
	if gzipQuality >= 0 {
		return gzipQuality > 0
	}
	return wildcardQuality > 0
}

func appendVary(header http.Header, token string) {
	values := header.Values("Vary")
	tokens := make([]string, 0, len(values)+1)
	found := false
	for _, value := range values {
		for _, existing := range strings.Split(value, ",") {
			existing = strings.TrimSpace(existing)
			if existing == "" {
				continue
			}
			if existing == "*" {
				return
			}
			if strings.EqualFold(existing, token) {
				found = true
			}
			duplicate := false
			for _, current := range tokens {
				if strings.EqualFold(current, existing) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				tokens = append(tokens, existing)
			}
		}
	}
	if !found {
		tokens = append(tokens, token)
	}
	header.Del("Vary")
	header.Set("Vary", strings.Join(tokens, ", "))
}

func shouldBypassEmbeddedFrontend(requestPath string) bool {
	trimmed := strings.TrimSpace(requestPath)
	return trimmed == "/api" ||
		trimmed == "/oauth" ||
		strings.HasPrefix(trimmed, "/api/") ||
		strings.HasPrefix(trimmed, "/oauth/")
}

func canonicalRequestPath(requestPath string) string {
	cleanPath := path.Clean("/" + strings.TrimSpace(requestPath))
	if cleanPath == "." || cleanPath == "" {
		return "/"
	}
	return cleanPath
}
