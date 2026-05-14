package cmd

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/spf13/cobra"
)

var newSyncEngine = attributionlocal.NewSyncEngine

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
		engine := newSyncEngine(apiClient)
		if err := engine.RunForWorkspace(context.Background(), ctx.repoRoot); err != nil {
			return fmt.Errorf("run attribution sync: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced local attribution data for %s\n", ctx.repoRoot)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
