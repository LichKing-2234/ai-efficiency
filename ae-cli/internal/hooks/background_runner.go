package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/pilot"
)

var syncTaskLeaseTTL = time.Hour
var syncTaskSpawnCooldown = 30 * time.Second
var syncTaskRunTimeout = 5 * time.Minute
var machineSyncOwnerTimeout = 10 * time.Minute
var v2ClaimProgressBatchSize = 64
var scanCodexV2ClaimSource = func(scan v2ClaimSource, ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
	return scan.ScanSource(ctx, sourceKey, options)
}

var errSyncTaskRecoveryRepository = errors.New("recovery repository is unavailable")
var errSyncTaskRecoveryCommit = errors.New("recovery commit is unavailable")
var errSyncTaskRecoveryCheckpoint = errors.New("recovery checkpoint identity does not match")

var spawnBackgroundSyncRunner = func(repoRoot string) error {
	aeCLI, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve ae-cli executable: %w", err)
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()

	cmd := exec.Command(aeCLI, "hook", "background-sync")
	cmd.Dir = repoRoot
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Stdin = nil
	detachBackgroundSyncCommand(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background sync runner: %w", err)
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func RunDetachedPendingSyncTask(execCtx ExecutionContext, uploader Uploader) error {
	ctx := context.Background()
	var cancel context.CancelFunc
	if machineSyncOwnerTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, machineSyncOwnerTimeout)
		defer cancel()
	}
	err := RunPendingSyncTask(ctx, execCtx, uploader)
	if errors.Is(err, ErrSyncTaskAlreadyRunning) {
		return nil
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err := spawnBackgroundSyncRunner(execCtx.RepoRoot); err != nil {
		return fmt.Errorf("start machine sync successor: %w", err)
	}
	return nil
}

func RunPendingSyncTask(ctx context.Context, execCtx ExecutionContext, uploader Uploader) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	return withMachineSyncRunLock(ctx, execCtx, func() error {
		if provider, ok := uploader.(interface{ RelayProviderID() int }); ok && provider.RelayProviderID() > 0 {
			if _, err := MigrateMachineSyncBacklog(SyncTaskMigrationBinding{
				ServerURL: execCtx.ServerURL, AuthSubject: execCtx.AuthSubject, RelayProviderID: provider.RelayProviderID(),
			}, time.Now().UTC()); err != nil {
				return fmt.Errorf("migrate machine sync backlog: %w", err)
			}
		}
		return drainPendingSyncTasks(ctx, execCtx, uploader)
	})
}

