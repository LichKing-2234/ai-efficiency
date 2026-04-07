package toolconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CodexConfig struct {
	BaseURL  string
	TokenEnv string
	Model    string
	SelfPath string
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

[features]
codex_hooks = true
`, cfg.Model, cfg.BaseURL, cfg.TokenEnv)

	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return err
	}

	command := hookCommand(strings.TrimSpace(cfg.SelfPath), "codex")
	hooks := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "startup|resume",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"UserPromptSubmit": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(codexHome, "hooks.json"), append(data, '\n'), 0o600)
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

func hookCommand(selfPath, tool string) string {
	selfPath = strings.TrimSpace(selfPath)
	if selfPath == "" {
		selfPath = "ae-cli"
	}
	return shellQuote(selfPath) + " hook session-event --tool " + tool
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
