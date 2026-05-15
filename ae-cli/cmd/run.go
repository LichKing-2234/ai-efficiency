package cmd

import "github.com/spf13/cobra"

var runCmd = &cobra.Command{
	Use:    "run <tool> [args...]",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
