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
	apiClient = nil
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
		if opts.Timeout != time.Minute {
			t.Fatalf("probe timeout = %s, want 1m", opts.Timeout)
		}
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
