package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var newSyncEngine = attributionlocal.NewSyncEngine
var runSyncEngine = func(engine *attributionlocal.SyncEngine, ctx context.Context, opts attributionlocal.RunOptions) error {
	return engine.Run(ctx, opts)
}
var syncRepoEligibilityTimeout = 10 * time.Second

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan local artifacts and sync usage to the backend",
	RunE: func(cmd *cobra.Command, args []string) error {
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		_, compactEnabled := loadEnabledReportingConfig()
		if resolveToken(configToken, "") == "" && !compactEnabled {
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
		execCtx, ok, err := resolveSyncExecutionContext(context.Background(), gitCtx)
		if err != nil {
			return err
		}
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
			TriggerKind:     "manual",
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
			if statusErr := printMachineSyncTaskStatus(cmd.OutOrStdout()); statusErr != nil {
				return fmt.Errorf("load machine sync tasks: %w", statusErr)
			}
			return nil
		} else if err != nil {
			return fmt.Errorf("run pending sync task: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Synced local attribution data for %s\n", attrCtx.repoRoot)
		return nil
	},
}

func resolveSyncExecutionContext(ctx context.Context, gitCtx *hooks.GitContext) (hooks.ExecutionContext, bool, error) {
	if execCtx, ok := resolveHookExecutionContext(ctx, gitCtx); ok {
		return execCtx, true, nil
	}
	if gitCtx == nil {
		return hooks.ExecutionContext{}, false, nil
	}

	binding := currentHookBinding()
	binding.RepoKey = firstNonEmpty(binding.RepoKey, gitCtx.RepoKey)
	var resolver hookRepoResolver
	if reportingConfig, ok := loadEnabledReportingConfig(); ok {
		resolver = attributionHookRepoResolverFor(binding.ServerURL, reportingConfig.ReporterToken)
	} else {
		resolver = hookRepoResolverFor(binding.ServerURL, usableToken())
	}
	if resolver == nil {
		return hooks.ExecutionContext{}, false, nil
	}

	refreshCtx, cancel := context.WithTimeout(ctx, syncRepoEligibilityTimeout)
	defer cancel()
	if err := hooks.RefreshCurrent(refreshCtx, resolver, gitCtx.RepoRoot, binding); err != nil {
		return hooks.ExecutionContext{}, false, fmt.Errorf("refresh repo eligibility: %w", err)
	}

	if execCtx, ok := resolveHookExecutionContext(ctx, gitCtx); ok {
		return execCtx, true, nil
	}
	return hooks.ExecutionContext{}, false, nil
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
		now := time.Now().UTC()
		if err := migrateMachineSyncBacklog(now); err != nil {
			return fmt.Errorf("migrate machine sync backlog: %w", err)
		}
		task, recovered, err := hooks.LoadSyncTaskRecovering(attrCtx.workspaceID)
		if err != nil {
			return fmt.Errorf("load sync task: %w", err)
		}
		if recovered {
			fmt.Fprintln(cmd.OutOrStdout(), "Sync Task: corrupt sync task moved aside")
		}
		if task != nil {
			var runnerRecovered bool
			task, runnerRecovered, err = hooks.RecoverInactiveSyncTaskRunner(attrCtx.workspaceID, now)
			if err != nil {
				return fmt.Errorf("recover inactive sync runner: %w", err)
			}
			if runnerRecovered {
				fmt.Fprintln(cmd.OutOrStdout(), "Sync Task: inactive runner recovered")
			}
		}
		printSyncTaskStatus(cmd.OutOrStdout(), task)
		if err := printMachineSyncTaskStatusAt(cmd.OutOrStdout(), now); err != nil {
			return fmt.Errorf("load machine sync tasks: %w", err)
		}
		if err := printV2ClaimDeliveryStatus(cmd.OutOrStdout()); err != nil {
			return err
		}
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

func printV2ClaimDeliveryStatus(out io.Writer) error {
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(out, "V2 Claim Delivery: pending=0 conflict=0 upgrade_required=0 accepted=0")
			fmt.Fprintln(out, "V2 Claim Gaps: missing_request_id=0 ambiguous_request_evidence=0 request_evidence_expired=0 unrecognized_patch_wrapper=0 invalid_local_usage=0 missing_local_usage=0 missing_structured_mutation=0 invalid_structured_mutation=0 commit_content_mismatch=0")
			return nil
		}
		return fmt.Errorf("load v2 claim delivery state: %w", err)
	}
	summary := attributionlocal.SummarizeV2ClaimDelivery(state)
	fmt.Fprintf(out, "V2 Claim Delivery: pending=%d conflict=%d upgrade_required=%d accepted=%d\n",
		summary.Pending, summary.Conflict, summary.UpgradeRequired, summary.Accepted)
	// Every reason a claim can fail, on one line. Five of these were bare
	// literals that nothing counted, so a machine holding thousands of them
	// reported only zeros and read as healthy.
	fmt.Fprintf(out, "V2 Claim Gaps: missing_request_id=%d ambiguous_request_evidence=%d request_evidence_expired=%d unrecognized_patch_wrapper=%d invalid_local_usage=%d missing_local_usage=%d missing_structured_mutation=%d invalid_structured_mutation=%d commit_content_mismatch=%d\n",
		summary.MissingRequestID, summary.AmbiguousRequestEvidence, summary.RequestEvidenceExpired, summary.UnrecognizedPatchWrapper,
		summary.InvalidLocalUsage, summary.MissingLocalUsage, summary.MissingStructuredMutation, summary.InvalidStructuredMutation, summary.CommitContentMismatch)
	return nil
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	rootCmd.AddCommand(syncCmd)
}
