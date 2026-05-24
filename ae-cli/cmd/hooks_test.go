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
