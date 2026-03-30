package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Marker is the workspace-local session binding stored under <workspace>/.ae/session.json.
//
// It should never contain sensitive secrets; those belong in the RuntimeBundle.
type Marker struct {
	SessionID    string `json:"session_id"`
	WorkspaceID  string `json:"workspace_id"`
	RuntimeRef   string `json:"runtime_ref,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`

	// Non-sensitive metadata that helps CLI UX / debugging.
	RelayAPIKeyID int64  `json:"relay_api_key_id,omitempty"`
	RepoFullName  string `json:"repo_full_name,omitempty"`
	Branch        string `json:"branch_snapshot,omitempty"`
	HeadSHA       string `json:"head_sha,omitempty"`
	TmuxSession   string `json:"tmux_session,omitempty"`
}

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

func markerPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".ae", "session.json")
}

func WriteMarker(workspaceRoot string, m *Marker) error {
	if m == nil {
		return fmt.Errorf("marker is nil")
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("workspace root is empty")
	}
	p := markerPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("creating marker dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling marker: %w", err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("writing marker: %w", err)
	}
	return nil
}

func ReadMarker(workspaceRoot string) (*Marker, error) {
	p := markerPath(workspaceRoot)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing marker: %w", err)
	}
	return &m, nil
}

func RemoveMarker(workspaceRoot string) error {
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("workspace root is empty")
	}
	p := markerPath(workspaceRoot)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing marker: %w", err)
	}
	return nil
}

type BoundState struct {
	WorkspaceRoot string
	Marker        *Marker
	Runtime       *RuntimeBundle
}

// ResolveBoundState loads the workspace marker (preferred) and the associated runtime bundle.
//
// It searches for .ae/session.json by walking up from cwd. If cwd is empty, uses os.Getwd().
func ResolveBoundState(cwd string) (*BoundState, error) {
	if strings.TrimSpace(cwd) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
		cwd = wd
	}

	dir, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("abs cwd: %w", err)
	}

	for {
		p := markerPath(dir)
		if _, err := os.Stat(p); err == nil {
			m, err := ReadMarker(dir)
			if err != nil {
				return nil, err
			}
			var rt *RuntimeBundle
			if strings.TrimSpace(m.SessionID) != "" {
				if b, err := ReadRuntimeBundle(m.SessionID); err == nil {
					rt = b
				}
			}
			return &BoundState{WorkspaceRoot: dir, Marker: m, Runtime: rt}, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}
