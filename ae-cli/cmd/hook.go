package cmd

import (
	"context"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Internal git hook entrypoint (hidden)",
	Hidden: true,
}

var hookPostCommitCmd = &cobra.Command{
	Use:    "post-commit",
	Short:  "Handle git post-commit hook (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		h := hooks.NewHandler(hooks.UnsupportedUploader{})
		// Fail-open: handler itself should never return errors that block commits.
		return h.PostCommit(context.Background(), cwd)
	},
}

func init() {
	hookCmd.AddCommand(hookPostCommitCmd)
	rootCmd.AddCommand(hookCmd)
}
