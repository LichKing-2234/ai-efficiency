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
	"github.com/google/uuid"
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
	// Reconcile: if local state exists but backend session is gone, clean up
	if existing, _ := m.Current(); existing != nil {
		if _, err := m.client.GetSession(context.Background(), existing.ID); errors.Is(err, client.ErrNotFound) {
			_ = removeState()
		}
	}

	repo, err := detectRepo()
	if err != nil {
		return nil, fmt.Errorf("detecting git repo: %w", err)
	}

	branch, err := detectBranch()
	if err != nil {
		return nil, fmt.Errorf("detecting git branch: %w", err)
	}

	sessionID := uuid.New().String()

	sess, err := m.client.CreateSession(context.Background(), client.CreateSessionRequest{
		ID:           sessionID,
		RepoFullName: repo,
		Branch:       branch,
	})
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	state := &State{
		ID:        sess.ID,
		Repo:      repo,
		Branch:    branch,
		StartedAt: sess.StartedAt,
	}

	if err := writeState(state); err != nil {
		return nil, fmt.Errorf("writing session state: %w", err)
	}

	return state, nil
}

func (m *Manager) Stop() (*State, error) {
	state, err := m.Current()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("no active session")
	}

	if err := m.client.StopSession(context.Background(), state.ID); err != nil {
		// Session already gone on backend — clean up local state anyway
		if !errors.Is(err, client.ErrNotFound) {
			return nil, fmt.Errorf("stopping session: %w", err)
		}
	}

	if err := removeState(); err != nil {
		return nil, fmt.Errorf("removing session state: %w", err)
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

	// Always clean up local state
	_ = removeState()
	return nil
}

// SaveState persists the session state to disk.
func (m *Manager) SaveState(state *State) error {
	return writeState(state)
}

func (m *Manager) Current() (*State, error) {
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
