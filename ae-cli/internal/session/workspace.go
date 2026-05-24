package session

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var workspaceNamespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("ae-workspace"))

func canonicalAbsPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}

	// Make absolute before EvalSymlinks to avoid depending on current working dir.
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("abs %q: %w", p, err)
	}
	abs = filepath.Clean(abs)

	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("eval symlinks %q: %w", abs, err)
	}
	real = filepath.Clean(real)
	real, err = filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("abs(real) %q: %w", real, err)
	}
	return real, nil
}

// deriveWorkspaceID produces a stable UUIDv5 derived from the canonical git context.
//
// Design doc formula:
// UUIDv5("ae-workspace", canonical_repo_root + "\x1f" + canonical_workspace_root + "\x1f" +
// canonical_git_dir + "\x1f" + canonical_git_common_dir)
//
// For ae-cli we treat repo_root as the workspace root (git toplevel). For linked worktrees,
// workspace_root differs across worktrees while git_common_dir can stay shared, which is
// sufficient to differentiate workspaces.
func deriveWorkspaceID(repoRoot, workspaceRoot, gitDir, gitCommonDir string) (string, error) {
	cRepoRoot, err := canonicalAbsPath(repoRoot)
	if err != nil {
		return "", fmt.Errorf("canonical repo root: %w", err)
	}
	cWorkspaceRoot, err := canonicalAbsPath(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("canonical workspace root: %w", err)
	}
	cGitDir, err := canonicalAbsPath(gitDir)
	if err != nil {
		return "", fmt.Errorf("canonical git dir: %w", err)
	}
	cGitCommon, err := canonicalAbsPath(gitCommonDir)
	if err != nil {
		return "", fmt.Errorf("canonical git common dir: %w", err)
	}

	name := cRepoRoot + "\x1f" + cWorkspaceRoot + "\x1f" + cGitDir + "\x1f" + cGitCommon
	return uuid.NewSHA1(workspaceNamespace, []byte(name)).String(), nil
}

// DeriveWorkspaceID is the shared derivation function used by ae-cli runtime and git hooks.
// It is a stable UUIDv5 derived from the canonical git context.
func DeriveWorkspaceID(repoRoot, workspaceRoot, gitDir, gitCommonDir string) (string, error) {
	return deriveWorkspaceID(repoRoot, workspaceRoot, gitDir, gitCommonDir)
}
