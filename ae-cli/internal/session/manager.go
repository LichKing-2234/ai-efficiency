package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/ai-efficiency/ae-cli/internal/proxy"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

type State struct {
	ID            string    `json:"id"`
	Repo          string    `json:"repo"`
	Branch        string    `json:"branch"`
	WorkspaceRoot string    `json:"workspace_root,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	TmuxSession   string    `json:"tmux_session,omitempty"`
}

type Manager struct {
	client *client.Client
	config *config.Config
}

var (
	spawnProxyProcess = proxy.Spawn
	stopProxyProcess  = proxy.Stop
)

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
			_ = m.cleanupLocal(existing.ID, existing.WorkspaceRoot)
		}
	}

	gc, err := detectGitContext()
	if err != nil {
		return nil, err
	}
	if err := toolconfig.CleanupLegacyWorkspaceCodexConfig(gc.workspaceRoot); err != nil {
		return nil, fmt.Errorf("cleaning legacy codex config: %w", err)
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

	var rt *RuntimeBundle
	rollback := func(cause error) error {
		if err := m.stopLocalProxy(rt); err != nil {
			// Preserve local marker/runtime when proxy shutdown fails so recovery is possible.
			if rt != nil && strings.TrimSpace(rt.SessionID) != "" {
				_ = WriteRuntimeBundle(rt)
			}
			return fmt.Errorf("%w: rollback proxy shutdown failed: %w", cause, err)
		}
		if strings.TrimSpace(resp.SessionID) != "" {
			_ = m.client.StopSession(context.Background(), resp.SessionID)
			_ = m.cleanupBootstrapArtifacts(gc.workspaceRoot, resp.SessionID)
		}
		return cause
	}

	state := &State{
		ID:            resp.SessionID,
		Repo:          gc.repoURL,
		Branch:        gc.branch,
		WorkspaceRoot: gc.workspaceRoot,
		StartedAt:     resp.StartedAt,
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

	rt = &RuntimeBundle{
		SessionID:     resp.SessionID,
		RuntimeRef:    resp.RuntimeRef,
		WorkspaceRoot: gc.workspaceRoot,
		EnvBundle:     resp.EnvBundle,
		KeyExpiresAt:  resp.KeyExpiresAt,
	}
	if err := m.startLocalProxy(rt); err != nil {
		return nil, rollback(fmt.Errorf("starting local proxy: %w", err))
	}
	codexHome := filepath.Join(runtimeDir(rt.SessionID), "codex-home")
	if err := toolconfig.WriteCodexSessionConfig(codexHome, toolconfig.CodexConfig{
		BaseURL:  "http://" + rt.Proxy.ListenAddr + "/openai/v1",
		TokenEnv: "AE_LOCAL_PROXY_TOKEN",
		Model:    "gpt-5.4",
	}); err != nil {
		return nil, rollback(fmt.Errorf("writing codex config: %w", err))
	}
	if rt.EnvBundle == nil {
		rt.EnvBundle = map[string]string{}
	}
	rt.EnvBundle["CODEX_HOME"] = codexHome
	rt.EnvBundle = toolconfig.ApplyClaudeProxyEnv(rt.EnvBundle, toolconfig.ClaudeEnv{
		BaseURL: "http://" + rt.Proxy.ListenAddr + "/anthropic",
		Token:   rt.Proxy.AuthToken,
	})
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
			ID:            bound.Marker.SessionID,
			Repo:          bound.Marker.RepoFullName,
			Branch:        bound.Marker.Branch,
			WorkspaceRoot: bound.WorkspaceRoot,
			TmuxSession:   bound.Marker.TmuxSession,
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

	if err := m.cleanupLocal(state.ID, state.WorkspaceRoot); err != nil {
		return nil, err
	}

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

	// Best effort cleanup; preserve runtime on proxy-stop errors for recovery.
	_ = m.cleanupLocal(state.ID, state.WorkspaceRoot)
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
			ID:            bound.Marker.SessionID,
			Repo:          bound.Marker.RepoFullName,
			Branch:        bound.Marker.Branch,
			WorkspaceRoot: bound.WorkspaceRoot,
			TmuxSession:   bound.Marker.TmuxSession,
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

func (m *Manager) cleanupLocal(sessionID, workspaceRoot string) error {
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

	// If Stop/Shutdown is invoked outside the workspace, use the runtime pointer to remove the marker.
	var rt *RuntimeBundle
	if strings.TrimSpace(sessionID) != "" {
		if loaded, err := ReadRuntimeBundle(sessionID); err == nil && loaded != nil {
			rt = loaded
			if err := m.stopLocalProxy(rt); err != nil {
				return err
			}
		}
	}
	if rt != nil {
		_ = os.RemoveAll(strings.TrimSpace(rt.EnvBundle["CODEX_HOME"]))
	} else if strings.TrimSpace(sessionID) != "" {
		_ = os.RemoveAll(filepath.Join(runtimeDir(sessionID), "codex-home"))
	}

	// If we're currently inside a bound workspace, only remove its marker if it matches the session.
	if bound, err := ResolveBoundState(""); err == nil && bound != nil {
		removeMarkerForSession(bound.WorkspaceRoot)
	}
	removeMarkerForSession(workspaceRoot)
	if rt != nil {
		removeMarkerForSession(rt.WorkspaceRoot)
	}

	if strings.TrimSpace(sessionID) != "" {
		hasPendingQueue, err := HasPendingQueue(sessionID)
		if err == nil && hasPendingQueue {
			// Preserve queued hook events for later flush/recovery, but still drop secrets.
			_ = RemoveRuntimeBundle(sessionID)
		} else {
			// Remove runtime (contains secrets) last.
			_ = RemoveRuntime(sessionID)
		}
	}

	// Remove legacy global state.
	_ = removeState()
	return nil
}

func (m *Manager) startLocalProxy(rt *RuntimeBundle) error {
	if rt == nil {
		return fmt.Errorf("runtime bundle is nil")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	providerURL, providerKey := resolveProxyUpstream(rt.EnvBundle)
	cfg := proxy.RuntimeConfig{
		SessionID:   rt.SessionID,
		ListenAddr:  "127.0.0.1:0",
		AuthToken:   token,
		ProviderURL: providerURL,
		ProviderKey: providerKey,
	}
	result, err := spawnProxyProcess(cfg)
	if err != nil {
		return err
	}
	rt.Proxy = &ProxyRuntime{
		PID:        result.PID,
		ListenAddr: result.ListenAddr,
		AuthToken:  token,
		ConfigPath: result.ConfigPath,
	}
	if rt.EnvBundle == nil {
		rt.EnvBundle = map[string]string{}
	}
	rt.EnvBundle["AE_LOCAL_PROXY_URL"] = "http://" + result.ListenAddr
	rt.EnvBundle["AE_LOCAL_PROXY_TOKEN"] = token
	return nil
}

func resolveProxyUpstream(env map[string]string) (string, string) {
	pairs := [][2]string{
		{"OPENAI_BASE_URL", "OPENAI_API_KEY"},
		{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY"},
		{"SUB2API_BASE_URL", "SUB2API_API_KEY"},
	}
	for _, pair := range pairs {
		url := strings.TrimSpace(env[pair[0]])
		key := strings.TrimSpace(env[pair[1]])
		if url != "" && key != "" {
			return url, key
		}
	}

	url := firstNonEmptyEnv(env, "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL", "SUB2API_BASE_URL")
	key := firstNonEmptyEnv(env, "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "SUB2API_API_KEY")
	return url, key
}

func firstNonEmptyEnv(env map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(env[key]); value != "" {
			return value
		}
	}
	return ""
}

func (m *Manager) stopLocalProxy(rt *RuntimeBundle) error {
	if rt == nil || rt.Proxy == nil || rt.Proxy.PID <= 0 {
		return nil
	}
	req := proxy.StopRequest{
		PID:        rt.Proxy.PID,
		ListenAddr: rt.Proxy.ListenAddr,
		AuthToken:  rt.Proxy.AuthToken,
		ConfigPath: rt.Proxy.ConfigPath,
	}
	if err := stopProxyProcess(req); err != nil {
		return fmt.Errorf("stopping local proxy pid=%d: %w", req.PID, err)
	}
	rt.Proxy = nil
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(b), nil
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
