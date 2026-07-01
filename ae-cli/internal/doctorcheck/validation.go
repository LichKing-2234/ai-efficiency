package doctorcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"

	Match       = "match"
	Mismatch    = "mismatch"
	Unavailable = "unavailable"
	Missing     = "missing"

	CredentialMatch       = "match"
	CredentialMismatch    = "mismatch"
	CredentialMissing     = "missing"
	CredentialUnavailable = "unavailable"
)

type ToolState struct {
	Name           string
	ExecutablePath string
	Version        string
	Probeable      bool
	Missing        bool
}

type ValidateOptions struct {
	HomeDir           string
	ShellPath         string
	Provider          toolconfig.Provider
	ProviderAvailable bool
	ProviderSource    string
	Tools             []ToolState
}

type ConfigReport struct {
	ProviderName   string
	ProviderSource string
	Results        []ConfigResult
}

func (r ConfigReport) ByName(name string) *ConfigResult {
	for i := range r.Results {
		if r.Results[i].Name == name {
			return &r.Results[i]
		}
	}
	return nil
}

type ConfigResult struct {
	Name             string
	Status           string
	ConfigPath       string
	AuthPath         string
	ExecutablePath   string
	Version          string
	BaseURLStatus    string
	CredentialStatus string
	Model            string
	ExpectedModel    string
	ModelContract    string
	Probeable        bool
	ProbeEnv         map[string]string
	Details          []string
	SkipReason       string
}

func ValidateTools(opts ValidateOptions) ConfigReport {
	source := strings.TrimSpace(opts.ProviderSource)
	if source == "" {
		source = "user/providers"
	}
	report := ConfigReport{
		ProviderName:   strings.TrimSpace(opts.Provider.Name),
		ProviderSource: source,
	}
	for _, tool := range opts.Tools {
		report.Results = append(report.Results, validateOneTool(opts, tool))
	}
	sort.SliceStable(report.Results, func(i, j int) bool {
		return toolOrder(report.Results[i].Name) < toolOrder(report.Results[j].Name)
	})
	return report
}

func validateOneTool(opts ValidateOptions, tool ToolState) ConfigResult {
	result := ConfigResult{
		Name:             strings.TrimSpace(tool.Name),
		ExecutablePath:   strings.TrimSpace(tool.ExecutablePath),
		Version:          strings.TrimSpace(tool.Version),
		BaseURLStatus:    Unavailable,
		CredentialStatus: CredentialUnavailable,
		ModelContract:    Unavailable,
		Probeable:        tool.Probeable,
	}
	platform, ok := toolPlatform(result.Name)
	if !ok {
		result.Status = StatusSkipped
		result.SkipReason = "unsupported tool"
		return result
	}
	credential, hasCredential := opts.Provider.CredentialForPlatform(platform)
	if opts.ProviderAvailable {
		result.CredentialStatus = CredentialMissing
	}
	if tool.Missing {
		if hasCredential {
			result.Status = StatusFailed
		} else {
			result.Status = StatusSkipped
		}
		result.SkipReason = "executable not found"
		return result
	}
	if !tool.Probeable {
		result.SkipReason = "callable CLI not found"
	}
	switch result.Name {
	case "codex":
		validateCodex(opts, credential, hasCredential, &result)
	case "claude":
		validateClaude(opts, credential, hasCredential, &result)
	case "gemini":
		validateGemini(opts, credential, hasCredential, &result)
	}
	if len(result.Details) == 0 && result.Status == "" {
		result.Status = StatusOK
	}
	if result.Status == "" {
		result.Status = StatusFailed
	}
	if result.Status != StatusOK {
		result.Probeable = false
	}
	return result
}

