package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/repolink"
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
		repoLinkStatus := "skipped"
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		if resolveToken(configToken, "") != "" {
			status, linkErr := repolink.Ensure(context.Background(), apiClient, gitRemoteURLForCutover(), gitBranchForCutover())
			repoLinkStatus = status
			if linkErr != nil {
				repoLinkStatus = "failed"
			}
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Initialized sessionless attribution.\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		fmt.Fprintf(out, "  Repo Link:     %s\n", repoLinkStatus)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
