# ae-cli Doctor Tool Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `ae-cli doctor` so the default diagnostic validates `ae-cli discover` output, probes Codex/Claude/Gemini through the actual local CLI commands, and uses a human-run diagnostic timeout for repo eligibility.

**Architecture:** Add `ae-cli/internal/doctorcheck` for local tool configuration validation, provider/credential matching, secret-safe diagnostic formatting, and command probing. Keep `ae-cli/cmd/doctor.go` as the orchestration layer that calls existing attribution/hook checks, fetches providers through the existing client, prints doctorcheck results, and runs repo eligibility with a separate timeout.

**Tech Stack:** Go, cobra, standard `os/exec`, `context`, `encoding/json`, `github.com/pelletier/go-toml/v2`, existing `ae-cli/internal/client`, `ae-cli/internal/toolconfig`, and current `go test` suite.

---

## File Structure

- Create `ae-cli/internal/doctorcheck/validation.go`: data types, provider credential matching, config file parsing, secret redaction, and tool configuration validation.
- Create `ae-cli/internal/doctorcheck/probe.go`: command runner interface, default `exec.CommandContext` runner, per-tool probe commands, timeout handling, stdout/stderr truncation, and environment injection for Gemini.
- Create `ae-cli/internal/doctorcheck/validation_test.go`: focused tests for Codex, Claude, Gemini, provider matching, model mismatch, missing executable state, and redaction.
- Create `ae-cli/internal/doctorcheck/probe_test.go`: fake-runner tests for successful output, timeout, non-zero exit, empty output, and Gemini env injection.
- Create `ae-cli/cmd/doctor_tool_test.go`: command-level tests proving `ae-cli doctor` prints `Tool configuration` and `Tool probe` while continuing on partial failures.
- Modify `ae-cli/cmd/doctor.go`: call doctorcheck, fetch provider contract via `apiClient.ListProviders`, print tool validation/probe output, and use a doctor-specific repo eligibility timeout.
- Modify `ae-cli/cmd/cutover_test.go`: keep existing doctor smoke assertions current if the output order changes.

## Task 1: Add Doctorcheck Validation Tests

**Files:**
- Create: `ae-cli/internal/doctorcheck/validation_test.go`
- Create: `ae-cli/internal/doctorcheck/validation.go`

- [x] **Step 1: Create the empty package file**

Create `ae-cli/internal/doctorcheck/validation.go` with the package and imports only:

```go
package doctorcheck
```

- [x] **Step 2: Write failing validation tests**

Create `ae-cli/internal/doctorcheck/validation_test.go`:

