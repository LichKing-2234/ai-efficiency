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
	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/doctorcheck"
)

func TestDoctorSkipsToolProbeByDefault(t *testing.T) {
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
	oldProbeTools := doctorProbeTools
	doctorProbeTools = false
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
		t.Fatal("probeToolsForDoctor should not run unless --probe-tools is set")
		return nil
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		listProvidersForDoctor = oldList
		detectToolsForDoctor = oldDetect
		probeToolsForDoctor = oldProbe
		doctorProbeTools = oldProbeTools
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
		"[warn] codex: warn",
		"credential=match",
		"Tool probe: skipped",
		"use --probe-tools to run local CLI probes",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "AE_DOCTOR_OK") {
		t.Fatalf("doctor output shows probe result even though probe was skipped:\n%s", output)
	}
	if strings.Contains(output, "sk-openai") || strings.Contains(output, "sk-anthropic") || strings.Contains(output, "sk-gemini") {
		t.Fatalf("doctor output leaked credential:\n%s", output)
	}
}

func TestDoctorPrintsProbeProgressWhenEnabled(t *testing.T) {
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
	oldProbeTools := doctorProbeTools
	doctorProbeTools = true
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "tok"}}
	apiClient = nil
	listProvidersForDoctor = func(context.Context) ([]client.ProviderInfo, string, error) {
		return []client.ProviderInfo{doctorProviderInfo()}, "user/providers", nil
	}
	detectToolsForDoctor = func([]string) ([]doctorcheck.ToolState, error) {
		return []doctorcheck.ToolState{
			{Name: "codex", ExecutablePath: "/bin/codex", Version: "codex 1", Probeable: true},
		}, nil
	}
	probeToolsForDoctor = func(ctx context.Context, opts doctorcheck.ProbeOptions) []doctorcheck.ProbeResult {
		result := doctorcheck.ProbeResult{Name: "codex", Status: doctorcheck.StatusOK, Duration: time.Millisecond, Output: "AE_DOCTOR_OK"}
		if opts.OnStart != nil {
			opts.OnStart(opts.Configs[0], opts.Timeout)
		}
		if opts.OnResult != nil {
			opts.OnResult(result)
		}
		return []doctorcheck.ProbeResult{result}
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		listProvidersForDoctor = oldList
		detectToolsForDoctor = oldDetect
		probeToolsForDoctor = oldProbe
		doctorProbeTools = oldProbeTools
	})

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	output := buf.String()
	running := "codex: running timeout=1m0s"
	done := "codex: ok duration=1ms output=AE_DOCTOR_OK"
	if !strings.Contains(output, running) {
		t.Fatalf("doctor output missing running status %q:\n%s", running, output)
	}
	if !strings.Contains(output, done) {
		t.Fatalf("doctor output missing result status %q:\n%s", done, output)
	}
	if strings.Index(output, running) > strings.Index(output, done) {
		t.Fatalf("running status printed after result:\n%s", output)
	}
	if strings.Contains(output, "Tool probe: skipped") {
		t.Fatalf("doctor output skipped probe even though probe was enabled:\n%s", output)
	}
}

func TestDoctorPrintsStatusColorsWhenForced(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	withWorkingDir(t, repo)
	writeDoctorToolFiles(t, home)

	oldCfg := cfg
	oldClient := apiClient
	oldList := listProvidersForDoctor
	oldDetect := detectToolsForDoctor
	oldProbe := probeToolsForDoctor
	oldProbeTools := doctorProbeTools
	doctorProbeTools = false
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "tok"}}
	apiClient = nil
	listProvidersForDoctor = func(context.Context) ([]client.ProviderInfo, string, error) {
		return []client.ProviderInfo{doctorProviderInfo()}, "user/providers", nil
	}
	detectToolsForDoctor = func([]string) ([]doctorcheck.ToolState, error) {
		return []doctorcheck.ToolState{
			{Name: "codex", ExecutablePath: "/bin/codex", Version: "codex 1", Probeable: true},
		}, nil
	}
	probeToolsForDoctor = func(ctx context.Context, opts doctorcheck.ProbeOptions) []doctorcheck.ProbeResult {
		t.Fatal("probeToolsForDoctor should not run unless --probe-tools is set")
		return nil
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		listProvidersForDoctor = oldList
		detectToolsForDoctor = oldDetect
		probeToolsForDoctor = oldProbe
		doctorProbeTools = oldProbeTools
	})

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("doctor output missing ANSI color codes:\n%s", output)
	}
	for _, want := range []string{
		"Logged In:     true \x1b[32m[ok]\x1b[0m",
		"State Exists:  false \x1b[33m[warn]\x1b[0m",
		"Tool probe: skipped \x1b[33m[warn]\x1b[0m",
		"Repo Eligibility: skipped \x1b[33m[warn]\x1b[0m",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing colored status %q:\n%s", want, output)
		}
	}
}

func TestDoctorRegistersProbeToolsFlag(t *testing.T) {
	flag := doctorCmd.Flags().Lookup("probe-tools")
	if flag == nil {
		t.Fatal("doctor --probe-tools flag is not registered")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--probe-tools default = %q, want false", flag.DefValue)
	}
}

