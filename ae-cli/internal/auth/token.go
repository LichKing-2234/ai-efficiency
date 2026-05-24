package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TokenFile represents the stored OAuth token.
type TokenFile struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	ServerURL    string    `json:"server_url"`
	AuthSubject  string    `json:"auth_subject,omitempty"`
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

func SubjectFromAccessToken(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	switch v := claims["user_id"].(type) {
	case float64:
		if v > 0 {
			return fmt.Sprintf("user:%d", int(v))
		}
	case string:
		v = strings.TrimSpace(v)
		if v != "" {
			return "user:" + v
		}
	}
	return ""
}

func (t *TokenFile) StableAuthSubject() string {
	if t == nil {
		return ""
	}
	if s := strings.TrimSpace(t.AuthSubject); s != "" {
		return s
	}
	return SubjectFromAccessToken(t.AccessToken)
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
