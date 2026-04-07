package toolconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ClaudeEnv struct {
	BaseURL string
	Token   string
}

type ClaudeHookConfig struct {
	SessionID    string
	SelfPath     string
	ProxyBaseURL string
	ProxyToken   string
}

func BuildClaudeEnv(cfg ClaudeEnv) map[string]string {
	return map[string]string{
		"ANTHROPIC_BASE_URL":   cfg.BaseURL,
		"ANTHROPIC_AUTH_TOKEN": cfg.Token,
	}
}

func ApplyClaudeProxyEnv(env map[string]string, cfg ClaudeEnv) map[string]string {
	if env == nil {
		env = map[string]string{}
	}
	delete(env, "ANTHROPIC_API_KEY")
	for k, v := range BuildClaudeEnv(cfg) {
		env[k] = v
	}
	return env
}

func WriteClaudeSessionConfig(workspaceRoot string, cfg ClaudeHookConfig) error {
	settingsPath := claudeSettingsPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return err
	}

	doc, err := loadClaudeSettings(settingsPath)
	if err != nil {
		return err
	}
	envDoc := ensureEnvMap(doc)
	if strings.TrimSpace(cfg.ProxyBaseURL) != "" {
		envDoc["ANTHROPIC_BASE_URL"] = strings.TrimSpace(cfg.ProxyBaseURL)
	}
	if strings.TrimSpace(cfg.ProxyToken) != "" {
		envDoc["ANTHROPIC_AUTH_TOKEN"] = strings.TrimSpace(cfg.ProxyToken)
	}
	hooksDoc := ensureHooksMap(doc)
	removeManagedClaudeHooks(hooksDoc, "")

	command := hookCommand(strings.TrimSpace(cfg.SelfPath), "claude") +
		" # ae-session-managed session=" + strings.TrimSpace(cfg.SessionID) + " tool=claude"
	for eventName, matcher := range map[string]string{
		"SessionStart":     "",
		"UserPromptSubmit": "",
		"PreToolUse":       "",
		"PostToolUse":      "",
		"Stop":             "",
	} {
		hooksDoc[eventName] = append(normalizeHookGroups(hooksDoc[eventName]), map[string]any{
			"matcher": matcher,
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": command,
				},
			},
		})
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0o600)
}

func CleanupClaudeSessionConfig(workspaceRoot, sessionID string) error {
	settingsPath := claudeSettingsPath(workspaceRoot)
	doc, err := loadClaudeSettings(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	hooksDoc := ensureHooksMap(doc)
	removeManagedClaudeHooks(hooksDoc, sessionID)
	if len(hooksDoc) == 0 {
		delete(doc, "hooks")
	}
	if envDoc, ok := doc["env"].(map[string]any); ok && envDoc != nil {
		delete(envDoc, "ANTHROPIC_BASE_URL")
		delete(envDoc, "ANTHROPIC_AUTH_TOKEN")
		if len(envDoc) == 0 {
			delete(doc, "env")
		}
	}

	if len(doc) == 0 {
		if err := os.Remove(settingsPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(filepath.Dir(settingsPath)); err != nil && !os.IsNotExist(err) {
			return nil
		}
		return nil
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0o600)
}

func claudeSettingsPath(workspaceRoot string) string {
	return filepath.Join(strings.TrimSpace(workspaceRoot), ".claude", "settings.local.json")
}

func loadClaudeSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse claude settings: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func ensureHooksMap(doc map[string]any) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	if existing, ok := doc["hooks"].(map[string]any); ok && existing != nil {
		return existing
	}
	hooksDoc := map[string]any{}
	doc["hooks"] = hooksDoc
	return hooksDoc
}

func ensureEnvMap(doc map[string]any) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	if existing, ok := doc["env"].(map[string]any); ok && existing != nil {
		return existing
	}
	envDoc := map[string]any{}
	doc["env"] = envDoc
	return envDoc
}

func normalizeHookGroups(value any) []any {
	switch v := value.(type) {
	case []any:
		return append([]any(nil), v...)
	case nil:
		return nil
	default:
		return []any{v}
	}
}

func removeManagedClaudeHooks(hooksDoc map[string]any, sessionID string) {
	sessionMarker := ""
	if strings.TrimSpace(sessionID) != "" {
		sessionMarker = "session=" + strings.TrimSpace(sessionID)
	}
	managedMarker := "ae-session-managed"

	for eventName, value := range hooksDoc {
		groups := normalizeHookGroups(value)
		filteredGroups := make([]any, 0, len(groups))
		for _, group := range groups {
			groupMap, ok := group.(map[string]any)
			if !ok {
				filteredGroups = append(filteredGroups, group)
				continue
			}

			hooks := normalizeHookGroups(groupMap["hooks"])
			filteredHooks := make([]any, 0, len(hooks))
			for _, hook := range hooks {
				hookMap, ok := hook.(map[string]any)
				if !ok {
					filteredHooks = append(filteredHooks, hook)
					continue
				}
				command := strings.TrimSpace(asHookString(hookMap["command"]))
				if !strings.Contains(command, managedMarker) {
					filteredHooks = append(filteredHooks, hook)
					continue
				}
				if sessionMarker != "" && !strings.Contains(command, sessionMarker) {
					filteredHooks = append(filteredHooks, hook)
					continue
				}
			}

			if len(filteredHooks) == 0 {
				continue
			}
			groupMap["hooks"] = filteredHooks
			filteredGroups = append(filteredGroups, groupMap)
		}

		if len(filteredGroups) == 0 {
			delete(hooksDoc, eventName)
			continue
		}
		hooksDoc[eventName] = filteredGroups
	}
}

func asHookString(value any) string {
	s, _ := value.(string)
	return s
}
