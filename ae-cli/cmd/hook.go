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

var newHookUploader = func() hooks.Uploader {
	return hooks.UnsupportedUploader{}
}

var hookPostCommitCmd = &cobra.Command{
	Use:    "post-commit",
	Short:  "Handle git post-commit hook (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		h := hooks.NewHandler(newHookUploader())
		// Fail-open: handler itself should never return errors that block commits.
		return h.PostCommit(context.Background(), cwd)
	},
}

var hookPostRewriteCmd = &cobra.Command{
	Use:    "post-rewrite <rewrite_type>",
	Short:  "Handle git post-rewrite hook (hidden)",
	Hidden: true,
	Args:   cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		h := hooks.NewHandler(newHookUploader())
		// Fail-open: handler itself should never return errors that block git workflows.
		return h.PostRewrite(context.Background(), cwd, args[0], os.Stdin)
	},
}

func init() {
	hookCmd.AddCommand(hookPostCommitCmd)
	hookCmd.AddCommand(hookPostRewriteCmd)
	rootCmd.AddCommand(hookCmd)
}
