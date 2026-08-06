package toolconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ConfigureCodexOTLP enables Codex native trace-safe OTLP/HTTP JSON export.
// Prompt logging remains disabled and no local Collector is installed.
func ConfigureCodexOTLP(homeDir, endpoint, token string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if endpoint == "" || token == "" {
		return "", fmt.Errorf("Codex OTLP endpoint and token are required")
	}
	path := filepath.Join(homeDir, ".codex", "config.toml")
	config := map[string]any{}
	if payload, err := os.ReadFile(path); err == nil && len(payload) > 0 {
		if err := toml.Unmarshal(payload, &config); err != nil {
			return "", fmt.Errorf("parse Codex config: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	otel := ensureNestedMap(config, "otel")
	otel["environment"] = "production"
	otel["log_user_prompt"] = false
	otel["trace_exporter"] = map[string]any{
		"otlp-http": map[string]any{
			"endpoint": endpoint,
			"protocol": "json",
			"headers": map[string]any{
				"Authorization": "Bearer " + token,
			},
		},
	}
	payload, err := toml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal Codex config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
