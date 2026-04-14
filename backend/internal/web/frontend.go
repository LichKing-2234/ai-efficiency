package web

import (
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var embeddedFrontendFS embed.FS

var frontendFS fs.FS = embeddedFrontendFS

func currentFrontendFS() fs.FS {
	return frontendFS
}

func SetFrontendFSForTest(fsys fs.FS) func() {
	prev := frontendFS
	frontendFS = fsys
	return func() {
		frontendFS = prev
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
	dist, err := distFS()
	if err != nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}
	fileServer := http.FileServer(http.FS(dist))

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

		if fileExists(dist, cleanPath) {
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		if serveEmbeddedIndex(c, fileServer) {
			c.Abort()
			return
		}
		c.Next()
	}
}

func ServeEmbeddedIndex(c *gin.Context) bool {
	dist, err := distFS()
	if err != nil {
		return false
	}
	fileServer := http.FileServer(http.FS(dist))
	return serveEmbeddedIndex(c, fileServer)
}

func distFS() (fs.FS, error) {
	return fs.Sub(frontendFS, "dist")
}

func fileExists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer func() {
		_ = f.Close()
	}()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func shouldBypassEmbeddedFrontend(requestPath string) bool {
	trimmed := strings.TrimSpace(requestPath)
	return trimmed == "/api" ||
		trimmed == "/oauth" ||
		strings.HasPrefix(trimmed, "/api/") ||
		strings.HasPrefix(trimmed, "/oauth/")
}

func serveEmbeddedIndex(c *gin.Context, fileServer http.Handler) bool {
	req := c.Request.Clone(c.Request.Context())
	req.URL = cloneURL(c.Request.URL)
	req.URL.Path = "/"
	req.URL.RawPath = ""
	req.RequestURI = "/"
	fileServer.ServeHTTP(c.Writer, req)
	return true
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{}
	}
	dup := *u
	return &dup
}

func canonicalRequestPath(requestPath string) string {
	cleanPath := path.Clean("/" + strings.TrimSpace(requestPath))
	if cleanPath == "." || cleanPath == "" {
		return "/"
	}
	return cleanPath
}
