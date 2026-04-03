package toolconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CodexConfig struct {
	BaseURL  string
	TokenEnv string
	Model    string
}

func WriteCodexSessionConfig(codexHome string, cfg CodexConfig) error {
	content := fmt.Sprintf(`model = %q
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
base_url = %q
env_key = %q
wire_api = "responses"
supports_websockets = false
`, cfg.Model, cfg.BaseURL, cfg.TokenEnv)

	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}

func CleanupLegacyWorkspaceCodexConfig(workspaceRoot string) error {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil
	}

	codexDir := filepath.Join(workspaceRoot, ".codex")
	configPath := filepath.Join(codexDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !isAEEfficiencyManagedLegacyCodexConfig(string(data)) {
		return nil
	}
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(codexDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		if err := os.Remove(codexDir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func isAEEfficiencyManagedLegacyCodexConfig(content string) bool {
	required := []string{
		`model_provider = "ae_local_proxy"`,
		`[model_providers.ae_local_proxy]`,
		`name = "AI Efficiency Local Proxy"`,
		`env_key = "AE_LOCAL_PROXY_TOKEN"`,
		`wire_api = "responses"`,
	}
	for _, marker := range required {
		if !strings.Contains(content, marker) {
			return false
		}
	}
	return true
}