```go
package doctorcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	writeFile(t, path, string(data))
}

func testProvider() toolconfig.Provider {
	return toolconfig.Provider{
		Name:      "sub2api",
		BaseURL:   "https://relay.example.com/v1",
		IsPrimary: true,
		Credentials: []toolconfig.PlatformCredential{
			{Platform: "openai", APIKey: "sk-openai", Status: "active"},
			{Platform: "anthropic", APIKey: "sk-anthropic", Status: "active"},
			{Platform: "gemini", APIKey: "sk-gemini", Status: "active"},
		},
	}
}

func TestValidateToolsReportsConfiguredToolsAndCredentialMatches(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
model = 'gpt-5.5'
review_model = 'gpt-5.4'

[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
`)
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{"OPENAI_API_KEY": "sk-openai"})
	writeJSON(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{
		"env": map[string]any{
			"ANTHROPIC_BASE_URL": "https://relay.example.com/v1",
			"ANTHROPIC_AUTH_TOKEN": "sk-anthropic",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
		},
	})
	writeFile(t, filepath.Join(home, ".ae-cli", "env.sh"), strings.Join([]string{
		"# BEGIN AE-CLI MANAGED",
		`export GEMINI_API_KEY="sk-gemini"`,
		`export GOOGLE_GEMINI_BASE_URL="https://relay.example.com/v1"`,
		"# END AE-CLI MANAGED",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(home, ".zshrc"), `[ -f "$HOME/.ae-cli/env.sh" ] && source "$HOME/.ae-cli/env.sh"`+"\n")

	report := ValidateTools(ValidateOptions{
		HomeDir:   home,
		ShellPath: "/bin/zsh",
		Provider:  testProvider(),
		ProviderAvailable: true,
		Tools: []ToolState{
			{Name: "codex", ExecutablePath: "/tmp/bin/codex", Probeable: true},
			{Name: "claude", ExecutablePath: "/tmp/bin/claude", Probeable: true},
			{Name: "gemini", ExecutablePath: "/tmp/bin/gemini", Probeable: true},
		},
	})

	if report.ProviderName != "sub2api" || report.ProviderSource != "user/providers" {
		t.Fatalf("provider = %q source=%q", report.ProviderName, report.ProviderSource)
	}
	for _, name := range []string{"codex", "claude", "gemini"} {
		result := report.ByName(name)
		if result == nil {
			t.Fatalf("missing result for %s", name)
		}
		if result.Status != StatusOK {
			t.Fatalf("%s status = %s details=%v", name, result.Status, result.Details)
		}
		if result.CredentialStatus != CredentialMatch {
			t.Fatalf("%s credential status = %s", name, result.CredentialStatus)
		}
		if result.BaseURLStatus != Match {
			t.Fatalf("%s base url status = %s", name, result.BaseURLStatus)
		}
	}
	if got := report.ByName("codex").ModelContract; got != Mismatch {
		t.Fatalf("codex model contract = %s, want mismatch", got)
	}
}

func TestValidateToolsDoesNotPrintSecretOnCredentialMismatch(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
`)
	writeJSON(t, filepath.Join(home, ".codex", "auth.json"), map[string]any{"OPENAI_API_KEY": "wrong-secret"})

	report := ValidateTools(ValidateOptions{
		HomeDir:   home,
		ShellPath: "/bin/zsh",
		Provider:  testProvider(),
		ProviderAvailable: true,
		Tools: []ToolState{{Name: "codex", ExecutablePath: "/tmp/bin/codex", Probeable: true}},
	})

	line := FormatConfigResult(report.ByName("codex"))
	if strings.Contains(line, "wrong-secret") || strings.Contains(line, "sk-openai") {
		t.Fatalf("formatted line leaked secret: %s", line)
	}
	if !strings.Contains(line, "credential=mismatch") {
		t.Fatalf("formatted line = %s, want credential mismatch", line)
	}
}

func TestValidateToolsReportsMissingExecutableAsFailedWhenCredentialExists(t *testing.T) {
	report := ValidateTools(ValidateOptions{
		HomeDir:   t.TempDir(),
		ShellPath: "/bin/zsh",
		Provider:  testProvider(),
		ProviderAvailable: true,
		Tools: []ToolState{{Name: "gemini", Missing: true}},
	})
	result := report.ByName("gemini")
	if result.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	if result.SkipReason != "executable not found" {
		t.Fatalf("skip reason = %q", result.SkipReason)
	}
}

func TestValidateToolsReportsMissingExecutableAsSkippedWithoutCredential(t *testing.T) {
	provider := toolconfig.Provider{Name: "sub2api", BaseURL: "https://relay.example.com/v1", IsPrimary: true}
	report := ValidateTools(ValidateOptions{
		HomeDir:   t.TempDir(),
		ShellPath: "/bin/zsh",
		Provider:  provider,
		ProviderAvailable: true,
		Tools: []ToolState{{Name: "claude", Missing: true}},
	})
	result := report.ByName("claude")
	if result.Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", result.Status)
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck -run 'ValidateTools' -count=1
```

Expected: FAIL with undefined identifiers such as `ValidateTools`, `ValidateOptions`, `ToolState`, `StatusOK`, and `FormatConfigResult`.

## Task 2: Implement Doctorcheck Validation

**Files:**
- Modify: `ae-cli/internal/doctorcheck/validation.go`
- Test: `ae-cli/internal/doctorcheck/validation_test.go`

- [x] **Step 1: Add validation types and format helpers**

Replace `ae-cli/internal/doctorcheck/validation.go` with:

```go
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
		ProviderName: strings.TrimSpace(opts.Provider.Name),
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
		if hasCredential {
			result.CredentialStatus = CredentialMissing
		} else {
			result.CredentialStatus = CredentialMissing
		}
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
	replacements := []string{"sk-", "OPENAI_API_KEY=", "ANTHROPIC_AUTH_TOKEN=", "GEMINI_API_KEY="}
	for _, marker := range replacements {
		if strings.Contains(s, marker) {
			s = strings.ReplaceAll(s, marker, marker+"<redacted>")
		}
	}
	return s
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
```

- [x] **Step 2: Run validation tests**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck -run 'ValidateTools' -count=1
```

Expected: PASS.

- [x] **Step 3: Commit validation package slice**

```bash
git add ae-cli/internal/doctorcheck/validation.go ae-cli/internal/doctorcheck/validation_test.go
git commit -m "feat(ae-cli): validate doctor tool configuration"
```

## Task 3: Add Tool Probe Runner

**Files:**
- Create: `ae-cli/internal/doctorcheck/probe.go`
- Create: `ae-cli/internal/doctorcheck/probe_test.go`

- [x] **Step 1: Write failing probe tests**

Create `ae-cli/internal/doctorcheck/probe_test.go`:

```go
package doctorcheck

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []ProbeCommand
	run   func(context.Context, ProbeCommand) CommandResult
}

func (f *fakeRunner) Run(ctx context.Context, cmd ProbeCommand) CommandResult {
	f.calls = append(f.calls, cmd)
	return f.run(ctx, cmd)
}

func TestProbeToolsRunsConfiguredCommands(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		return CommandResult{Stdout: "AE_DOCTOR_OK\n"}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: 30 * time.Second,
		Runner:  runner,
		Configs: []ConfigResult{
			{Name: "codex", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/codex"},
			{Name: "claude", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/claude"},
			{Name: "gemini", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/gemini", ProbeEnv: map[string]string{"GEMINI_API_KEY": "sk-gemini"}},
		},
	})
	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3", len(results))
	}
	for _, result := range results {
		if result.Status != StatusOK {
			t.Fatalf("%s status = %s message=%s", result.Name, result.Status, result.Message)
		}
	}
	if runner.calls[0].Args[0] != "--ask-for-approval" || runner.calls[0].Args[2] != "exec" {
		t.Fatalf("codex args = %v", runner.calls[0].Args)
	}
	if runner.calls[1].Args[0] != "-p" {
		t.Fatalf("claude args = %v", runner.calls[1].Args)
	}
	if runner.calls[2].Env["GEMINI_API_KEY"] != "sk-gemini" {
		t.Fatalf("gemini env = %+v", runner.calls[2].Env)
	}
}

func TestProbeToolsReportsTimeoutAndRedactsOutput(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		return CommandResult{Err: context.DeadlineExceeded, Stderr: "failed with sk-secret"}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: time.Millisecond,
		Runner:  runner,
		Configs: []ConfigResult{{Name: "codex", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/codex"}},
	})
	if results[0].Status != StatusFailed {
		t.Fatalf("status = %s, want failed", results[0].Status)
	}
	line := FormatProbeResult(results[0])
	if strings.Contains(line, "sk-secret") {
		t.Fatalf("line leaked secret: %s", line)
	}
	if !strings.Contains(line, "timeout") {
		t.Fatalf("line = %s, want timeout", line)
	}
}

func TestProbeToolsSkipsConfigurationFailures(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		t.Fatalf("runner should not be called")
		return CommandResult{}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: 30 * time.Second,
		Runner:  runner,
		Configs: []ConfigResult{{Name: "claude", Status: StatusFailed, Probeable: false, SkipReason: "configuration failed"}},
	})
	if results[0].Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", results[0].Status)
	}
}

