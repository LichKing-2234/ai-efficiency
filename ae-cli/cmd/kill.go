package cmd

import "github.com/spf13/cobra"

var killCmd = &cobra.Command{
	Use:    "kill <pane-id>",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

func init() {
	rootCmd.AddCommand(killCmd)
}