func validateCodex(opts ValidateOptions, credential toolconfig.PlatformCredential, hasCredential bool, result *ConfigResult) {
	configPath := filepath.Join(opts.HomeDir, ".codex", "config.toml")
	authPath := filepath.Join(opts.HomeDir, ".codex", "auth.json")
	result.ConfigPath = configPath
	result.AuthPath = authPath
	cfg := map[string]any{}
	data, err := os.ReadFile(configPath)
	if err != nil {
		result.Details = append(result.Details, "config missing")
		result.Status = StatusFailed
	} else if err := toml.Unmarshal(data, &cfg); err != nil {
		result.Details = append(result.Details, "config invalid")
		result.Status = StatusFailed
	}
	modelProvider := stringValue(cfg["model_provider"])
	if modelProvider == "" {
		result.Details = append(result.Details, "missing model_provider")
		result.Status = StatusFailed
	}
	result.Model = stringValue(cfg["model"])
	result.ExpectedModel = "gpt-5.4"
	if result.Model != "" {
		if result.Model == result.ExpectedModel {
			result.ModelContract = Match
		} else {
			result.ModelContract = Mismatch
		}
	}
	baseURL := ""
	if providers, ok := cfg["model_providers"].(map[string]any); ok && modelProvider != "" {
		if providerCfg, ok := providers[modelProvider].(map[string]any); ok {
			baseURL = stringValue(providerCfg["base_url"])
			if stringValue(providerCfg["wire_api"]) != "responses" {
				result.Details = append(result.Details, "wire_api is not responses")
				result.Status = StatusFailed
			}
			if boolValue(providerCfg["requires_openai_auth"]) != true {
				result.Details = append(result.Details, "requires_openai_auth is not true")
				result.Status = StatusFailed
			}
		}
	}
	if baseURL == "" {
		result.Details = append(result.Details, "missing provider base_url")
		result.Status = StatusFailed
	}
	result.BaseURLStatus = matchString(baseURL, opts.Provider.BaseURL, opts.ProviderAvailable)
	auth := map[string]any{}
	if err := readJSONFile(authPath, &auth); err != nil {
		result.Details = append(result.Details, "auth missing or invalid")
		result.Status = StatusFailed
	}
	key := stringValue(auth["OPENAI_API_KEY"])
	compareCredential(key, credential.APIKey, hasCredential, opts.ProviderAvailable, result)
}

func validateClaude(opts ValidateOptions, credential toolconfig.PlatformCredential, hasCredential bool, result *ConfigResult) {
	path := filepath.Join(opts.HomeDir, ".claude", "settings.json")
	result.ConfigPath = path
	cfg := map[string]any{}
	if err := readJSONFile(path, &cfg); err != nil {
		result.Details = append(result.Details, "settings missing or invalid")
		result.Status = StatusFailed
	}
	env, _ := cfg["env"].(map[string]any)
	baseURL := stringValue(env["ANTHROPIC_BASE_URL"])
	token := stringValue(env["ANTHROPIC_AUTH_TOKEN"])
	if baseURL == "" {
		result.Details = append(result.Details, "missing env.ANTHROPIC_BASE_URL")
		result.Status = StatusFailed
	}
	if token == "" {
		result.Details = append(result.Details, "missing env.ANTHROPIC_AUTH_TOKEN")
		result.Status = StatusFailed
	}
	if stringValue(env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"]) != "1" {
		result.Details = append(result.Details, "missing env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC")
		result.Status = StatusFailed
	}
	if stringValue(env["CLAUDE_CODE_ATTRIBUTION_HEADER"]) != "0" {
		result.Details = append(result.Details, "missing env.CLAUDE_CODE_ATTRIBUTION_HEADER")
		result.Status = StatusFailed
	}
	result.BaseURLStatus = matchString(baseURL, opts.Provider.BaseURL, opts.ProviderAvailable)
	compareCredential(token, credential.APIKey, hasCredential, opts.ProviderAvailable, result)
}

