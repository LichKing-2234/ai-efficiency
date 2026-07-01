package hooks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/repoidentity"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

type GitContext struct {
	RepoRoot        string
	RemoteURL       string
	Branch          string
	GitDir          string
	GitCommonDir    string
	DefaultHooksDir string
	WorkspaceID     string
	RepoKey         string
}

func DetectGitContext(cwd string) (*GitContext, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	repoRoot, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("detect repo root: %w", err)
	}
	gitDir, err := gitOutput(cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, fmt.Errorf("detect git dir: %w", err)
	}
	gitCommon, err := gitOutput(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("detect git common dir: %w", err)
	}
	gitCommon, err = absUnder(repoRoot, gitCommon)
	if err != nil {
		return nil, fmt.Errorf("abs git common dir: %w", err)
	}
	defaultHooksDir, err := gitOutput(cwd, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return nil, fmt.Errorf("detect default hooks dir: %w", err)
	}
	defaultHooksDir, err = absUnder(repoRoot, defaultHooksDir)
	if err != nil {
		return nil, fmt.Errorf("abs default hooks dir: %w", err)
	}
	remoteURL, _ := gitOutput(cwd, "config", "--get", "remote.origin.url")
	branch, _ := gitOutput(cwd, "symbolic-ref", "--short", "-q", "HEAD")
	workspaceID, err := session.DeriveWorkspaceID(repoRoot, repoRoot, gitDir, gitCommon)
	if err != nil {
		return nil, fmt.Errorf("derive workspace id: %w", err)
	}
	repoKey := ""
	if identity, err := repoidentity.Derive(remoteURL); err == nil {
		repoKey = identity.RepoKey
	} else if strings.TrimSpace(remoteURL) != "" {
		repoKey = repoidentity.Fallback(remoteURL, "").RepoKey
	}

	return &GitContext{
		RepoRoot:        filepath.Clean(repoRoot),
		RemoteURL:       strings.TrimSpace(remoteURL),
		Branch:          strings.TrimSpace(branch),
		GitDir:          filepath.Clean(gitDir),
		GitCommonDir:    filepath.Clean(gitCommon),
		DefaultHooksDir: filepath.Clean(defaultHooksDir),
		WorkspaceID:     workspaceID,
		RepoKey:         repoKey,
	}, nil
}
