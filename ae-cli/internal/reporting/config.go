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

type InstallationIdentity struct {
	Version        int    `json:"version"`
	InstallationID string `json:"installation_id"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "reporting.json"), nil
}

func DefaultIdentityPath() (string, error) {
	path, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return identityPathForConfig(path), nil
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
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	config, err := Load(path)
	if err == nil {
		if _, parseErr := uuid.Parse(config.InstallationID); parseErr != nil {
			return nil, fmt.Errorf("invalid reporting installation_id")
		}
		if err := ensureInstallationIdentity(identityPathForConfig(path), config.InstallationID); err != nil {
			return nil, err
		}
		return config, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	identity, err := loadOrCreateInstallationIdentity(identityPathForConfig(path))
	if err != nil {
		return nil, err
	}
	config = &Config{Version: 1, InstallationID: identity.InstallationID}
	if err := Save(path, config); err != nil {
		return nil, err
	}
	return config, nil
}

func LoadInstallationIdentity(path string) (*InstallationIdentity, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultIdentityPath()
		if err != nil {
			return nil, err
		}
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var identity InstallationIdentity
	if err := json.Unmarshal(payload, &identity); err != nil {
		return nil, fmt.Errorf("parse reporting installation identity: %w", err)
	}
	if _, err := uuid.Parse(strings.TrimSpace(identity.InstallationID)); err != nil {
		return nil, fmt.Errorf("invalid reporting installation identity")
	}
	return &identity, nil
}

func loadOrCreateInstallationIdentity(path string) (*InstallationIdentity, error) {
	identity, err := LoadInstallationIdentity(path)
	if err == nil {
		return identity, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	identity = &InstallationIdentity{Version: 1, InstallationID: uuid.NewString()}
	if err := saveInstallationIdentity(path, identity); err != nil {
		return nil, err
	}
	return identity, nil
}

func ensureInstallationIdentity(path, installationID string) error {
	identity, err := LoadInstallationIdentity(path)
	if err == nil {
		if identity.InstallationID != installationID {
			return fmt.Errorf("reporting installation identity does not match credential config")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return saveInstallationIdentity(path, &InstallationIdentity{Version: 1, InstallationID: installationID})
}

func saveInstallationIdentity(path string, identity *InstallationIdentity) error {
	if identity == nil {
		return fmt.Errorf("reporting installation identity is nil")
	}
	if _, err := uuid.Parse(strings.TrimSpace(identity.InstallationID)); err != nil {
		return fmt.Errorf("invalid reporting installation identity")
	}
	if identity.Version == 0 {
		identity.Version = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create reporting identity dir: %w", err)
	}
	payload, err := json.MarshalIndent(identity, "", "  ")
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

func identityPathForConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "reporting-installation.json")
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

// Delete removes only the account-scoped reporting credential config. The
// stable machine installation identity is intentionally preserved so a later
// successful enrollment can rotate credentials for the same installation.
func Delete(path string) error {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove reporting config: %w", err)
	}
	return nil
}
