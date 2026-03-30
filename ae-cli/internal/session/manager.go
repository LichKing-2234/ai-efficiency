package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

type State struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	Branch      string    `json:"branch"`
	StartedAt   time.Time `json:"started_at"`
	TmuxSession string    `json:"tmux_session,omitempty"`
}

type Manager struct {
	client *client.Client
	config *config.Config
}

func NewManager(c *client.Client, cfg *config.Config) *Manager {
	return &Manager{
		client: c,
		config: cfg,
	}
}

func (m *Manager) Start() (*State, error) {
	// Reconcile: if local binding exists but backend session is gone, clean it up.
	if existing, _ := m.Current(); existing != nil && strings.TrimSpace(existing.ID) != "" {
		if _, err := m.client.GetSession(context.Background(), existing.ID); errors.Is(err, client.ErrNotFound) {
			_ = m.cleanupLocal(existing.ID)
		}
	}

	gc, err := detectGitContext()
	if err != nil {
		return nil, err
	}

	resp, err := m.client.BootstrapSession(context.Background(), client.BootstrapSessionRequest{
		RepoFullName:   gc.repoURL,
		BranchSnapshot: gc.branch,
		HeadSHA:        gc.headSHA,
		WorkspaceRoot:  gc.workspaceRoot,
		GitDir:         gc.gitDir,
		GitCommonDir:   gc.gitCommonDir,
		WorkspaceID:    gc.workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrapping session: %w", err)
	}

	rollback := func(cause error) error {
		if strings.TrimSpace(resp.SessionID) != "" {
			_ = m.client.StopSession(context.Background(), resp.SessionID)
			_ = m.cleanupBootstrapArtifacts(gc.workspaceRoot, resp.SessionID)
		}
		return cause
	}

	state := &State{
		ID:        resp.SessionID,
		Repo:      gc.repoURL,
		Branch:    gc.branch,
		StartedAt: resp.StartedAt,
	}

	marker := &Marker{
		SessionID:     resp.SessionID,
		WorkspaceID:   gc.workspaceID,
		RuntimeRef:    resp.RuntimeRef,
		ProviderName:  resp.ProviderName,
		RelayAPIKeyID: resp.RelayAPIKeyID,
		RepoFullName:  gc.repoURL,
		Branch:        gc.branch,
		HeadSHA:       gc.headSHA,
	}

	// Ensure marker dir isn't accidentally committed.
	if err := ensureGitInfoExcludeHas(gc.gitCommonDir, "/.ae/"); err != nil {
		if err2 := ensureGitInfoExcludeHas(gc.gitDir, "/.ae/"); err2 != nil {
			return nil, rollback(fmt.Errorf("ensuring git exclude: %w", err2))
		}
	}
	if err := WriteMarker(gc.workspaceRoot, marker); err != nil {
		return nil, rollback(fmt.Errorf("writing workspace marker: %w", err))
	}

	rt := &RuntimeBundle{
		SessionID:     resp.SessionID,
		RuntimeRef:    resp.RuntimeRef,
		WorkspaceRoot: gc.workspaceRoot,
		EnvBundle:     resp.EnvBundle,
		KeyExpiresAt:  resp.KeyExpiresAt,
	}
	if err := WriteRuntimeBundle(rt); err != nil {
		return nil, rollback(fmt.Errorf("writing runtime bundle: %w", err))
	}

	// Compatibility: keep legacy global state for commands run outside the workspace.
	if err := writeState(state); err != nil {
		return nil, rollback(fmt.Errorf("writing session state: %w", err))
	}

	return state, nil
}

func (m *Manager) Stop() (*State, error) {
	// Resolve bound state first so we know which workspace marker to remove.
	bound, err := ResolveBoundState("")
	if err != nil {
		return nil, err
	}

	var state *State
	if bound != nil && bound.Marker != nil && strings.TrimSpace(bound.Marker.SessionID) != "" {
		state = &State{
			ID:          bound.Marker.SessionID,
			Repo:        bound.Marker.RepoFullName,
			Branch:      bound.Marker.Branch,
			TmuxSession: bound.Marker.TmuxSession,
		}
	} else {
		state, err = m.Current()
		if err != nil {
			return nil, err
		}
		if state == nil {
			return nil, fmt.Errorf("no active session")
		}
	}

	if err := m.client.StopSession(context.Background(), state.ID); err != nil {
		// Session already gone on backend — clean up local state anyway
		if !errors.Is(err, client.ErrNotFound) {
			return nil, fmt.Errorf("stopping session: %w", err)
		}
	}

	// Clean up local state (marker/runtime/global state) best-effort.
	_ = m.cleanupLocal(state.ID)

	return state, nil
}

// Shutdown performs a best-effort session cleanup on process exit.
// Unlike Stop, it ignores API errors to ensure the state file is always removed.
func (m *Manager) Shutdown(ctx context.Context) error {
	state, err := m.Current()
	if err != nil || state == nil {
		return nil
	}

	// Best-effort: try to notify backend, ignore errors
	_ = m.client.StopSession(ctx, state.ID)

	// Always clean up local state
	_ = m.cleanupLocal(state.ID)
	return nil
}

// SaveState persists the session state to disk.
func (m *Manager) SaveState(state *State) error {
	// Prefer workspace marker when present.
	if bound, err := ResolveBoundState(""); err != nil {
		return err
	} else if bound != nil && bound.Marker != nil && strings.TrimSpace(bound.Marker.SessionID) != "" && bound.Marker.SessionID == state.ID {
		bound.Marker.TmuxSession = state.TmuxSession
		if err := WriteMarker(bound.WorkspaceRoot, bound.Marker); err != nil {
			return fmt.Errorf("writing workspace marker: %w", err)
		}
	}
	// Compatibility: also update legacy state.
	return writeState(state)
}

func (m *Manager) Current() (*State, error) {
	// Prefer workspace marker/runtime binding when available.
	if bound, err := ResolveBoundState(""); err != nil {
		return nil, err
	} else if bound != nil && bound.Marker != nil && strings.TrimSpace(bound.Marker.SessionID) != "" {
		return &State{
			ID:          bound.Marker.SessionID,
			Repo:        bound.Marker.RepoFullName,
			Branch:      bound.Marker.Branch,
			TmuxSession: bound.Marker.TmuxSession,
		}, nil
	}

	path, err := stateFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &state, nil
}

func detectRepo() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func detectBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

type gitContext struct {
	repoRoot      string
	workspaceRoot string
	gitDir        string
	gitCommonDir  string
	repoURL       string
	branch        string
	headSHA       string
	workspaceID   string
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func absUnder(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(root, p))
}

func detectGitContext() (*gitContext, error) {
	repoRoot, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("detecting git workspace root: %w", err)
	}
	repoRoot, err = canonicalAbsPath(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("canonical workspace root: %w", err)
	}
	// For ae-cli today, repo root and workspace root are the git toplevel. Keep them
	// separate in the derivation contract for forward compatibility.
	workspaceRoot := repoRoot

	// Stabilize all other git outputs by running from the repo root.
	repoURL, err := gitOutput(workspaceRoot, "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("detecting git repo: %w", err)
	}
	branch, err := gitOutput(workspaceRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("detecting git branch: %w", err)
	}
	headSHA, err := gitOutput(workspaceRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("detecting git head sha: %w", err)
	}

	// Prefer absolute git dir paths to correctly handle linked worktrees (where .git may be a file).
	gitDirAbs, err := gitOutput(workspaceRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		gitDirRel, err2 := gitOutput(workspaceRoot, "rev-parse", "--git-dir")
		if err2 != nil {
			return nil, fmt.Errorf("detecting git dir: %w", err2)
		}
		gitDirAbs, err = absUnder(workspaceRoot, gitDirRel)
		if err != nil {
			return nil, fmt.Errorf("abs git dir: %w", err)
		}
	}
	gitCommonRel, err := gitOutput(workspaceRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("detecting git common dir: %w", err)
	}
	gitCommonAbs, err := absUnder(workspaceRoot, gitCommonRel)
	if err != nil {
		return nil, fmt.Errorf("abs git common dir: %w", err)
	}
	gitDirAbs, err = canonicalAbsPath(gitDirAbs)
	if err != nil {
		return nil, fmt.Errorf("canonical git dir: %w", err)
	}
	gitCommonAbs, err = canonicalAbsPath(gitCommonAbs)
	if err != nil {
		return nil, fmt.Errorf("canonical git common dir: %w", err)
	}

	workspaceID, err := deriveWorkspaceID(repoRoot, workspaceRoot, gitDirAbs, gitCommonAbs)
	if err != nil {
		return nil, fmt.Errorf("deriving workspace_id: %w", err)
	}

	return &gitContext{
		repoRoot:      repoRoot,
		workspaceRoot: workspaceRoot,
		gitDir:        gitDirAbs,
		gitCommonDir:  gitCommonAbs,
		repoURL:       repoURL,
		branch:        branch,
		headSHA:       headSHA,
		workspaceID:   workspaceID,
	}, nil
}

