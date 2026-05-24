package cmd

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var newSyncEngine = attributionlocal.NewSyncEngine
var runSyncEngine = func(engine *attributionlocal.SyncEngine, ctx context.Context, opts attributionlocal.RunOptions) error {
	return engine.Run(ctx, opts)
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
		attrCtx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		gitCtx, err := hooks.DetectGitContext(attrCtx.repoRoot)
		if err != nil {
			return err
		}
		execCtx, ok := resolveHookExecutionContext(context.Background(), gitCtx)
		if !ok {
			return fmt.Errorf("repository is not registered or reporting-enabled; run 'ae-cli init' or ask an admin to configure it")
		}
		h := hooks.NewHandler(newHookUploader())
		if err := h.FlushResolved(context.Background(), execCtx); err != nil {
			return fmt.Errorf("flush pending hook queue: %w", err)
		}
		engine := newSyncEngine(apiClient)
		if err := runSyncEngine(engine, context.Background(), attributionlocal.RunOptions{
			WorkspaceRoot: gitCtx.RepoRoot,
			WorkspaceID:   execCtx.WorkspaceID,
			ServerURL:     execCtx.ServerURL,
			AuthSubject:   execCtx.AuthSubject,
			RepoConfigID:  execCtx.RepoConfigID,
			RepoKey:       execCtx.RepoKey,
			DurableReplay: execCtx.DurableReplay,
			ManagedUpload: true,
		}); err != nil {
			return fmt.Errorf("run attribution sync: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced local attribution data for %s\n", attrCtx.repoRoot)
		return nil
	},
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local attribution sync status",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ".", Uploads: true, Binding: currentHookBinding()})
		if err != nil {
			return err
		}
		printHookStatus(cmd.OutOrStdout(), status)
		return nil
	},
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	rootCmd.AddCommand(syncCmd)
}
