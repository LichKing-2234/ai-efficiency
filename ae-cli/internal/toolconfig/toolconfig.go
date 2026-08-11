package toolconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	envFileRelativePath = ".ae-cli/env.sh"
	managedBlockStart   = "# BEGIN AE-CLI MANAGED"
	managedBlockEnd     = "# END AE-CLI MANAGED"
	codexModel          = "gpt-5.4"
)

type Provider struct {
	ID           int
	Name         string
	DisplayName  string
	BaseURL      string
	APIKey       string
	APIKeyID     int64
	DefaultModel string
	IsPrimary    bool
	Credentials  []PlatformCredential
}

type PlatformCredential struct {
	Platform string
	GroupID  string
	APIKey   string
	APIKeyID int64
	Status   string
}

type InstalledTool struct {
	Name string
	Path string
}

type ConfiguredTool struct {
	Name  string
	Paths []string
}

type Result struct {
	Configured []ConfiguredTool
}

type Options struct {
	HomeDir   string
	ShellPath string
	Provider  Provider
	Tools     []InstalledTool
	DryRun    bool
}

func SelectProvider(providers []Provider, explicit string) (Provider, error) {
	if len(providers) == 0 {
		return Provider{}, fmt.Errorf("no providers available")
	}
	if explicit != "" {
		for _, provider := range providers {
			if provider.Name == explicit || provider.DisplayName == explicit {
				return provider, nil
			}
		}
		return Provider{}, fmt.Errorf("provider %q not found", explicit)
	}
	for _, provider := range providers {
		if provider.IsPrimary {
			return provider, nil
		}
	}
	return providers[0], nil
}

func DetectInstalledTools(toolNames []string) ([]InstalledTool, error) {
	var tools []InstalledTool
	for _, name := range toolNames {
		path, err := exec.LookPath(name)
		if err == nil {
			tools = append(tools, InstalledTool{Name: name, Path: path})
			continue
		}
		if path, ok := detectAppBackedTool(name); ok {
			tools = append(tools, InstalledTool{Name: name, Path: path})
		}
	}
	return tools, nil
}

func detectAppBackedTool(name string) (string, bool) {
	switch name {
	case "codex":
		return firstExistingDir(codexAppBundleCandidates())
	default:
		return "", false
	}
}

func codexAppBundleCandidates() []string {
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, "Applications", "ChatGPT.app"))
	}
	candidates = append(candidates, "/Applications/ChatGPT.app")
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, "Applications", "Codex.app"))
	}
	candidates = append(candidates, "/Applications/Codex.app")
	return candidates
}

func firstExistingDir(paths []string) (string, bool) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path, true
		}
	}
	return "", false
}

func ConfigureTools(opts Options) (Result, error) {
	if strings.TrimSpace(opts.HomeDir) == "" {
		return Result{}, fmt.Errorf("home directory is required")
	}
	if strings.TrimSpace(opts.Provider.BaseURL) == "" {
		return Result{}, fmt.Errorf("provider base URL is required")
	}

	var (
		result  Result
		envVars = map[string]string{}
		rcPath  string
	)
	for _, tool := range opts.Tools {
		platform, ok := toolPlatform(tool.Name)
		if !ok {
			continue
		}
		credential, ok := opts.Provider.CredentialForPlatform(platform)
		if !ok {
			continue
		}
		switch tool.Name {
		case "codex":
			paths, err := configureCodex(opts, credential)
			if err != nil {
				return Result{}, err
			}
			result.Configured = append(result.Configured, ConfiguredTool{Name: tool.Name, Paths: paths})
		case "claude":
			path, err := configureClaude(opts, credential)
			if err != nil {
				return Result{}, err
			}
			result.Configured = append(result.Configured, ConfiguredTool{Name: tool.Name, Paths: []string{path}})
		case "gemini":
			result.Configured = append(result.Configured, ConfiguredTool{Name: tool.Name})
			envVars["GEMINI_API_KEY"] = credential.APIKey
			envVars["GOOGLE_GEMINI_BASE_URL"] = opts.Provider.BaseURL
		}
	}

	if len(envVars) > 0 {
		envPath := filepath.Join(opts.HomeDir, envFileRelativePath)
		if !opts.DryRun {
			if err := writeManagedEnvFile(envPath, envVars); err != nil {
				return Result{}, err
			}
			var err error
			rcPath, err = ensureShellSourcesEnvFile(opts.HomeDir, opts.ShellPath, envPath)
			if err != nil {
				return Result{}, err
			}
		} else {
			rcPath = shellRCPath(opts.HomeDir, opts.ShellPath)
		}
		for i := range result.Configured {
			switch result.Configured[i].Name {
			case "gemini":
				result.Configured[i].Paths = append(result.Configured[i].Paths, envPath, rcPath)
			}
		}
	}

	return result, nil
}

func (p Provider) CredentialForPlatform(platform string) (PlatformCredential, bool) {
	platform = strings.TrimSpace(platform)
	for _, credential := range p.Credentials {
		if strings.TrimSpace(credential.APIKey) == "" {
			continue
		}
		if status := strings.TrimSpace(credential.Status); status != "" && !strings.EqualFold(status, "active") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(credential.Platform), platform) {
			return credential, true
		}
	}
	if len(p.Credentials) == 0 && strings.TrimSpace(p.APIKey) != "" {
		return PlatformCredential{
			Platform: platform,
			APIKey:   p.APIKey,
			APIKeyID: p.APIKeyID,
			Status:   "active",
		}, true
	}
	return PlatformCredential{}, false
}