func drainPendingSyncTasks(ctx context.Context, execCtx ExecutionContext, uploader Uploader) error {
	blocked := map[string]int{}
	var firstErr error
	for {
		tasks, err := ListSyncTasks()
		if err != nil {
			return err
		}
		pending := matchingMachineSyncTasks(tasks, execCtx)
		if len(pending) == 0 {
			return firstErr
		}
		progressed := false
		for _, queued := range pending {
			if generation, failed := blocked[queued.WorkspaceID]; failed && generation >= queued.RequestGeneration {
				continue
			}
			expired, pruneErr := pruneExpiredV2SyncTriggers(&queued, time.Now().UTC(), v2SyncTriggerRetention)
			if pruneErr != nil {
				pruneErr = syncTaskFailure(SyncTaskFailureStageLocalState, "expired local trigger cleanup failed", pruneErr)
				_ = MarkSyncTaskFailure(&queued, time.Now().UTC(), pruneErr)
				blocked[queued.WorkspaceID] = queued.RequestGeneration
				if firstErr == nil {
					firstErr = pruneErr
				}
				continue
			}
			if expired {
				progressed = true
				continue
			}
			workspaceCtx, recoveryErr := executionContextForSyncTask(queued, execCtx)
			if recoveryErr != nil {
				_ = MarkSyncTaskFailure(&queued, time.Now().UTC(), recoveryErr)
				blocked[queued.WorkspaceID] = queued.RequestGeneration
				if firstErr == nil {
					firstErr = recoveryErr
				}
				continue
			}
			if err := runPendingSyncWorkspace(ctx, workspaceCtx, uploader); err != nil {
				blocked[queued.WorkspaceID] = queued.RequestGeneration
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			progressed = true
		}
		if !progressed {
			return firstErr
		}
	}
}

func runPendingSyncWorkspace(ctx context.Context, execCtx ExecutionContext, uploader Uploader) error {
	parentCtx := ctx
	if syncTaskRunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, syncTaskRunTimeout)
		defer cancel()
	}
	for {
		task, err := LoadSyncTask(execCtx.WorkspaceID)
		if err != nil || task == nil {
			return err
		}

		startedAt := time.Now().UTC()
		acquired, err := TryAcquireSyncTaskLease(task, os.Getpid(), startedAt, syncTaskLeaseTTL)
		if err != nil {
			return err
		}
		if !acquired {
			return ErrSyncTaskAlreadyRunning
		}

		passGeneration := task.RequestGeneration
		if len(execCtx.runnableV2TriggerIDs) > 0 {
			runnable := make([]V2SyncTrigger, 0, len(execCtx.runnableV2TriggerIDs))
			for _, trigger := range task.V2Triggers {
				if _, ok := execCtx.runnableV2TriggerIDs[trigger.EventID]; ok {
					runnable = append(runnable, trigger)
				}
			}
			task.V2Triggers = runnable
		}
		if err := runPendingSyncPass(ctx, execCtx, uploader, task); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				if yieldErr := MarkSyncTaskYielded(task, time.Now().UTC()); yieldErr != nil {
					return yieldErr
				}
				if parentCtx.Err() != nil {
					return parentCtx.Err()
				}
				return nil
			}
			_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
			return err
		}
		idle, err := CompleteSyncTaskPass(task, passGeneration, time.Now().UTC())
		if err != nil {
			return err
		}
		if execCtx.retainedV2TriggerError != nil {
			if err := MarkSyncTaskFailure(task, time.Now().UTC(), execCtx.retainedV2TriggerError); err != nil {
				return err
			}
			return execCtx.retainedV2TriggerError
		}
		if idle {
			return nil
		}
		nextCtx, recoveryErr := executionContextForSyncTask(*task, execCtx)
		if recoveryErr != nil {
			_ = MarkSyncTaskFailure(task, time.Now().UTC(), recoveryErr)
			return recoveryErr
		}
		execCtx = nextCtx
	}
}

func matchingMachineSyncTasks(tasks []SyncTask, seed ExecutionContext) []SyncTask {
	matched := make([]SyncTask, 0, len(tasks))
	for _, task := range tasks {
		if normalizeHookServerURL(task.ServerURL) != normalizeHookServerURL(seed.ServerURL) || strings.TrimSpace(task.AuthSubject) != strings.TrimSpace(seed.AuthSubject) {
			continue
		}
		if !executionContextFromSyncTask(task).hasStableReplayBinding() {
			continue
		}
		matched = append(matched, task)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		leftSeed := matched[i].WorkspaceID == seed.WorkspaceID
		rightSeed := matched[j].WorkspaceID == seed.WorkspaceID
		if leftSeed != rightSeed {
			return leftSeed
		}
		return false
	})
	return matched
}

func executionContextFromSyncTask(task SyncTask) ExecutionContext {
	return ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID,
		RepoKey: task.RepoKey, RepoFullName: task.RepoKey, WorkspaceID: task.WorkspaceID,
		RepoRoot: task.RepoRoot, DurableReplay: true,
	}
}