func TestProbeToolsReportsNonZeroAndEmptyStdout(t *testing.T) {
	for name, commandResult := range map[string]CommandResult{
		"nonzero": {ExitCode: 2, Stderr: "bad"},
		"empty":   {Stdout: ""},
		"error":   {Err: errors.New("spawn failed")},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
				return commandResult
			}}
			results := ProbeTools(context.Background(), ProbeOptions{
				Timeout: 30 * time.Second,
				Runner:  runner,
				Configs: []ConfigResult{{Name: "gemini", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/gemini"}},
			})
			if results[0].Status != StatusFailed {
				t.Fatalf("status = %s, want failed", results[0].Status)
			}
		})
	}
}
```

- [x] **Step 2: Run probe tests to verify they fail**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck -run 'ProbeTools' -count=1
```

Expected: FAIL with undefined identifiers such as `ProbeTools`, `ProbeOptions`, `ProbeCommand`, and `CommandResult`.

- [x] **Step 3: Implement probe runner**

Create `ae-cli/internal/doctorcheck/probe.go`:

```go
package doctorcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const probePrompt = "Reply exactly: AE_DOCTOR_OK"

type ProbeCommand struct {
	Name string
	Path string
	Args []string
	Env  map[string]string
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	Duration time.Duration
}

type CommandRunner interface {
	Run(context.Context, ProbeCommand) CommandResult
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command ProbeCommand) CommandResult {
	started := time.Now()
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	env := os.Environ()
	for key, value := range command.Env {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err: err,
		Duration: time.Since(started),
	}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}

type ProbeOptions struct {
	Timeout time.Duration
	Runner  CommandRunner
	Configs []ConfigResult
}

type ProbeResult struct {
	Name     string
	Status   string
	Duration time.Duration
	Output   string
	Message  string
}

func ProbeTools(ctx context.Context, opts ProbeOptions) []ProbeResult {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	results := make([]ProbeResult, 0, len(opts.Configs))
	for _, cfg := range opts.Configs {
		if cfg.Status != StatusOK || !cfg.Probeable {
			reason := cfg.SkipReason
			if reason == "" {
				reason = "configuration failed"
			}
			results = append(results, ProbeResult{Name: cfg.Name, Status: StatusSkipped, Message: reason})
			continue
		}
		command := probeCommand(cfg)
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		result := runner.Run(probeCtx, command)
		cancel()
		results = append(results, classifyProbeResult(cfg.Name, result, timeout))
	}
	return results
}

func probeCommand(cfg ConfigResult) ProbeCommand {
	switch cfg.Name {
	case "codex":
		return ProbeCommand{
			Name: "codex",
			Path: cfg.ExecutablePath,
			Args: []string{"--ask-for-approval", "never", "exec", "--ephemeral", "--sandbox", "read-only", probePrompt},
		}
	case "claude":
		return ProbeCommand{
			Name: "claude",
			Path: cfg.ExecutablePath,
			Args: []string{"-p", probePrompt, "--output-format", "text", "--no-session-persistence", "--tools", ""},
		}
	case "gemini":
		return ProbeCommand{
			Name: "gemini",
			Path: cfg.ExecutablePath,
			Args: []string{"--prompt", probePrompt, "--output-format", "text", "--skip-trust"},
			Env:  cfg.ProbeEnv,
		}
	default:
		return ProbeCommand{Name: cfg.Name, Path: cfg.ExecutablePath}
	}
}

func classifyProbeResult(name string, result CommandResult, timeout time.Duration) ProbeResult {
	out := strings.TrimSpace(result.Stdout)
	errText := strings.TrimSpace(result.Stderr)
	probe := ProbeResult{Name: name, Duration: result.Duration}
	if errors.Is(result.Err, context.DeadlineExceeded) || errors.Is(result.Err, context.Canceled) {
		probe.Status = StatusFailed
		probe.Message = fmt.Sprintf("timeout after %s", timeout)
		if errText != "" {
			probe.Message += ": " + truncate(errText, 240)
		}
		return probe
	}
	if result.Err != nil {
		probe.Status = StatusFailed
		probe.Message = truncate(firstNonEmpty(errText, result.Err.Error()), 240)
		return probe
	}
	if strings.TrimSpace(out) == "" {
		probe.Status = StatusFailed
		probe.Message = "empty output"
		return probe
	}
	probe.Status = StatusOK
	probe.Output = truncate(out, 120)
	return probe
}

func FormatProbeResult(result ProbeResult) string {
	parts := []string{fmt.Sprintf("%s:", result.Name), result.Status}
	if result.Duration > 0 {
		parts = append(parts, "duration="+result.Duration.Round(time.Millisecond).String())
	}
	if result.Output != "" {
		parts = append(parts, "output="+result.Output)
	}
	if result.Message != "" {
		parts = append(parts, result.Message)
	}
	return RedactSecrets(strings.Join(parts, " "))
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
```

