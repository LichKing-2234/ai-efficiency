package toolconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

type CodexConfig struct {
	BaseURL  string
	TokenEnv string
	Model    string
}

func WriteCodexSessionConfig(workspaceRoot string, cfg CodexConfig) error {
	content := fmt.Sprintf(`model = %q
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
base_url = %q
env_key = %q
wire_api = "responses"
supports_websockets = false
`, cfg.Model, cfg.BaseURL, cfg.TokenEnv)

	configPath := filepath.Join(workspaceRoot, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}