func executionContextForSyncTask(task SyncTask, seed ExecutionContext) (ExecutionContext, error) {
	execCtx := executionContextFromSyncTask(task)
	commitTriggers := syncTaskCommitTriggers(task.V2Triggers)
	if len(commitTriggers) == 0 {
		if _, err := os.Stat(execCtx.RepoRoot); err == nil {
			return execCtx, nil
		} else {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", err)
		}
	}
	repoRoot := execCtx.RepoRoot
	workspaceID := task.WorkspaceID
	if err := validateSyncTaskRepository(task, repoRoot, workspaceID); err != nil {
		if task.RepoConfigID != seed.RepoConfigID || strings.TrimSpace(task.RepoKey) != strings.TrimSpace(seed.RepoKey) {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", fmt.Errorf("recovery repository identity does not match"))
		}
		if err := validateSyncTaskRepository(task, seed.RepoRoot, seed.WorkspaceID); err != nil {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", err)
		}
		repoRoot = seed.RepoRoot
		workspaceID = seed.WorkspaceID
	}
	runnable := make(map[string]struct{}, len(task.V2Triggers))
	var retainedErr error
	for _, trigger := range task.V2Triggers {
		if strings.TrimSpace(trigger.Kind) != "post-commit" || strings.TrimSpace(trigger.CommitSHA) == "" {
			runnable[trigger.EventID] = struct{}{}
			continue
		}
		if trigger.RelayProviderID <= 0 {
			if retainedErr == nil {
				retainedErr = syncTaskFailure(SyncTaskFailureStageLocalState, "provider identity unavailable for recovery", fmt.Errorf("trigger provider identity is missing"))
			}
			continue
		}
		err := validateSyncTaskTrigger(task, repoRoot, workspaceID, trigger)
		if err == nil {
			runnable[trigger.EventID] = struct{}{}
			continue
		}
		var triggerErr error
		switch {
		case errors.Is(err, errSyncTaskRecoveryCommit):
			triggerErr = syncTaskFailure(SyncTaskFailureStageLocalState, "commit unavailable in recovery checkout", err)
		case errors.Is(err, errSyncTaskRecoveryCheckpoint):
			triggerErr = syncTaskFailure(SyncTaskFailureStageLocalState, "checkpoint identity does not match", err)
		default:
			triggerErr = syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", err)
		}
		if retainedErr == nil {
			retainedErr = triggerErr
		}
	}
	if len(runnable) == 0 && retainedErr != nil {
		return execCtx, retainedErr
	}
	execCtx.RepoRoot = repoRoot
	execCtx.runnableV2TriggerIDs = runnable
	execCtx.retainedV2TriggerError = retainedErr
	return execCtx, nil
}

func syncTaskCommitTriggers(triggers []V2SyncTrigger) []V2SyncTrigger {
	result := make([]V2SyncTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		if strings.TrimSpace(trigger.Kind) == "post-commit" && strings.TrimSpace(trigger.CommitSHA) != "" {
			result = append(result, trigger)
		}
	}
	return result
}

func validateSyncTaskCheckout(task SyncTask, repoRoot, workspaceID string, triggers []V2SyncTrigger) error {
	if err := validateSyncTaskRepository(task, repoRoot, workspaceID); err != nil {
		return err
	}
	for _, trigger := range triggers {
		if err := validateSyncTaskTrigger(task, repoRoot, workspaceID, trigger); err != nil {
			return err
		}
	}
	return nil
}

func validateSyncTaskRepository(task SyncTask, repoRoot, workspaceID string) error {
	gitCtx, err := DetectGitContext(repoRoot)
	if err != nil {
		return fmt.Errorf("%w: %w", errSyncTaskRecoveryRepository, err)
	}
	if strings.TrimSpace(gitCtx.RepoKey) != strings.TrimSpace(task.RepoKey) || strings.TrimSpace(gitCtx.WorkspaceID) != strings.TrimSpace(workspaceID) {
		return fmt.Errorf("%w: Git identity does not match", errSyncTaskRecoveryRepository)
	}
	return nil
}