func validateGemini(opts ValidateOptions, credential toolconfig.PlatformCredential, hasCredential bool, result *ConfigResult) {
	envPath := filepath.Join(opts.HomeDir, ".ae-cli", "env.sh")
	rcPath := shellRCPath(opts.HomeDir, opts.ShellPath)
	result.ConfigPath = envPath
	result.AuthPath = rcPath
	vars, err := parseManagedEnvFile(envPath)
	if err != nil {
		result.Details = append(result.Details, err.Error())
		result.Status = StatusFailed
	}
	body, err := os.ReadFile(rcPath)
	if err != nil || !strings.Contains(string(body), `[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"`) {
		result.Details = append(result.Details, "shell rc does not source env.sh")
		result.Status = StatusFailed
	}
	baseURL := vars["GOOGLE_GEMINI_BASE_URL"]
	key := vars["GEMINI_API_KEY"]
	if key == "" {
		result.Details = append(result.Details, "missing GEMINI_API_KEY")
		result.Status = StatusFailed
	}
	if baseURL == "" {
		result.Details = append(result.Details, "missing GOOGLE_GEMINI_BASE_URL")
		result.Status = StatusFailed
	}
	result.ProbeEnv = vars
	result.BaseURLStatus = matchString(baseURL, opts.Provider.BaseURL, opts.ProviderAvailable)
	compareCredential(key, credential.APIKey, hasCredential, opts.ProviderAvailable, result)
}

func FormatConfigResult(result *ConfigResult) string {
	if result == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("%s:", result.Name), result.Status}
	if result.ConfigPath != "" {
		parts = append(parts, "config="+result.ConfigPath)
	}
	if result.AuthPath != "" && result.Name == "codex" {
		parts = append(parts, "auth=present")
	}
	if result.BaseURLStatus != "" {
		parts = append(parts, "base_url="+result.BaseURLStatus)
	}
	if result.CredentialStatus != "" {
		parts = append(parts, "credential="+result.CredentialStatus)
	}
	if result.Model != "" {
		parts = append(parts, "model="+result.Model)
	}
	if result.ModelContract == Mismatch {
		parts = append(parts, "model_contract=mismatch(expected="+result.ExpectedModel+")")
	}
	if result.SkipReason != "" {
		parts = append(parts, result.SkipReason)
	}
	if len(result.Details) > 0 {
		parts = append(parts, strings.Join(result.Details, "; "))
	}
	return RedactSecrets(strings.Join(parts, " "))
}

func RedactSecrets(s string) string {
	fields := strings.Fields(s)
	for i, field := range fields {
		trimmed := strings.Trim(field, `"'`)
		if strings.HasPrefix(trimmed, "sk-") {
			fields[i] = strings.Replace(field, trimmed, "sk-<redacted>", 1)
			continue
		}
		for _, key := range []string{"OPENAI_API_KEY=", "ANTHROPIC_AUTH_TOKEN=", "GEMINI_API_KEY="} {
			if strings.HasPrefix(trimmed, key) {
				fields[i] = strings.Replace(field, trimmed, key+"<redacted>", 1)
			}
		}
	}
	return strings.Join(fields, " ")
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

func toolOrder(tool string) int {
	switch tool {
	case "codex":
		return 0
	case "claude":
		return 1
	case "gemini":
		return 2
	default:
		return 99
	}
}

func readJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func boolValue(v any) bool {
	value, _ := v.(bool)
	return value
}

func matchString(got, want string, available bool) string {
	if !available {
		return Unavailable
	}
	if strings.TrimSpace(got) == "" {
		return Missing
	}
	if strings.TrimSpace(got) == strings.TrimSpace(want) {
		return Match
	}
	return Mismatch
}

func compareCredential(got, want string, hasCredential bool, providerAvailable bool, result *ConfigResult) {
	if !providerAvailable {
		result.CredentialStatus = CredentialUnavailable
		return
	}
	if strings.TrimSpace(got) == "" {
		result.CredentialStatus = CredentialMissing
		result.Status = StatusFailed
		return
	}
	if !hasCredential {
		result.CredentialStatus = CredentialMissing
		return
	}
	if got == want {
		result.CredentialStatus = CredentialMatch
		return
	}
	result.CredentialStatus = CredentialMismatch
	result.Status = StatusFailed
}

func parseManagedEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("env missing or invalid")
	}
	body := string(data)
	start := strings.Index(body, "# BEGIN AE-CLI MANAGED")
	end := strings.Index(body, "# END AE-CLI MANAGED")
	if start < 0 || end < start {
		return nil, fmt.Errorf("managed block missing")
	}
	vars := map[string]string{}
	for _, line := range strings.Split(body[start:end], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "export ") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return vars, nil
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