- [x] **Step 4: Run probe tests**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck -run 'ProbeTools' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit probe slice**

```bash
git add ae-cli/internal/doctorcheck/probe.go ae-cli/internal/doctorcheck/probe_test.go
git commit -m "feat(ae-cli): probe configured local tools in doctor"
```

## Task 4: Wire Tool Validation into `ae-cli doctor`

**Files:**
- Modify: `ae-cli/cmd/doctor.go`
- Create: `ae-cli/cmd/doctor_tool_test.go`

- [x] **Step 1: Write failing command-level tests**

Create `ae-cli/cmd/doctor_tool_test.go`:

```go
package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/doctorcheck"
)

func TestDoctorPrintsToolConfigurationAndProbe(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)
	writeDoctorToolFiles(t, home)

	oldCfg := cfg
	oldClient := apiClient
	oldList := listProvidersForDoctor
	oldDetect := detectToolsForDoctor
	oldProbe := probeToolsForDoctor
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "tok"}}
	apiClient = client.New("https://ae.example.com", "tok")
	listProvidersForDoctor = func(context.Context) ([]client.ProviderInfo, string, error) {
		return []client.ProviderInfo{doctorProviderInfo()}, "user/providers", nil
	}
	detectToolsForDoctor = func([]string) ([]doctorcheck.ToolState, error) {
		return []doctorcheck.ToolState{
			{Name: "codex", ExecutablePath: "/bin/codex", Version: "codex 1", Probeable: true},
			{Name: "claude", ExecutablePath: "/bin/claude", Version: "claude 1", Probeable: true},
			{Name: "gemini", ExecutablePath: "/bin/gemini", Version: "gemini 1", Probeable: true},
		}, nil
	}
	probeToolsForDoctor = func(ctx context.Context, opts doctorcheck.ProbeOptions) []doctorcheck.ProbeResult {
		return []doctorcheck.ProbeResult{
			{Name: "codex", Status: doctorcheck.StatusOK, Duration: time.Millisecond, Output: "AE_DOCTOR_OK"},
			{Name: "claude", Status: doctorcheck.StatusOK, Duration: time.Millisecond, Output: "AE_DOCTOR_OK"},
			{Name: "gemini", Status: doctorcheck.StatusOK, Duration: time.Millisecond, Output: "AE_DOCTOR_OK"},
		}
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		listProvidersForDoctor = oldList
		detectToolsForDoctor = oldDetect
		probeToolsForDoctor = oldProbe
	})

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"Tool configuration",
		"provider: sub2api source=user/providers",
		"codex:",
		"credential=match",
		"Tool probe",
		"output=AE_DOCTOR_OK",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "sk-openai") || strings.Contains(output, "sk-anthropic") || strings.Contains(output, "sk-gemini") {
		t.Fatalf("doctor output leaked credential:\n%s", output)
	}
}

func doctorProviderInfo() client.ProviderInfo {
	return client.ProviderInfo{
		Name:      "sub2api",
		BaseURL:   "https://relay.example.com/v1",
		IsPrimary: true,
		Credentials: []client.ProviderCredentialInfo{
			{Platform: "openai", APIKey: "sk-openai", Status: "active"},
			{Platform: "anthropic", APIKey: "sk-anthropic", Status: "active"},
			{Platform: "gemini", APIKey: "sk-gemini", Status: "active"},
		},
	}
}

func writeDoctorToolFiles(t *testing.T, home string) {
	t.Helper()
	writeFileForDoctor(t, filepath.Join(home, ".codex", "config.toml"), `