func validateSyncTaskTrigger(task SyncTask, repoRoot, workspaceID string, trigger V2SyncTrigger) error {
	if err := validateSyncTaskRepository(task, repoRoot, workspaceID); err != nil {
		return err
	}
	if triggerServer := normalizeHookServerURL(trigger.ServerURL); triggerServer != "" && triggerServer != normalizeHookServerURL(task.ServerURL) {
		return fmt.Errorf("%w: trigger server identity does not match", errSyncTaskRecoveryRepository)
	}
	if triggerOwner := strings.TrimSpace(trigger.AuthSubject); triggerOwner != "" && triggerOwner != strings.TrimSpace(task.AuthSubject) {
		return fmt.Errorf("%w: trigger owner identity does not match", errSyncTaskRecoveryRepository)
	}
	if trigger.RepoConfigID > 0 && trigger.RepoConfigID != task.RepoConfigID {
		return fmt.Errorf("%w: trigger Repository ID does not match", errSyncTaskRecoveryRepository)
	}
	if triggerRepo := strings.TrimSpace(trigger.RepoKey); triggerRepo != "" && triggerRepo != strings.TrimSpace(task.RepoKey) {
		return fmt.Errorf("%w: trigger Repository identity does not match", errSyncTaskRecoveryRepository)
	}
	if triggerWorkspace := strings.TrimSpace(trigger.WorkspaceID); triggerWorkspace != "" && triggerWorkspace != strings.TrimSpace(task.WorkspaceID) {
		return fmt.Errorf("%w: trigger workspace identity does not match", errSyncTaskRecoveryRepository)
	}
	taskContext := executionContextFromSyncTask(task)
	expectedEventID, err := CheckpointEventID(eventIDRepoHint(taskContext), trigger.CommitSHA)
	if err != nil {
		return fmt.Errorf("%w: derive expected event ID: %w", errSyncTaskRecoveryCheckpoint, err)
	}
	if strings.TrimSpace(trigger.EventID) != expectedEventID {
		return errSyncTaskRecoveryCheckpoint
	}
	if !syncTaskCommitReachable(repoRoot, trigger.CommitSHA) {
		return fmt.Errorf("%w: trigger commit is not reachable", errSyncTaskRecoveryCommit)
	}
	return nil
}

func syncTaskCommitReachable(repoRoot, commitSHA string) bool {
	commitSHA = strings.TrimSpace(commitSHA)
	if (len(commitSHA) != 40 && len(commitSHA) != 64) || !validHexObjectID(commitSHA) {
		return false
	}
	resolved, err := gitOutput(repoRoot, "rev-parse", "--verify", commitSHA+"^{commit}")
	if err != nil || !strings.EqualFold(strings.TrimSpace(resolved), commitSHA) {
		return false
	}
	if _, err := gitOutput(repoRoot, "merge-base", "--is-ancestor", commitSHA, "HEAD"); err == nil {
		return true
	}
	refs, err := gitOutput(repoRoot, "for-each-ref", "--format=%(refname)", "--contains="+commitSHA)
	return err == nil && strings.TrimSpace(refs) != ""
}