func TestDoctorRecentFailuresFlagCanHideSection(t *testing.T) {
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
	oldProbeTools := doctorProbeTools
	oldRecent := recentCodexFailureSummary
	oldRecentLimit := doctorRecentFailures
	recentFlag := doctorCmd.Flags().Lookup("recent-failures")
	if recentFlag == nil {
		t.Fatal("doctor --recent-failures flag is not registered")
	}
	oldRecentFlag := recentFlag.Value.String()

	doctorProbeTools = false
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "tok"}}
	apiClient = nil
	listProvidersForDoctor = func(context.Context) ([]client.ProviderInfo, string, error) {
		return []client.ProviderInfo{doctorProviderInfo()}, "user/providers", nil
	}
	detectToolsForDoctor = func([]string) ([]doctorcheck.ToolState, error) {
		return []doctorcheck.ToolState{{Name: "codex", ExecutablePath: "/bin/codex", Version: "codex 1", Probeable: true}}, nil
	}
	probeToolsForDoctor = func(context.Context, doctorcheck.ProbeOptions) []doctorcheck.ProbeResult {
		t.Fatal("probeToolsForDoctor should not run unless --probe-tools is set")
		return nil
	}
	recentCodexFailureSummary = func(string, int) (attributionlocal.CodexFailureSummary, error) {
		t.Fatal("recentCodexFailureSummary should not run when --recent-failures=0")
		return attributionlocal.CodexFailureSummary{}, nil
	}
	if err := doctorCmd.Flags().Set("recent-failures", "0"); err != nil {
		t.Fatalf("set recent-failures: %v", err)
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		listProvidersForDoctor = oldList
		detectToolsForDoctor = oldDetect
		probeToolsForDoctor = oldProbe
		doctorProbeTools = oldProbeTools
		recentCodexFailureSummary = oldRecent
		doctorRecentFailures = oldRecentLimit
		_ = doctorCmd.Flags().Set("recent-failures", oldRecentFlag)
	})

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if strings.Contains(buf.String(), "Recent Codex Failures") {
		t.Fatalf("doctor output should hide recent Codex failures when flag is 0:\n%s", buf.String())
	}
}

func TestDoctorPrintsRecentCodexFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldRecent := recentCodexFailureSummary
	recentCodexFailureSummary = func(gotHome string, limit int) (attributionlocal.CodexFailureSummary, error) {
		if gotHome != home {
			t.Fatalf("home = %q, want %q", gotHome, home)
		}
		if limit != 3 {
			t.Fatalf("limit = %d, want 3", limit)
		}
		return attributionlocal.CodexFailureSummary{
			Recent: []attributionlocal.CodexFailedRequest{
				{
					Timestamp:  time.Date(2026, 6, 18, 9, 31, 0, 0, time.UTC),
					StatusCode: 502,
					StatusText: "Bad Gateway",
					URL:        "http://127.0.0.1:15721/v1/responses",
				},
			},
			RecentWithRequestID: []attributionlocal.CodexFailedRequest{
				{
					Timestamp:        time.Date(2026, 6, 18, 9, 30, 0, 0, time.UTC),
					StatusCode:       503,
					StatusText:       "Service Unavailable",
					URL:              "https://relay.example.com/responses",
					XRequestID:       "req-503",
					XClientRequestID: "client-503",
					XKongRequestID:   "kong-503",
				},
			},
		}, nil
	}
	t.Cleanup(func() {
		recentCodexFailureSummary = oldRecent
	})

	buf := &bytes.Buffer{}
	printRecentFailures(buf, 3)

	output := buf.String()
	for _, want := range []string{
		"Recent Codex Failures: 1 [warn] (most recent Codex request errors)",
		"status=502 Bad Gateway",
		"url=http://127.0.0.1:15721/v1/responses",
		"x-request-id=(none)",
		"Recent Codex Failures With Request IDs: 1 [warn] (most recent Codex request errors with upstream IDs)",
		"status=503 Service Unavailable",
		"url=https://relay.example.com/responses",
		"x-request-id=req-503",
		"x-client-request-id=client-503",
		"x-kong-request-id=kong-503",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor recent failure output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Recent Failures:") {
		t.Fatalf("doctor output should be explicitly Codex-scoped:\n%s", output)
	}
}

func TestDoctorDoesNotPrintRequestIDFallbackWhenRecentFailuresHaveIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldRecent := recentCodexFailureSummary
	recentCodexFailureSummary = func(string, int) (attributionlocal.CodexFailureSummary, error) {
		failure := attributionlocal.CodexFailedRequest{
			Timestamp:        time.Date(2026, 6, 18, 9, 30, 0, 0, time.UTC),
			StatusCode:       503,
			StatusText:       "Service Unavailable",
			URL:              "https://relay.example.com/responses",
			XRequestID:       "req-503",
			XClientRequestID: "client-503",
			XKongRequestID:   "kong-503",
		}
		return attributionlocal.CodexFailureSummary{
			Recent:              []attributionlocal.CodexFailedRequest{failure},
			RecentWithRequestID: []attributionlocal.CodexFailedRequest{failure},
		}, nil
	}
	t.Cleanup(func() {
		recentCodexFailureSummary = oldRecent
	})

	buf := &bytes.Buffer{}
	printRecentFailures(buf, 3)

	output := buf.String()
	for _, want := range []string{
		"Recent Codex Failures: 1 [warn] (most recent Codex request errors)",
		"status=503 Service Unavailable",
		"url=https://relay.example.com/responses",
		"x-request-id=req-503",
		"x-client-request-id=client-503",
		"x-kong-request-id=kong-503",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor recent failure output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Recent Codex Failures With Request IDs") {
		t.Fatalf("doctor output should not duplicate request-id fallback when recent failures already have IDs:\n%s", output)
	}
}

func TestFailureURLRedactsSensitiveParts(t *testing.T) {
	got := failureURL("https://alice:test-password@relay.example.com/responses?api_key=test-password&ok=1#fragment")
	if got != "https://relay.example.com/responses" {
		t.Fatalf("failureURL redacted URL = %q", got)
	}
	got = failureURL("not-a-url?token=secret")
	if got != "not-a-url" {
		t.Fatalf("failureURL fallback = %q", got)
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
supports_websockets = false
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
