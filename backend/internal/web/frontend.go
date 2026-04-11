package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
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

func ServeEmbeddedFrontend() gin.HandlerFunc {
	dist, err := distFS()
	if err != nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}
	fileServer := http.FileServer(http.FS(dist))

	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path
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

		serveEmbeddedIndex(c, dist)
		c.Abort()
	}
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

func serveEmbeddedIndex(c *gin.Context, dist fs.FS) {
	f, err := dist.Open("index.html")
	if err != nil {
		c.Next()
		return
	}
	defer func() {
		_ = f.Close()
	}()

	data, err := io.ReadAll(f)
	if err != nil {
		c.Next()
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