model_provider = 'sub2api'
model = 'gpt-5.5'
review_model = 'gpt-5.4'
[model_providers.sub2api]
base_url = 'https://relay.example.com/v1'
wire_api = 'responses'
requires_openai_auth = true
`)
	writeFileForDoctor(t, filepath.Join(home, ".codex", "auth.json"), `{"OPENAI_API_KEY":"sk-openai"}`)
	writeFileForDoctor(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"ANTHROPIC_BASE_URL":"https://relay.example.com/v1","ANTHROPIC_AUTH_TOKEN":"sk-anthropic","CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":"1","CLAUDE_CODE_ATTRIBUTION_HEADER":"0"}}`)
	writeFileForDoctor(t, filepath.Join(home, ".ae-cli", "env.sh"), "# BEGIN AE-CLI MANAGED\nexport GEMINI_API_KEY=\"sk-gemini\"\nexport GOOGLE_GEMINI_BASE_URL=\"https://relay.example.com/v1\"\n# END AE-CLI MANAGED\n")
	writeFileForDoctor(t, filepath.Join(home, ".zshrc"), "[ -f \"$HOME/.ae-cli/env.sh\" ] && source \"$HOME/.ae-cli/env.sh\"\n")
}

func writeFileForDoctor(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
```

- [x] **Step 2: Run command test to verify it fails**

Run:

```bash
cd ae-cli && go test ./cmd -run 'DoctorPrintsToolConfigurationAndProbe' -count=1
```

Expected: FAIL with undefined identifiers such as `listProvidersForDoctor`, `detectToolsForDoctor`, and `probeToolsForDoctor`.

- [x] **Step 3: Modify `doctor.go` to call doctorcheck**

In `ae-cli/cmd/doctor.go`, add imports:

```go
	"os/exec"
	"path/filepath"

	"github.com/ai-efficiency/ae-cli/internal/doctorcheck"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
```

Add package-level hooks near `doctorCmd`:

```go
var doctorToolNames = []string{"codex", "claude", "gemini"}

var listProvidersForDoctor = func(ctx context.Context) ([]client.ProviderInfo, string, error) {
	if apiClient == nil {
		return nil, "", fmt.Errorf("API client is not configured")
	}
	providers, err := apiClient.ListProviders(ctx)
	return providers, "user/providers", err
}

var detectToolsForDoctor = detectDoctorTools

var probeToolsForDoctor = doctorcheck.ProbeTools
```

After `printSyncTaskStatus(out, task)` and before `printRepoEligibilityDiagnostic(out)`, insert:

```go
		printToolDiagnostics(out)
```

Add helpers:

```go
func printToolDiagnostics(out io.Writer) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(out, "Tool configuration\n  provider: unavailable (%v)\n", err)
		return
	}
	providers, providerSource, providerErr := listProvidersForDoctor(context.Background())
	providerAvailable := providerErr == nil && len(providers) > 0
	var selected toolconfig.Provider
	if providerAvailable {
		selected, err = toolconfig.SelectProvider(mapProviders(providers), "")
		if err != nil {
			providerAvailable = false
			providerErr = err
		}
	}
	tools, err := detectToolsForDoctor(doctorToolNames)
	if err != nil {
		fmt.Fprintf(out, "Tool configuration\n  provider: unavailable (%v)\n", err)
		return
	}
	report := doctorcheck.ValidateTools(doctorcheck.ValidateOptions{
		HomeDir:           homeDir,
		ShellPath:         os.Getenv("SHELL"),
		Provider:          selected,
		ProviderAvailable: providerAvailable,
		ProviderSource:    providerSource,
		Tools:             tools,
	})
	fmt.Fprintln(out, "Tool configuration")
	if providerAvailable {
		fmt.Fprintf(out, "  provider: %s source=%s\n", report.ProviderName, report.ProviderSource)
	} else {
		fmt.Fprintf(out, "  provider: unavailable (%v)\n", providerErr)
	}
	for i := range report.Results {
		fmt.Fprintf(out, "  %s\n", doctorcheck.FormatConfigResult(&report.Results[i]))
	}
	fmt.Fprintln(out, "Tool probe")
	probeResults := probeToolsForDoctor(context.Background(), doctorcheck.ProbeOptions{
		Timeout: time.Minute,
		Configs: report.Results,
	})
	for _, result := range probeResults {
		fmt.Fprintf(out, "  %s\n", doctorcheck.FormatProbeResult(result))
	}
}

