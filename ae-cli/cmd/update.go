package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	updatepkg "github.com/ai-efficiency/ae-cli/internal/update"
	"github.com/spf13/cobra"
)

var (
	updateInstallForce bool
	checkForUpdate     = updatepkg.CheckForUpdate
	installUpdate      = updatepkg.InstallLatest
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install ae-cli updates",
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check whether a newer ae-cli release is available",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := checkForUpdate(commandContext(cmd), updatepkg.CheckOptions{
			CurrentVersion: buildinfo.Version,
		})
		if err != nil {
			return err
		}
		switch result.Status {
		case "update_available":
			fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s -> %s\n", result.CurrentVersion, result.LatestTag)
		case "current_newer":
			fmt.Fprintf(cmd.OutOrStdout(), "Current version %s is newer than latest published stable %s\n", result.CurrentVersion, result.LatestTag)
		default:
			fmt.Fprintf(cmd.OutOrStdout(), "ae-cli is up to date (%s)\n", result.CurrentVersion)
		}
		return nil
	},
}

var updateInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the latest published ae-cli release",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUpdateInstall(cmd)
	},
}

var updateUpgradeCmd = &cobra.Command{
	Use:    "upgrade",
	Short:  "Alias for update install",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUpdateInstall(cmd)
	},
}

var updatePostInstallCmd = &cobra.Command{
	Use:    "post-install",
	Short:  "Run compatibility cleanup after installing a release",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// An upgrade is the only moment that reaches a developer who logged in
		// before Pilot became this CLI's usage source. It reports rather than
		// installs: running another project's installer is not something an
		// upgrade should do behind someone's back.
		notePilotStatus(cmd.OutOrStdout(), cmd.ErrOrStderr())
		return cleanupLegacyCodexOTLPAfterUpdate()
	},
}

func runUpdateInstall(cmd *cobra.Command) error {
	result, err := installUpdate(commandContext(cmd), updatepkg.InstallOptions{
		CurrentVersion: buildinfo.Version,
		Force:          updateInstallForce,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	switch result.Status {
	case "updated":
		fmt.Fprintf(cmd.OutOrStdout(), "Upgraded ae-cli %s -> %s\n", result.PreviousVersion, result.InstalledVersion)
	case "current_newer":
		fmt.Fprintf(cmd.OutOrStdout(), "No update installed: current version %s is newer than latest published stable %s\n", result.PreviousVersion, result.InstalledVersion)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "ae-cli is up to date (%s)\n", result.PreviousVersion)
	}
	return nil
}

func cleanupLegacyCodexOTLPAfterUpdate() error {
	config, err := reporting.Load("")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load reporting config: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home: %w", err)
	}
	endpoint := codexOTLPEndpoint(config.ServerURL)
	_, changed, err := toolconfig.DisableCodexOTLP(home, endpoint, config.OTLPToken)
	if err != nil {
		return fmt.Errorf("remove managed Codex OTLP: %w", err)
	}
	if !changed {
		inspection, err := toolconfig.InspectCodexOTLP(home, endpoint, config.OTLPToken)
		if err != nil {
			return fmt.Errorf("inspect retained Codex OTLP: %w", err)
		}
		if inspection.ExporterPresent {
			return fmt.Errorf("preserved user-modified Codex OTLP exporter because it does not exactly match AE ownership evidence")
		}
	}
	config.OTLPToken = ""
	config.OTelEnabled = false
	path, err := reporting.DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve reporting config path: %w", err)
	}
	if err := reporting.Save(path, config); err != nil {
		return fmt.Errorf("save cleared legacy OTLP state: %w", err)
	}
	return nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func init() {
	updateInstallCmd.Flags().BoolVar(&updateInstallForce, "force", false, "reinstall the latest published release even when the current version is already up to date")
	updateUpgradeCmd.Flags().BoolVar(&updateInstallForce, "force", false, "reinstall the latest published release even when the current version is already up to date")
	updateCmd.AddCommand(updateCheckCmd, updateInstallCmd, updateUpgradeCmd, updatePostInstallCmd)
	rootCmd.AddCommand(updateCmd)
}
