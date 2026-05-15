package cmd

import "github.com/spf13/cobra"

var shellCmd = &cobra.Command{
	Use:    "shell",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
