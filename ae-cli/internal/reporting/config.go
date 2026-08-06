package reporting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	Version          int        `json:"version"`
	InstallationID   string     `json:"installation_id"`
	ServerURL        string     `json:"server_url,omitempty"`
	AuthSubject      string     `json:"auth_subject,omitempty"`
	ReporterToken    string     `json:"reporter_token,omitempty"`
	OTLPToken        string     `json:"otlp_token,omitempty"`
	ReportingEnabled bool       `json:"reporting_enabled"`
	OTelEnabled      bool       `json:"otel_enabled"`
	EnabledAt        *time.Time `json:"enabled_at,omitempty"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "reporting.json"), nil
}

func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(payload, &config); err != nil {
		return nil, fmt.Errorf("parse reporting config: %w", err)
	}
	return &config, nil
}

func LoadOrCreate(path string) (*Config, error) {
	config, err := Load(path)
	if err == nil {
		if _, parseErr := uuid.Parse(config.InstallationID); parseErr != nil {
			return nil, fmt.Errorf("invalid reporting installation_id")
		}
		return config, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	config = &Config{Version: 1, InstallationID: uuid.NewString()}
	if err := Save(path, config); err != nil {
		return nil, err
	}
	return config, nil
}

func Save(path string, config *Config) error {
	if config == nil {
		return fmt.Errorf("reporting config is nil")
	}
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if config.Version == 0 {
		config.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create reporting config dir: %w", err)
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