func validHexObjectID(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func runPendingSyncPass(ctx context.Context, execCtx ExecutionContext, uploader Uploader, task *SyncTask) error {
	h := NewHandler(uploader)
	if err := h.FlushUnresolvedResolved(ctx, execCtx); err != nil {
		return err
	}
	if err := h.FlushResolved(ctx, execCtx); err != nil {
		return err
	}
	syncClient := h.attributionSyncClient()
	if h.v2ClaimClient() != nil {
		protocolSource, ok := uploader.(interface {
			AttributionProtocol() client.AttributionProtocol
		})
		if !ok {
			return fmt.Errorf("v2 uploader does not expose attribution protocol")
		}
		protocol := protocolSource.AttributionProtocol()
		if err := protocol.Validate(); err != nil {
			return err
		}
		claimErr := runV2ClaimSync(ctx, uploader, execCtx, task, protocol)
		// The usage surface uploads alongside the claims rather than instead of
		// them. Returning after the claim sync — the previous shape — meant a
		// compact machine never uploaded a tool usage event at all: dashboards
		// starved while claims flowed, and nothing reported it. Usage also does
		// not wait on the claim sync's own preconditions — it needs no relay
		// provider — so a claim-side failure leaves it running.
		if syncClient == nil {
			return claimErr
		}
		usageErr := runAttributionSync(ctx, attributionlocal.RunOptions{
			WorkspaceRoot: execCtx.RepoRoot, WorkspaceID: execCtx.WorkspaceID, ServerURL: execCtx.ServerURL,
			AuthSubject: execCtx.AuthSubject, RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey,
			DurableReplay: execCtx.DurableReplay, ManagedUpload: true,
		}, syncClient)
		return errors.Join(claimErr, usageErr)
	}
	if syncClient == nil {
		return fmt.Errorf("sync uploader does not expose tool usage client")
	}
	return runAttributionSync(ctx, attributionlocal.RunOptions{
		WorkspaceRoot: execCtx.RepoRoot, WorkspaceID: execCtx.WorkspaceID, ServerURL: execCtx.ServerURL,
		AuthSubject: execCtx.AuthSubject, RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey,
		DurableReplay: execCtx.DurableReplay, ManagedUpload: true,
	}, syncClient)
}

type v2ClaimUploader interface {
	V2ClaimClient() attributionlocal.V2ClaimBackendClient
	RelayProviderID() int
}

func runV2ClaimSync(ctx context.Context, uploader Uploader, execCtx ExecutionContext, task *SyncTask, protocol client.AttributionProtocol) error {
	v2, ok := uploader.(v2ClaimUploader)
	if !ok || v2.V2ClaimClient() == nil || v2.RelayProviderID() <= 0 || task == nil {
		return nil
	}
	triggers := append([]V2SyncTrigger(nil), task.V2Triggers...)
	if len(triggers) == 0 && task.TriggerEventID != "" {
		triggers = []V2SyncTrigger{{Kind: task.TriggerKind, EventID: task.TriggerEventID, CommitSHA: task.TriggerCommitSHA, Branch: task.TriggerBranch, CapturedAt: task.LastRequestedAt}}
	}
	options := make([]attributionlocal.V2ClaimScanOptions, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger.Kind != "post-commit" || trigger.EventID == "" || trigger.CommitSHA == "" {
			continue
		}
		providerID := trigger.RelayProviderID
		if providerID <= 0 {
			providerID = v2.RelayProviderID()
		}
		options = append(options, attributionlocal.V2ClaimScanOptions{
			RepoRoot: execCtx.RepoRoot, CommitSHA: trigger.CommitSHA, RelayProviderID: providerID,
			RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey, WorkspaceID: execCtx.WorkspaceID,
			CheckpointEventID: trigger.EventID,
		})
	}
	// Commits whose evidence had not arrived when their own scan ran are retried
	// here, so a scan can have work to do even when this task carries no trigger
	// of its own.
	pendingCommits, err := LoadV2UnprovenCommits(execCtx.WorkspaceID)
	if err != nil {
		return syncTaskFailure(SyncTaskFailureStageLocalState, "pending claim commits could not be loaded", err)
	}
	options = appendUnprovenCommitOptions(options, pendingCommits)

	if len(options) > 0 {
		scan, err := prepareV2ClaimSource(ctx, time.Now().UTC().Add(-v2ClaimSourceWindow))
		if err != nil {
			return syncTaskFailure(SyncTaskFailureStageSourceDiscovery, "local claim evidence discovery failed", err)
		}
		contextID, err := v2ClaimScanContextID(options[0])
		if err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be prepared", err)
		}
		progress, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID)
		if err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be loaded", err)
		}
		if progress == nil || progress.Version != v2ClaimScanProgressVersion || progress.ContextID != contextID {
			progress = &V2ClaimScanProgress{Version: v2ClaimScanProgressVersion, WorkspaceID: execCtx.WorkspaceID, ContextID: contextID, StartedAt: time.Now().UTC()}
		}
		if progress.SourceTurnKeys == nil {
			progress.SourceTurnKeys = map[string][]string{}
		}
		if progress.SourceEvidenceKeys == nil {
			progress.SourceEvidenceKeys = map[string]string{}
		}
		progress.SourceKeys = scan.SourceKeys()
		progress.Complete = false
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
		}
		completed := v2CompletedSourceSet(progress.CompletedUnits)
		provenCommits := map[string]struct{}{}
		type scannedSource struct {
			sourceKey    string
			pendingUnits []string
			candidates   []attributionlocal.V2ClaimCandidate
		}
		var batch []scannedSource
		flushBatch := func() error {
			if len(batch) == 0 {
				return nil
			}
			hasCandidates := false
			for _, source := range batch {
				hasCandidates = hasCandidates || len(source.candidates) > 0
			}
			if hasCandidates {
				if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
					for _, source := range batch {
						attributionlocal.MergeV2ClaimState(state, source.candidates, time.Now().UTC())
					}
					return nil
				}); err != nil {
					return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
				}
			}
			for _, source := range batch {
				for key := range provenCommitKeys(source.candidates) {
					provenCommits[key] = struct{}{}
				}
				turnKeys := attributionlocal.MergeV2ClaimTurnKeys(progress.SourceTurnKeys[source.sourceKey], attributionlocal.V2ClaimTurnKeys(source.candidates))
				progress.SourceTurnKeys[source.sourceKey] = turnKeys
				progress.SourceEvidenceKeys[source.sourceKey] = scan.SourceEvidenceKey(turnKeys)
				pendingSet := v2CompletedSourceSet(source.pendingUnits)
				keptUnits := progress.CompletedUnits[:0]
				for _, unitID := range progress.CompletedUnits {
					if _, replace := pendingSet[unitID]; !replace {
						keptUnits = append(keptUnits, unitID)
					}
				}
				progress.CompletedUnits = append(keptUnits, source.pendingUnits...)
				for _, unitID := range source.pendingUnits {
					completed[unitID] = struct{}{}
				}
			}
			if err := SaveV2ClaimScanProgress(progress); err != nil {
				return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
			}
			batch = batch[:0]
			if hasCandidates {
				_, err := deliverV2ClaimState(ctx, v2.V2ClaimClient(), protocol)
				return err
			}
			return nil
		}
		for _, sourceKey := range progress.SourceKeys {
			evidenceChanged := len(progress.SourceTurnKeys[sourceKey]) > 0 && progress.SourceEvidenceKeys[sourceKey] != scan.SourceEvidenceKey(progress.SourceTurnKeys[sourceKey])
			pendingOptions := make([]attributionlocal.V2ClaimScanOptions, 0, len(options))
			pendingUnits := make([]string, 0, len(options))
			for _, option := range options {
				unitID, err := v2ClaimScanUnitID(sourceKey, option)
				if err != nil {
					return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be prepared", err)
				}
				if _, ok := completed[unitID]; ok && !evidenceChanged {
					continue
				}
				pendingOptions = append(pendingOptions, option)
				pendingUnits = append(pendingUnits, unitID)
			}
			if len(pendingOptions) == 0 {
				continue
			}
			candidates, err := scanCodexV2ClaimSource(scan, ctx, sourceKey, pendingOptions)
			if err != nil {
				if flushErr := flushBatch(); flushErr != nil {
					return flushErr
				}
				return syncTaskFailure(SyncTaskFailureStageSourceScan, "local Codex evidence scan failed", err)
			}
			batch = append(batch, scannedSource{sourceKey: sourceKey, pendingUnits: pendingUnits, candidates: candidates})
			if len(candidates) > 0 || len(batch) >= max(1, v2ClaimProgressBatchSize) {
				if err := flushBatch(); err != nil {
					return err
				}
			}
		}
		if err := flushBatch(); err != nil {
			return err
		}
		progress.Complete = true
		// Saved apart from progress, which finishV2ClaimScan deletes below.
		now := time.Now().UTC()
		if err := SaveV2UnprovenCommits(execCtx.WorkspaceID, mergeUnprovenCommits(pendingCommits, options, provenCommits, now), now); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "pending claim commits could not be saved", err)
		}
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
		}
		if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
			scan.FinalizeCandidates(state.Claims)
			return nil
		}); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim state could not be finalized", err)
		}
	}
	if _, err := deliverV2ClaimState(ctx, v2.V2ClaimClient(), protocol); err != nil {
		return err
	}
	return finishV2ClaimScan(execCtx.WorkspaceID, len(options) > 0)
}

