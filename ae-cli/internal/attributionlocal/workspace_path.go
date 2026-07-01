package attributionlocal

import (
	"os"
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

func sameWorkspacePathOrGitCommon(a, b string) bool {
	if sameWorkspacePath(a, b) {
		return true
	}
	left := gitCommonDirForPath(a)
	right := gitCommonDirForPath(b)
	return left != "" && left == right
}

func gitCommonDirForPath(path string) string {
	current := canonicalWorkspacePath(path)
	if current == "" {
		return ""
	}
	for {
		gitDir, err := resolveWorkspaceGitDir(current)
		if err == nil {
			return canonicalWorkspacePath(resolveGitCommonDir(gitDir))
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func resolveGitCommonDir(gitDir string) string {
	commonDir := gitDir
	commonPath := filepath.Join(gitDir, "commondir")
	if data, err := os.ReadFile(commonPath); err == nil {
		rel := strings.TrimSpace(string(data))
		if rel != "" {
			if filepath.IsAbs(rel) {
				commonDir = rel
			} else {
				commonDir = filepath.Join(gitDir, rel)
			}
		}
	}
	return commonDir
}
