package cmd

import "github.com/spf13/cobra"

var flushCmd = &cobra.Command{
	Use:    "flush",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

func init() {
	rootCmd.AddCommand(flushCmd)
}
