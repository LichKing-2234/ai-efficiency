package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolveWritableConfigPath returns the config path used by runtime-editable settings.
func ResolveWritableConfigPath(explicitPath, stateDir string) string {
	if v := strings.TrimSpace(explicitPath); v != "" {
		return v
	}
	if v := strings.TrimSpace(stateDir); v != "" {
		return filepath.Join(v, "config.yaml")
	}
	return "config.yaml"
}

// ResolveRuntimeStateDir returns the state directory used for runtime-editable data.
func ResolveRuntimeStateDir(getenv func(string) string) string {
	if getenv == nil {
		return ""
	}
	if v := strings.TrimSpace(getenv("AE_STATE_DIR")); v != "" {
		return v
	}
	return strings.TrimSpace(getenv("AE_DEPLOYMENT_STATE_DIR"))
}

// EnsureWritableConfigFile materializes the current effective config to disk when no writable config exists yet.
func EnsureWritableConfigFile(path string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(configToYAMLMap(cfg))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func configToYAMLMap(cfg *Config) map[string]any {
	return map[string]any{
		"server": map[string]any{
			"port":         cfg.Server.Port,
			"mode":         cfg.Server.Mode,
			"frontend_url": cfg.Server.FrontendURL,
			"public_url":   cfg.Server.PublicURL,
		},
		"db": map[string]any{
			"dsn":               cfg.DB.DSN,
			"max_open_conns":    cfg.DB.MaxOpenConns,
			"max_idle_conns":    cfg.DB.MaxIdleConns,
			"conn_max_lifetime": cfg.DB.ConnMaxLifetime,
		},
		"redis": map[string]any{
			"addr":     cfg.Redis.Addr,
			"password": cfg.Redis.Password,
			"db":       cfg.Redis.DB,
		},
		"relay": map[string]any{
			"provider":         cfg.Relay.Provider,
			"url":              cfg.Relay.URL,
			"admin_api_key":    cfg.Relay.AdminAPIKey,
			"model":            cfg.Relay.Model,
			"default_group_id": cfg.Relay.DefaultGroupID,
		},
		"version_check": map[string]any{
			"enabled":         cfg.VersionCheck.Enabled,
			"release_api_url": cfg.VersionCheck.ReleaseAPIURL,
		},
		"auth": map[string]any{
			"jwt_secret":        cfg.Auth.JWTSecret,
			"access_token_ttl":  cfg.Auth.AccessTokenTTL,
			"refresh_token_ttl": cfg.Auth.RefreshTokenTTL,
			"ldap": map[string]any{
				"url":           cfg.Auth.LDAP.URL,
				"base_dn":       cfg.Auth.LDAP.BaseDN,
				"bind_dn":       cfg.Auth.LDAP.BindDN,
				"bind_password": cfg.Auth.LDAP.BindPassword,
				"user_filter":   cfg.Auth.LDAP.UserFilter,
				"tls":           cfg.Auth.LDAP.TLS,
			},
		},
		"encryption": map[string]any{
			"key": cfg.Encryption.Key,
		},
	}
}
