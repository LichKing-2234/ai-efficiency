package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

func legacyWorkflowRetiredError() error {
	return fmt.Errorf("legacy workflow retired: use 'ae-cli init', 'ae-cli sync', or 'ae-cli doctor'")
}

type attributionContext struct {
	repoRoot      string
	gitDir        string
	gitCommonDir  string
	workspaceID   string
	attributionRoot string
}

func detectAttributionContext() (*attributionContext, error) {
	repoRoot, err := gitOutputForCutover("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("detect repo root: %w", err)
	}
	gitDirAbs, err := gitOutputForCutover("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, fmt.Errorf("detect git dir: %w", err)
	}
	gitCommonRel, err := gitOutputForCutover("rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("detect git common dir: %w", err)
	}
	gitCommonAbs, err := absUnderForCutover(repoRoot, gitCommonRel)
	if err != nil {
		return nil, fmt.Errorf("abs git common dir: %w", err)
	}
	workspaceID, err := session.DeriveWorkspaceID(repoRoot, repoRoot, gitDirAbs, gitCommonAbs)
	if err != nil {
		return nil, fmt.Errorf("derive workspace id: %w", err)
	}
	return &attributionContext{
		repoRoot:        repoRoot,
		gitDir:          gitDirAbs,
		gitCommonDir:    gitCommonAbs,
		workspaceID:     workspaceID,
		attributionRoot: attributionlocal.AttributionRootDir(),
	}, nil
}

func gitOutputForCutover(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr=%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func absUnderForCutover(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(root, p))
}

func bestEffortSelfPath() string {
	selfPath, err := os.Executable()
	if err == nil && strings.TrimSpace(selfPath) != "" {
		return selfPath
	}
	if len(os.Args) > 0 && strings.TrimSpace(os.Args[0]) != "" {
		return os.Args[0]
	}
	return "ae-cli"
}
