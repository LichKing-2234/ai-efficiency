package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
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

func NewManager(c *client.Client, cfg *config.Config) *Manager {
	return &Manager{
		client: c,
		config: cfg,
	}
}

func (m *Manager) SaveState(state *State) error {
	if bound, err := ResolveBoundState(""); err != nil {
		return err
	} else if bound != nil && bound.Marker != nil && strings.TrimSpace(bound.Marker.SessionID) != "" && bound.Marker.SessionID == state.ID {
		bound.Marker.TmuxSession = state.TmuxSession
		if err := WriteMarker(bound.WorkspaceRoot, bound.Marker); err != nil {
			return fmt.Errorf("writing workspace marker: %w", err)
		}
	}
	return writeState(state)
}

func (m *Manager) Current() (*State, error) {
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