func detectDoctorTools(toolNames []string) ([]doctorcheck.ToolState, error) {
	installed, err := toolconfig.DetectInstalledTools(toolNames)
	if err != nil {
		return nil, err
	}
	byName := map[string]toolconfig.InstalledTool{}
	for _, item := range installed {
		byName[item.Name] = item
	}
	out := make([]doctorcheck.ToolState, 0, len(toolNames))
	for _, name := range toolNames {
		item, ok := byName[name]
		if !ok {
			out = append(out, doctorcheck.ToolState{Name: name, Missing: true})
			continue
		}
		probeable := true
		if strings.HasSuffix(item.Path, ".app") {
			probeable = false
		}
		out = append(out, doctorcheck.ToolState{
			Name:           name,
			ExecutablePath: item.Path,
			Version:        doctorToolVersion(item.Path),
			Probeable:      probeable,
		})
	}
	return out, nil
}

func doctorToolVersion(path string) string {
	if strings.TrimSpace(path) == "" || strings.HasSuffix(path, ".app") {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func doctorToolPathName(path string) string {
	return filepath.Base(path)
}
```

- [x] **Step 4: Run command tool test**

Run:

```bash
cd ae-cli && go test ./cmd -run 'DoctorPrintsToolConfigurationAndProbe' -count=1
```

Expected: PASS.

- [x] **Step 5: Run existing doctor smoke test**

Run:

```bash
cd ae-cli && go test ./cmd -run 'DoctorCommandPrintsWorkspaceIdentity' -count=1
```

Expected: PASS.

- [x] **Step 6: Commit doctor wiring slice**

```bash
git add ae-cli/cmd/doctor.go ae-cli/cmd/doctor_tool_test.go
git commit -m "feat(ae-cli): include tool readiness in doctor"
```

## Task 5: Split Doctor Repo Eligibility Timeout

**Files:**
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/cmd/cutover_test.go`

- [x] **Step 1: Write failing timeout test**

Append this test to `ae-cli/cmd/cutover_test.go`:

```go
func TestDoctorRepoEligibilityUsesDoctorTimeout(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(75 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"eligible":true,"repo_config_id":456,"repo_key":"github.com/acme/repo","status":"active"}}`))
	}))
	defer srv.Close()

	oldCfg := cfg
	oldClient := apiClient
	oldHookTimeout := hookEligibilityResolveTimeout
	oldDoctorTimeout := doctorRepoEligibilityTimeout
	cfg = &config.Config{Server: config.ServerConfig{URL: srv.URL, Token: "tok"}}
	apiClient = client.New(srv.URL, "tok")
	hookEligibilityResolveTimeout = 10 * time.Millisecond
	doctorRepoEligibilityTimeout = 500 * time.Millisecond
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		hookEligibilityResolveTimeout = oldHookTimeout
		doctorRepoEligibilityTimeout = oldDoctorTimeout
	})

	var buf bytes.Buffer
	printRepoEligibilityDiagnostic(&buf)
	output := buf.String()
	if !strings.Contains(output, "Repo Eligibility: eligible") || !strings.Contains(output, "duration=") {
		t.Fatalf("output = %q, want eligible with duration", output)
	}
}
```

- [x] **Step 2: Run timeout test to verify it fails**

Run:

```bash
cd ae-cli && go test ./cmd -run 'DoctorRepoEligibilityUsesDoctorTimeout' -count=1
```

Expected: FAIL with undefined `doctorRepoEligibilityTimeout`, or with timeout because `printRepoEligibilityDiagnostic` still uses `hookEligibilityResolveTimeout`.

- [x] **Step 3: Implement doctor-specific timeout and duration output**

In `ae-cli/cmd/doctor.go`, add:

```go
var doctorRepoEligibilityTimeout = 10 * time.Second
```

In `printRepoEligibilityDiagnostic`, replace the timeout and add timing:

```go
	started := time.Now()
	resolveCtx, cancel := context.WithTimeout(context.Background(), doctorRepoEligibilityTimeout)
	defer cancel()
	resp, err := apiClient.ResolveRepoFromRemote(resolveCtx, client.ResolveRepoRequest{
		RemoteURL:          gitCtx.RemoteURL,
		Branch:             gitCtx.Branch,
		ClientCacheVersion: client.RepoEligibilityVersion,
	})
	duration := time.Since(started).Round(time.Millisecond)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(out, "Repo Eligibility: unavailable (timeout after %s)\n", doctorRepoEligibilityTimeout)
			return
		}
		fmt.Fprintf(out, "Repo Eligibility: unavailable (%v, duration=%s)\n", err, duration)
		return
	}
	if resp != nil && resp.Eligible && resp.RepoConfigID > 0 {
		fmt.Fprintf(out, "Repo Eligibility: eligible (repo_config_id=%d, duration=%s)\n", resp.RepoConfigID, duration)
		return
	}