func deliverV2ClaimState(ctx context.Context, backend attributionlocal.V2ClaimBackendClient, protocol client.AttributionProtocol) (attributionlocal.V2DeliverySummary, error) {
	var groups []client.AttributionV2ClaimGroup
	var summary attributionlocal.V2DeliverySummary
	err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		groups = append(groups, attributionlocal.UploadableV2ClaimGroups(state.Claims)...)
		summary = attributionlocal.SummarizeV2ClaimDelivery(state)
		return nil
	})
	if err != nil {
		return summary, syncTaskFailure(SyncTaskFailureStageLocalState, "local claim state could not be loaded", err)
	}
	if len(groups) == 0 {
		if summary.UpgradeRequired > 0 {
			return summary, syncTaskFailure(SyncTaskFailureStageAcknowledgement, "backend acknowledgement requires recovery", fmt.Errorf("v2 claim delivery requires recovery: upgrade_required=%d", summary.UpgradeRequired))
		}
		return summary, nil
	}
	result, err := backend.SendAttributionV2Claims(ctx, groups)
	if err != nil {
		return summary, syncTaskFailure(SyncTaskFailureStageBackendDelivery, "backend claim delivery failed", err)
	}
	var ackErr error
	if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		ackErr = attributionlocal.ApplyV2ClaimAcknowledgements(state, groups, result, protocol, time.Now().UTC())
		summary = attributionlocal.SummarizeV2ClaimDelivery(state)
		return nil
	}); err != nil {
		return summary, syncTaskFailure(SyncTaskFailureStageLocalState, "local claim acknowledgement could not be saved", err)
	}
	if summary.Pending > 0 || summary.UpgradeRequired > 0 {
		if ackErr == nil {
			ackErr = fmt.Errorf("v2 claim delivery requires recovery: pending=%d upgrade_required=%d", summary.Pending, summary.UpgradeRequired)
		}
		return summary, syncTaskFailure(SyncTaskFailureStageAcknowledgement, "backend acknowledgement requires recovery", ackErr)
	}
	return summary, nil
}

