package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	updatepkg "github.com/ai-efficiency/ae-cli/internal/update"
	"github.com/spf13/cobra"
)

func TestUpdateCommandIsRegistered(t *testing.T) {
	var update *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "update" {
			update = cmd
			break
		}
	}
	if update == nil {
		t.Fatal("expected update command to be registered")
	}

	expected := map[string]bool{
		"check":   false,
		"install": false,
		"upgrade": false,
	}
	for _, cmd := range update.Commands() {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Fatalf("expected update subcommand %q to be registered", name)
		}
	}
}

func TestPersistentPreRunESkipsForUpdateSubcommands(t *testing.T) {
	oldCfgFile := cfgFile
	defer func() { cfgFile = oldCfgFile }()

	cfgFile = "/nonexistent/path/config.yaml"
	if err := rootCmd.PersistentPreRunE(updateCheckCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE for update check should not error: %v", err)
	}
}

func TestUpdateCheckCommandPrintsAvailableMessage(t *testing.T) {
	oldCheck := checkForUpdate
	defer func() { checkForUpdate = oldCheck }()

	checkForUpdate = func(_ context.Context, _ updatepkg.CheckOptions) (updatepkg.CheckResult, error) {
		return updatepkg.CheckResult{
			CurrentVersion:  "v0.1.0",
			LatestTag:       "v0.2.0",
			UpdateAvailable: true,
			Status:          "update_available",
		}, nil
	}

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"update", "check"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute update check: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "Update available: v0.1.0 -> v0.2.0") {
		t.Fatalf("output = %q, want update-available message", got)
	}
}

func TestUpdateInstallCommandPrintsUpgradeMessage(t *testing.T) {
	oldInstall := installUpdate
	defer func() { installUpdate = oldInstall }()

	installUpdate = func(_ context.Context, _ updatepkg.InstallOptions) (updatepkg.InstallResult, error) {
		return updatepkg.InstallResult{
			PreviousVersion:  "v0.1.0",
			InstalledVersion: "v0.2.0",
			Updated:          true,
			Status:           "updated",
		}, nil
	}

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"update", "install"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute update install: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "Upgraded ae-cli v0.1.0 -> v0.2.0") {
		t.Fatalf("output = %q, want upgrade message", got)
	}
}

func TestUpdateInstallRemovesOnlyManagedCodexOTLP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	endpoint := "https://ae.example.com/api/v1/attribution/otel/v1/traces"
	if _, err := toolconfig.ConfigureCodexOTLP(home, endpoint, "test-otlp-token"); err != nil {
		t.Fatal(err)
	}
	reportingPath := filepath.Join(home, ".ae-cli", "reporting.json")
	if err := reporting.Save(reportingPath, &reporting.Config{
		InstallationID: "11111111-1111-4111-8111-111111111111",
		ServerURL:      "https://ae.example.com",
		OTLPToken:      "test-otlp-token",
	}); err != nil {
		t.Fatal(err)
	}

	oldInstall := installUpdate
	defer func() { installUpdate = oldInstall }()
	installUpdate = func(_ context.Context, _ updatepkg.InstallOptions) (updatepkg.InstallResult, error) {
		return updatepkg.InstallResult{PreviousVersion: "v0.1.0", InstalledVersion: "v0.2.0", Updated: true, Status: "updated"}, nil
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runUpdateInstall(cmd); err != nil {
		t.Fatal(err)
	}
	inspection, err := toolconfig.InspectCodexOTLP(home, endpoint, "test-otlp-token")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Configured {
		t.Fatalf("managed Codex OTLP survived successful update: %+v", inspection)
	}
	config, err := reporting.Load(reportingPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.OTLPToken != "" || config.OTelEnabled {
		t.Fatalf("legacy local OTLP state survived successful update: token_present=%t enabled=%t", config.OTLPToken != "", config.OTelEnabled)
	}
}
