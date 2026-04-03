package cmd

import (
	"context"
	"os"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/proxy"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Internal git hook entrypoint (hidden)",
	Hidden: true,
}

var newHookUploader = func() hooks.Uploader {
	if apiClient == nil {
		return hooks.UnsupportedUploader{}
	}
	return hooks.NewBackendUploader(apiClient)
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

var hookSessionEventCmd = &cobra.Command{
	Use:    "session-event",
	Short:  "Forward tool hook events to the local proxy (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		tool, err := cmd.Flags().GetString("tool")
		if err != nil {
			return err
		}
		err = proxy.ForwardHookEvent(context.Background(), os.Stdin, proxy.HookForwardRequest{
			Tool:            tool,
			LocalProxyURL:   os.Getenv("AE_LOCAL_PROXY_URL"),
			LocalProxyToken: os.Getenv("AE_LOCAL_PROXY_TOKEN"),
			SessionID:       os.Getenv("AE_SESSION_ID"),
			WorkspaceID:     os.Getenv("AE_WORKSPACE_ID"),
			CapturedAt:      time.Now().UTC(),
		})
		if err != nil {
			return nil
		}
		return nil
	},
}

func init() {
	hookCmd.AddCommand(hookPostCommitCmd)
	hookCmd.AddCommand(hookPostRewriteCmd)
	hookSessionEventCmd.Flags().String("tool", "", "originating tool name")
	hookCmd.AddCommand(hookSessionEventCmd)
	rootCmd.AddCommand(hookCmd)
}
