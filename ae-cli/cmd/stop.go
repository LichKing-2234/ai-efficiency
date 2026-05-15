package cmd

import "github.com/spf13/cobra"

var stopCmd = &cobra.Command{
	Use:    "stop",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
