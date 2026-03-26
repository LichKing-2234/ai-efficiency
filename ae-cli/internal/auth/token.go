package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenFile represents the stored OAuth token.
type TokenFile struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	ServerURL    string    `json:"server_url"`
}

// DefaultTokenPath returns ~/.ae-cli/token.json.
func DefaultTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "token.json"), nil
}

// IsValid returns true if the token exists and hasn't expired.
func (t *TokenFile) IsValid() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt)
}

// NeedsRefresh returns true if the token expires within 5 minutes.
func (t *TokenFile) NeedsRefresh() bool {
	return time.Until(t.ExpiresAt) < 5*time.Minute
}

// ReadToken reads and parses the token file.
func ReadToken(path string) (*TokenFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	var token TokenFile
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &token, nil
}

// WriteToken atomically writes the token file with 0600 permissions.
func WriteToken(path string, token *TokenFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename token file: %w", err)
	}
	return nil
}

// DeleteToken removes the token file.
func DeleteToken(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}