func v2ClaimScanContextID(option attributionlocal.V2ClaimScanOptions) (string, error) {
	option.CommitSHA = ""
	option.CheckpointEventID = ""
	return v2ClaimScanDigest(option)
}

func v2ClaimScanUnitID(sourceKey string, option attributionlocal.V2ClaimScanOptions) (string, error) {
	return v2ClaimScanDigest(struct {
		SourceKey string                              `json:"source_key"`
		Option    attributionlocal.V2ClaimScanOptions `json:"option"`
	}{SourceKey: sourceKey, Option: option})
}

func v2ClaimScanDigest(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal v2 claim scan digest: %w", err)
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest[:]), nil
}

func v2CompletedSourceSet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

func finishV2ClaimScan(workspaceID string, scanned bool) error {
	if !scanned {
		return nil
	}
	if err := DeleteV2ClaimScanProgress(workspaceID); err != nil {
		return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be cleared", err)
	}
	return nil
}

// v2ClaimSource is the claim evidence a machine holds locally. Two readers
// satisfy it: LoongSuite Pilot, which covers every agent it instruments, and
// the Codex session files, which are all a machine without Pilot has.
type v2ClaimSource interface {
	SourceKeys() []string
	SourceEvidenceKey(turnKeys []string) string
	FinalizeCandidates(candidates []attributionlocal.V2ClaimCandidate)
	ScanSource(ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error)
}

// v2ClaimSourceWindow bounds how far back a claim source is read. It matches the
// retention the Codex reader already used.
const v2ClaimSourceWindow = 90 * 24 * time.Hour

// prepareV2ClaimSource picks the claim source this machine has.
//
// Pilot is preferred wherever it is collecting, for the same reason the usage
// surface prefers it: it covers every agent, while the Codex session files cover
// one. A machine without Pilot, or one whose collector has stopped, falls back
// to the Codex reader rather than losing the claims it can still prove.
func prepareV2ClaimSource(ctx context.Context, cutoff time.Time) (v2ClaimSource, error) {
	if (pilot.Checker{}).Running() {
		scan, err := attributionlocal.PrepareLocalV2ClaimScan("", cutoff)
		if err == nil && len(scan.SourceKeys()) > 0 {
			return scan, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return attributionlocal.PrepareCodexV2ClaimScan(ctx, "", cutoff)
}

// appendUnprovenCommitOptions adds the commits still waiting on evidence to the
// ones this task asked about, without repeating any the task already carries.
func appendUnprovenCommitOptions(options []attributionlocal.V2ClaimScanOptions, pending []V2UnprovenCommit) []attributionlocal.V2ClaimScanOptions {
	if len(pending) == 0 {
		return options
	}
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		seen[unprovenCommitKey(option)] = struct{}{}
	}
	for _, item := range pending {
		option := item.scanOptions()
		key := unprovenCommitKey(option)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		options = append(options, option)
	}
	return options
}