```

Also add `errors` to the imports.

- [x] **Step 4: Run timeout test**

Run:

```bash
cd ae-cli && go test ./cmd -run 'DoctorRepoEligibilityUsesDoctorTimeout|DoctorCommandPrintsWorkspaceIdentity' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit timeout slice**

```bash
git add ae-cli/cmd/doctor.go ae-cli/cmd/cutover_test.go
git commit -m "fix(ae-cli): use diagnostic timeout for doctor eligibility"
```

## Task 6: Full Verification

**Files:**
- Modify only if previous tasks expose test compile issues.

- [x] **Step 1: Run all ae-cli tests**

Run:

```bash
cd ae-cli && go test ./...
```

Expected: PASS.

- [x] **Step 2: Run doctor manually against the current repo**

Run:

```bash
cd /Users/admin/ai-efficiency
cd ae-cli && go run . doctor
```

Expected:

- Output includes `Tool configuration`.
- Output includes `Tool probe`.
- Output includes Codex, Claude, and Gemini lines.
- `Repo Eligibility` does not fail with `context deadline exceeded` when the backend responds within 10 seconds.
- No API key, token, or full secret value appears in output.

- [x] **Step 3: Commit any verification-only follow-up fixes**

If Step 1 or Step 2 required a small correction, commit it:

