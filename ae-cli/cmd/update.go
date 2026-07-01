package cmd

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
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

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func init() {
	updateInstallCmd.Flags().BoolVar(&updateInstallForce, "force", false, "reinstall the latest published release even when the current version is already up to date")
	updateUpgradeCmd.Flags().BoolVar(&updateInstallForce, "force", false, "reinstall the latest published release even when the current version is already up to date")
	updateCmd.AddCommand(updateCheckCmd, updateInstallCmd, updateUpgradeCmd)
	rootCmd.AddCommand(updateCmd)
}