func ensureGitInfoExcludeHas(gitDirAbs string, pattern string) error {
	gitDirAbs = strings.TrimSpace(gitDirAbs)
	pattern = strings.TrimSpace(pattern)
	if gitDirAbs == "" {
		return fmt.Errorf("git dir is empty")
	}
	if pattern == "" {
		return fmt.Errorf("pattern is empty")
	}

	excludePath := filepath.Join(gitDirAbs, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("creating exclude dir: %w", err)
	}

	var existing []byte
	mode := os.FileMode(0o644)
	if info, err := os.Stat(excludePath); err == nil {
		mode = info.Mode().Perm()
		if b, err := os.ReadFile(excludePath); err == nil {
			existing = b
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat exclude: %w", err)
	}

	lines := strings.Split(string(existing), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	out := string(existing)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += pattern + "\n"

	if err := os.WriteFile(excludePath, []byte(out), mode); err != nil {
		return fmt.Errorf("writing exclude: %w", err)
	}
	return nil
}

func stateFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "current-session.json"), nil
}

func writeState(state *State) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

func removeState() error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing state file: %w", err)
	}

	return nil
}

func (m *Manager) cleanupLocal(sessionID string) error {
	removeMarkerForSession := func(workspaceRoot string) {
		workspaceRoot = strings.TrimSpace(workspaceRoot)
		if workspaceRoot == "" || strings.TrimSpace(sessionID) == "" {
			return
		}
		mk, err := ReadMarker(workspaceRoot)
		if err != nil || mk == nil {
			return
		}
		if strings.TrimSpace(mk.SessionID) != sessionID {
			return
		}
		_ = RemoveMarker(workspaceRoot)
	}

	// If we're currently inside a bound workspace, only remove its marker if it matches the session.
	if bound, err := ResolveBoundState(""); err == nil && bound != nil {
		removeMarkerForSession(bound.WorkspaceRoot)
	}

	// If Stop/Shutdown is invoked outside the workspace, use the runtime pointer to remove the marker.
	if strings.TrimSpace(sessionID) != "" {
		if rt, err := ReadRuntimeBundle(sessionID); err == nil && rt != nil {
			removeMarkerForSession(rt.WorkspaceRoot)
		}
		// Remove runtime (contains secrets) last.
		_ = RemoveRuntime(sessionID)
	}

	// Remove legacy global state.
	_ = removeState()
	return nil
}

func (m *Manager) cleanupBootstrapArtifacts(workspaceRoot, sessionID string) error {
	if strings.TrimSpace(workspaceRoot) != "" {
		_ = RemoveMarker(workspaceRoot)
	}
	if strings.TrimSpace(sessionID) != "" {
		_ = RemoveRuntime(sessionID)
	}
	_ = removeState()
	return nil
}