```bash
git add ae-cli/cmd ae-cli/internal/doctorcheck
git commit -m "fix(ae-cli): polish doctor tool diagnostics"
```

If no fixes were needed, do not create an empty commit.

- [x] **Step 4: Capture final status**

Run:

```bash
git status --short
git log --oneline -6
```

Expected:

- No unstaged implementation changes remain.
- The recent commits include the validation, probe, doctor wiring, and eligibility timeout slices.

## Self-Review Notes

- Spec coverage: Tasks 1-4 cover tool detection, provider contract validation, config validation, local command probing, redaction, and no `--live-tools`; Task 5 covers doctor-specific repo eligibility timeout and duration output; Task 6 covers full verification.
- The plan keeps `/api/v1/user/providers/:id/test` out of doctor and uses only local `codex`, `claude`, and `gemini` commands for probes.
- The plan intentionally keeps doctor as a diagnostic command that prints failures but returns non-zero only for existing unrecoverable doctor setup errors.

## Follow-up 2026-06-01: Visible Progress and Colored Status

**Status:** Full ae-cli tests pass; local install refresh still pending.

**Files:**
- Modify: `ae-cli/internal/doctorcheck/probe.go`
- Modify: `ae-cli/internal/doctorcheck/probe_test.go`
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/cmd/doctor_tool_test.go`

- [x] **Step 1: Write failing tests for probe progress callbacks and colored doctor output**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck -run 'ProbeToolsReportsStartAndResultCallbacksInOrder' -count=1
cd ae-cli && go test ./cmd -run 'DoctorPrintsProbeProgressAndColorWhenForced' -count=1
```

Expected: FAIL before implementation because `ProbeOptions` does not yet expose progress callbacks and doctor output does not print running/color status.

- [x] **Step 2: Implement progress callbacks and doctor status styling**

Update `ProbeOptions` with start/result callbacks, call them around each local CLI probe, and make `doctor` print `running timeout=<duration>` before each long-running command. Add ANSI color only when stdout is a terminal or `CLICOLOR_FORCE` is set; honor `NO_COLOR`.

- [x] **Step 3: Run focused tests**

Run:

```bash
cd ae-cli && go test ./internal/doctorcheck ./cmd -run 'ProbeToolsReportsStartAndResultCallbacksInOrder|DoctorPrintsProbeProgressAndColorWhenForced' -count=1
```

Expected: PASS.

- [x] **Step 4: Run full ae-cli tests**

Run:

```bash
cd ae-cli && go test ./...
```

Expected: PASS.

- [ ] **Step 5: Rebuild local install and verify actual doctor output**

Run:

```bash
cd ae-cli
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/ai-efficiency/ae-cli/internal/buildinfo.Version=v0.1.0-dev.<short-sha>" -o ~/.local/bin/ae-cli .
~/.local/bin/ae-cli version
~/.local/bin/ae-cli doctor
```

Expected: installed binary prints the local dev version, `doctor` prints visible `running` lines during tool probes, tool probe results return, and no secrets appear in output.
