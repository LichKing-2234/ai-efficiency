package cmd

import (
	"fmt"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var installSharedHooks = hooks.InstallSharedHooks

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize sessionless attribution for the current repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(ctx.attributionRoot, 0o700); err != nil {
			return fmt.Errorf("create attribution state dir: %w", err)
		}
		if err := installSharedHooks(ctx.repoRoot, bestEffortSelfPath()); err != nil {
			return fmt.Errorf("install shared hooks: %w", err)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Initialized sessionless attribution.\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
