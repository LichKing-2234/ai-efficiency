package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

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
