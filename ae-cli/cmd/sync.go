package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		if err := hooks.UpsertPendingSyncTask(hooks.SyncTask{
			WorkspaceID:     execCtx.WorkspaceID,
			RepoRoot:        gitCtx.RepoRoot,
			ServerURL:       execCtx.ServerURL,
			AuthSubject:     execCtx.AuthSubject,
			RepoConfigID:    execCtx.RepoConfigID,
			RepoKey:         execCtx.RepoKey,
			Status:          hooks.SyncTaskStatusPending,
			LastRequestedAt: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("upsert pending sync task: %w", err)
		}
		if err := runBackgroundSyncTask(context.Background(), execCtx, newHookUploader()); errors.Is(err, hooks.ErrSyncTaskAlreadyRunning) {
			fmt.Fprintf(cmd.OutOrStdout(), "Attribution sync already running for %s\n", attrCtx.repoRoot)
			task, _, loadErr := hooks.LoadSyncTaskRecovering(execCtx.WorkspaceID)
			if loadErr != nil {
				return fmt.Errorf("load sync task: %w", loadErr)
			}
			printSyncTaskStatus(cmd.OutOrStdout(), task)
			return nil
		} else if err != nil {
			return fmt.Errorf("run pending sync task: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced local attribution data for %s\n", attrCtx.repoRoot)
		return nil
	},
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local attribution sync status",
	RunE: func(cmd *cobra.Command, args []string) error {
		attrCtx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ".", Uploads: true, Binding: currentHookBinding()})
		if err != nil {
			return err
		}
		printHookStatus(cmd.OutOrStdout(), status)
		task, recovered, err := hooks.LoadSyncTaskRecovering(attrCtx.workspaceID)
		if err != nil {
			return fmt.Errorf("load sync task: %w", err)
		}
		if recovered {
			fmt.Fprintln(cmd.OutOrStdout(), "Sync Task: corrupt sync task moved aside")
		}
		if task != nil {
			var runnerRecovered bool
			task, runnerRecovered, err = hooks.RecoverInactiveSyncTaskRunner(attrCtx.workspaceID, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("recover inactive sync runner: %w", err)
			}
			if runnerRecovered {
				fmt.Fprintln(cmd.OutOrStdout(), "Sync Task: inactive runner recovered")
			}
		}
		printSyncTaskStatus(cmd.OutOrStdout(), task)
		unresolvedCount, err := hooks.CountUnresolvedHookEvents()
		if err != nil {
			return fmt.Errorf("count unresolved hook events: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Unresolved Hook Events: %d\n", unresolvedCount)

		deadLetterCount, err := attributionlocal.CountToolUsageDeadLetters(attrCtx.workspaceID)
		if err != nil {
			return fmt.Errorf("count tool usage dead letters: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Tool Usage Dead Letters: %d\n", deadLetterCount)
		return nil
	},
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	rootCmd.AddCommand(syncCmd)
}