func toolPlatform(tool string) (string, bool) {
	switch tool {
	case "codex":
		return "openai", true
	case "claude":
		return "anthropic", true
	case "gemini":
		return "gemini", true
	default:
		return "", false
	}
}

func configureCodex(opts Options, credential PlatformCredential) ([]string, error) {
	configPath := filepath.Join(opts.HomeDir, ".codex", "config.toml")
	authPath := filepath.Join(opts.HomeDir, ".codex", "auth.json")
	if opts.DryRun {
		return []string{configPath, authPath}, nil
	}
	providerName := strings.TrimSpace(opts.Provider.Name)
	if providerName == "" {
		return nil, fmt.Errorf("provider name is required for codex config")
	}
	cfg := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil && len(data) > 0 {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse codex config: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read codex config: %w", err)
	}
	delete(cfg, "openai_base_url")
	cfg["model_provider"] = providerName
	cfg["model"] = codexModel
	cfg["review_model"] = codexModel
	cfg["model_reasoning_effort"] = "xhigh"
	cfg["disable_response_storage"] = true
	cfg["network_access"] = "enabled"
	cfg["windows_wsl_setup_acknowledged"] = true
	cfg["model_context_window"] = 1000000
	cfg["model_auto_compact_token_limit"] = 900000
	modelProviders := ensureNestedMap(cfg, "model_providers")
	codexProvider := ensureNestedMap(modelProviders, providerName)
	codexProvider["name"] = providerName
	codexProvider["base_url"] = opts.Provider.BaseURL
	codexProvider["wire_api"] = "responses"
	codexProvider["requires_openai_auth"] = true
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return nil, fmt.Errorf("create codex config dir: %w", err)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal codex config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("write codex config: %w", err)
	}
	auth := map[string]any{
		"OPENAI_API_KEY": credential.APIKey,
	}
	if err := writeJSONObject(authPath, auth); err != nil {
		return nil, fmt.Errorf("write codex auth: %w", err)
	}
	return []string{configPath, authPath}, nil
}

func configureClaude(opts Options, credential PlatformCredential) (string, error) {
	path := filepath.Join(opts.HomeDir, ".claude", "settings.json")
	if opts.DryRun {
		return path, nil
	}
	cfg, err := readJSONObject(path)
	if err != nil {
		return "", fmt.Errorf("read claude settings: %w", err)
	}
	env := ensureNestedMap(cfg, "env")
	delete(env, "ANTHROPIC_API_KEY")
	env["ANTHROPIC_BASE_URL"] = opts.Provider.BaseURL
	env["ANTHROPIC_AUTH_TOKEN"] = credential.APIKey
	env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	env["CLAUDE_CODE_ATTRIBUTION_HEADER"] = "0"
	if err := writeJSONObject(path, cfg); err != nil {
		return "", fmt.Errorf("write claude settings: %w", err)
	}
	return path, nil
}

func readJSONObject(path string) (map[string]any, error) {
	cfg := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func writeJSONObject(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func ensureNestedMap(root map[string]any, key string) map[string]any {
	if root == nil {
		root = map[string]any{}
	}
	if existing, ok := root[key]; ok {
		if m, ok := existing.(map[string]any); ok {
			return m
		}
	}
	m := map[string]any{}
	root[key] = m
	return m
}

func writeManagedEnvFile(path string, vars map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read env file: %w", err)
	}

	body := renderManagedEnvBlock(vars)
	newContent := replaceManagedBlock(existing, body)
	if err := os.WriteFile(path, []byte(newContent), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	return nil
}

func renderManagedEnvBlock(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := []string{managedBlockStart}
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("export %s=%s", key, shellQuote(vars[key])))
	}
	lines = append(lines, managedBlockEnd)
	return strings.Join(lines, "\n")
}

func replaceManagedBlock(existing, block string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return block + "\n"
	}
	start := strings.Index(existing, managedBlockStart)
	end := strings.Index(existing, managedBlockEnd)
	if start >= 0 && end >= start {
		end += len(managedBlockEnd)
		prefix := strings.TrimSpace(existing[:start])
		suffix := strings.TrimSpace(existing[end:])
		parts := make([]string, 0, 3)
		if prefix != "" {
			parts = append(parts, prefix)
		}
		parts = append(parts, block)
		if suffix != "" {
			parts = append(parts, suffix)
		}
		return strings.Join(parts, "\n\n") + "\n"
	}
	return existing + "\n\n" + block + "\n"
}

func ensureShellSourcesEnvFile(homeDir, shellPath, envPath string) (string, error) {
	rcPath := shellRCPath(homeDir, shellPath)
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o700); err != nil {
		return "", fmt.Errorf("create shell rc dir: %w", err)
	}
	var body string
	if data, err := os.ReadFile(rcPath); err == nil {
		body = string(data)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read shell rc: %w", err)
	}
	sourceLine := `[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"`
	if strings.Contains(body, sourceLine) {
		return rcPath, nil
	}
	if strings.TrimSpace(body) != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n# Added by ae-cli discover\n" + sourceLine + "\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write shell rc: %w", err)
	}
	return rcPath, nil
}

func shellRCPath(homeDir, shellPath string) string {
	switch filepath.Base(shellPath) {
	case "bash":
		return filepath.Join(homeDir, ".bashrc")
	case "zsh":
		return filepath.Join(homeDir, ".zshrc")
	default:
		return filepath.Join(homeDir, ".profile")
	}
}

func shellQuote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + replacer.Replace(value) + `"`
}
