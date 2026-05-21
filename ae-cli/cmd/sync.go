package cmd

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/repolink"
	"github.com/spf13/cobra"
)

var newSyncEngine = attributionlocal.NewSyncEngine
var runSyncEngineForWorkspace = func(engine *attributionlocal.SyncEngine, ctx context.Context, repoRoot string) error {
	return engine.RunForWorkspace(ctx, repoRoot)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan local artifacts and sync usage to the backend",
	RunE: func(cmd *cobra.Command, args []string) error {
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		if resolveToken(configToken, "") == "" {
			return fmt.Errorf("not logged in — run 'ae-cli login'")
		}
		ctx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		if _, err := repolink.Ensure(context.Background(), apiClient, gitRemoteURLForCutover(), gitBranchForCutover()); err != nil {
			return fmt.Errorf("ensure repo link: %w", err)
		}
		h := hooks.NewHandler(newHookUploader())
		if err := h.Flush(context.Background(), ctx.repoRoot); err != nil {
			return fmt.Errorf("flush pending hook queue: %w", err)
		}
		engine := newSyncEngine(apiClient)
		if err := runSyncEngineForWorkspace(engine, context.Background(), ctx.repoRoot); err != nil {
			return fmt.Errorf("run attribution sync: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced local attribution data for %s\n", ctx.repoRoot)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
