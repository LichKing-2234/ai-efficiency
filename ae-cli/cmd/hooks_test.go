package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	hookspkg "github.com/ai-efficiency/ae-cli/internal/hooks"
)

func TestHooksCommandIsRegistered(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "hooks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected hooks subcommand to be registered")
	}
}

func TestHooksEnableRequiresExactlyOneScope(t *testing.T) {
	resetHooksEnableFlagsForTest()
	if err := hooksEnableCmd.RunE(hooksEnableCmd, nil); err == nil {
		t.Fatalf("expected missing scope error")
	}
	hooksEnableGlobal = true
	hooksEnableRepo = true
	if err := hooksEnableCmd.RunE(hooksEnableCmd, nil); err == nil {
		t.Fatalf("expected multiple scope error")
	}
}

func TestHooksEnableRequiresUsableToken(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)

	oldCfg := cfg
	oldClient := apiClient
	cfg = &config.Config{}
	apiClient = client.New("http://example.invalid", "")
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})

	resetHooksEnableFlagsForTest()
	hooksEnableRepo = true
	if err := hooksEnableCmd.RunE(hooksEnableCmd, nil); err == nil || !strings.Contains(err.Error(), "ae-cli login") {
		t.Fatalf("err = %v, want login guidance", err)
	}
}

func TestHooksStatusPrintsHookStatus(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	t.Setenv("HOME", t.TempDir())
	withWorkingDir(t, repo)

	var buf bytes.Buffer
	hooksStatusCmd.SetOut(&buf)
	if err := hooksStatusCmd.RunE(hooksStatusCmd, nil); err != nil {
		t.Fatalf("hooks status: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Hook status", "Global:", "Repo-local:", "Effective:", "Template:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestPrintHookStatusIncludesUploadSummary(t *testing.T) {
	uploadedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	status := &hookspkg.Status{
		EffectiveMode: hookspkg.HookModeAEGlobal,
		UploadGroups: []hookspkg.UploadGroup{{
			ServerURL:            "https://ae.example.com",
			AuthSubject:          "user:1",
			RepoConfigID:         123,
			RepoKey:              "repo-host.example.com/org/repo",
			WorkspaceID:          "workspace-1",
			PendingCount:         2,
			UploadedCount:        3,
			FailedCount:          1,
			SkippedCount:         4,
			LastSuccessfulUpload: &uploadedAt,
			LastError:            "backend unavailable",
		}},
	}
	var buf bytes.Buffer
	printHookStatus(&buf, status)
	output := buf.String()
	for _, want := range []string{"Uploads:", "repo_config_id=123", "pending=2", "uploaded=3", "failed=1", "skipped=4", "last_success=2026-05-24T12:00:00Z", "last_error=backend unavailable"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestPrintHookStatusIncludesSpecDiagnostics(t *testing.T) {
	status := &hookspkg.Status{
		EffectiveMode:           hookspkg.HookModeGitDefault,
		EffectiveScope:          hookspkg.ConfigScopeLocal,
		CurrentTemplateVersion:  2,
		TemplateVersion:         1,
		TemplateStale:           true,
		ContextFingerprint:      "abc123",
		ObservedRepo:            "bound",
		DefaultExecutableHooks:  []string{"post-commit"},
		DefaultHooksDisposition: "effective",
	}
	var buf bytes.Buffer
	printHookStatus(&buf, status)
	output := buf.String()
	for _, want := range []string{
		"Scope:         local",
		"Template:      stale (installed=1 current=2)",
		"Context:       abc123",
		"Observed Repo: bound",
		"Default Hooks: effective (post-commit)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want %q", output, want)
		}
	}
}

func TestHooksDisableRepoPrintsSharedLocalScopeWarning(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	t.Setenv("HOME", t.TempDir())
	withWorkingDir(t, repo)
	if err := hookspkg.EnableRepo(hookspkg.InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	hooksDisableGlobal = false
	hooksDisableRepo = true
	var buf bytes.Buffer
	hooksDisableCmd.SetOut(&buf)
	t.Cleanup(func() {
		hooksDisableCmd.SetOut(nil)
		hooksDisableGlobal = false
		hooksDisableRepo = false
	})

	if err := hooksDisableCmd.RunE(hooksDisableCmd, nil); err != nil {
		t.Fatalf("hooks disable --repo: %v", err)
	}
	if !strings.Contains(buf.String(), "shared by linked worktrees") {
		t.Fatalf("output = %q, want shared linked-worktree warning", buf.String())
	}
}

func TestHooksRefreshRequiresStableAuthSubject(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatalf("DefaultTokenPath: %v", err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "opaque-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
		ServerURL:    "https://ae.example.com",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	oldCfg := cfg
	oldClient := apiClient
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "opaque-token"}}
	apiClient = client.New("https://ae.example.com", "opaque-token")
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})

	hooksRefreshCurrent = false
	if err := hooksRefreshCmd.RunE(hooksRefreshCmd, nil); err == nil || !strings.Contains(err.Error(), "auth_subject") {
		t.Fatalf("err = %v, want auth_subject guidance", err)
	}
}

func TestHooksRefreshInstallationsCommandIsHidden(t *testing.T) {
	var found bool
	for _, c := range hooksCmd.Commands() {
		if c.Name() == "refresh-installations" {
			found = true
			if !c.Hidden {
				t.Fatalf("refresh-installations should be hidden")
			}
		}
	}
	if !found {
		t.Fatal("expected hooks refresh-installations command")
	}
}

func resetHooksEnableFlagsForTest() {
	hooksEnableGlobal = false
	hooksEnableRepo = false
	hooksEnableForce = false
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}
