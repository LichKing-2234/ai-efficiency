package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of ae-cli",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "ae-cli "+buildinfo.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
