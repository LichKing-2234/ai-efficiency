package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RuntimeBundle is the user-level runtime state for a session.
//
// It may contain sensitive data (for example env vars with API key secrets),
// so it must be stored with restricted permissions.
type RuntimeBundle struct {
	SessionID    string            `json:"session_id"`
	RuntimeRef   string            `json:"runtime_ref,omitempty"`
	EnvBundle    map[string]string `json:"env_bundle,omitempty"`
	KeyExpiresAt time.Time         `json:"key_expires_at,omitempty"`
}

func runtimeDir(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// The tests set HOME, so failing here is unexpected; keep behavior deterministic.
		return ""
	}
	return filepath.Join(home, ".ae-cli", "runtime", sessionID)
}

func runtimeFilePath(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "runtime.json")
}

func WriteRuntimeBundle(b *RuntimeBundle) error {
	if b == nil {
		return fmt.Errorf("runtime bundle is nil")
	}
	if strings.TrimSpace(b.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}

	d := runtimeDir(b.SessionID)
	if d == "" {
		return fmt.Errorf("runtime dir is empty")
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling runtime bundle: %w", err)
	}

	// Restricted permissions: runtime contains secrets.
	if err := os.WriteFile(runtimeFilePath(b.SessionID), data, 0o600); err != nil {
		return fmt.Errorf("writing runtime bundle: %w", err)
	}
	return nil
}

func ReadRuntimeBundle(sessionID string) (*RuntimeBundle, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	data, err := os.ReadFile(runtimeFilePath(sessionID))
	if err != nil {
		return nil, err
	}
	var b RuntimeBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing runtime bundle: %w", err)
	}
	return &b, nil
}

func RuntimeCollectorsDir(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "collectors")
}

func RemoveRuntime(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	d := runtimeDir(sessionID)
	if d == "" {
		return fmt.Errorf("runtime dir is empty")
	}
	if err := os.RemoveAll(d); err != nil {
		return fmt.Errorf("removing runtime dir: %w", err)
	}
	return nil
}
