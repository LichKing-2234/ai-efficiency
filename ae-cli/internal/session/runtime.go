package session

import (
	"os"
	"path/filepath"
	"strings"
)

func runtimeDir(sessionID string) string {
	root := RuntimeRootDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, strings.TrimSpace(sessionID))
}

func RuntimeRootDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ae-cli", "runtime")
}

func RuntimeCollectorsDir(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "collectors")
}
