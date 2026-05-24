package attributionlocal

import (
	"path/filepath"
	"strings"
)

func sameWorkspacePath(a, b string) bool {
	left := canonicalWorkspacePath(a)
	right := canonicalWorkspacePath(b)
	return left != "" && left == right
}

func canonicalWorkspacePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs)
}
