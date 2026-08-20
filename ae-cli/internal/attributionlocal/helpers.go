package attributionlocal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func isPatchTool(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	return name == "apply_patch" || strings.HasSuffix(name, ".apply_patch") || strings.HasSuffix(name, "__apply_patch")
}

// StableCommitPatchID returns Git's stable patch identity without retaining
// source or diff content.
func StableCommitPatchID(ctx context.Context, repoRoot, commitSHA string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	commitSHA = strings.TrimSpace(commitSHA)
	if repoRoot == "" || commitSHA == "" {
		return ""
	}
	show := exec.CommandContext(ctx, "git", "-C", repoRoot, "show", "--pretty=format:", "--no-ext-diff", commitSHA)
	patch, err := show.Output()
	if err != nil || len(bytes.TrimSpace(patch)) == 0 {
		return ""
	}
	patchID := exec.CommandContext(ctx, "git", "patch-id", "--stable")
	patchID.Stdin = bytes.NewReader(patch)
	output, err := patchID.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func withStateFileLock(ctx context.Context, lockPath, busyMessage string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	for attempt := 0; attempt < 200; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(lockPath)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s", busyMessage)
}
