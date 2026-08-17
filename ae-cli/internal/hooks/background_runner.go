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
)

var syncTaskLeaseTTL = time.Hour
var syncTaskSpawnCooldown = 30 * time.Second
var syncTaskRunTimeout = 5 * time.Minute
var v2ClaimProgressBatchSize = 64
var scanCodexV2ClaimSource = func(scan *attributionlocal.CodexV2ClaimScan, ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
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

func RunPendingSyncTask(ctx context.Context, execCtx ExecutionContext, uploader Uploader) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	return withMachineSyncRunLock(ctx, func() error {
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

	for {
		passGeneration := task.RequestGeneration
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
		if idle {
			return nil
		}
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
	if err := validateSyncTaskCheckout(task, execCtx.RepoRoot, task.WorkspaceID, commitTriggers); err == nil {
		return execCtx, nil
	}
	if task.RepoConfigID != seed.RepoConfigID || strings.TrimSpace(task.RepoKey) != strings.TrimSpace(seed.RepoKey) {
		return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", fmt.Errorf("recovery repository identity does not match"))
	}
	for _, trigger := range commitTriggers {
		if trigger.RelayProviderID <= 0 {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "provider identity unavailable for recovery", fmt.Errorf("trigger provider identity is missing"))
		}
	}
	if err := validateSyncTaskCheckout(task, seed.RepoRoot, seed.WorkspaceID, commitTriggers); err != nil {
		if errors.Is(err, errSyncTaskRecoveryCommit) {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "commit unavailable in recovery checkout", err)
		}
		if errors.Is(err, errSyncTaskRecoveryCheckpoint) {
			return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "checkpoint identity does not match", err)
		}
		return execCtx, syncTaskFailure(SyncTaskFailureStageLocalState, "repository checkout unavailable", err)
	}
	execCtx.RepoRoot = seed.RepoRoot
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
	gitCtx, err := DetectGitContext(repoRoot)
	if err != nil {
		return fmt.Errorf("%w: %w", errSyncTaskRecoveryRepository, err)
	}
	if strings.TrimSpace(gitCtx.RepoKey) != strings.TrimSpace(task.RepoKey) || strings.TrimSpace(gitCtx.WorkspaceID) != strings.TrimSpace(workspaceID) {
		return fmt.Errorf("%w: Git identity does not match", errSyncTaskRecoveryRepository)
	}
	taskContext := executionContextFromSyncTask(task)
	for _, trigger := range triggers {
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
	compactClient := h.compactAttributionSyncClient()
	if compactClient != nil {
		protocolSource, ok := uploader.(interface {
			AttributionProtocol() client.AttributionProtocol
		})
		if !ok {
			return fmt.Errorf("compact uploader does not expose attribution protocol")
		}
		protocol := protocolSource.AttributionProtocol()
		if err := protocol.Validate(); err != nil {
			return err
		}
		if protocol.V1WritePolicy == client.AttributionV1WritePolicyAccept {
			if err := (&attributionlocal.CompactSyncEngine{Client: compactClient}).Run(ctx, attributionlocal.CompactRunOptions{
				InstallationID: compactInstallationID(uploader), RepoRoot: execCtx.RepoRoot, RepoConfigID: execCtx.RepoConfigID,
				RepoKey: execCtx.RepoKey, WorkspaceID: execCtx.WorkspaceID, CommitSHA: task.TriggerCommitSHA,
				Branch: task.TriggerBranch, TriggerKind: task.TriggerKind, Cutoff: task.LastRequestedAt,
			}); err != nil {
				var upgrade *client.AttributionUpgradeRequiredError
				if !errors.As(err, &upgrade) {
					return err
				}
				protocol.V1WritePolicy = client.AttributionV1WritePolicyUpgradeNeeded
				protocol.MinimumCLIVersion = upgrade.MinimumCLIVersion
			}
		}
		return runV2ClaimSync(ctx, uploader, execCtx, task, protocol)
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
	if len(options) > 0 {
		scan, err := attributionlocal.PrepareCodexV2ClaimScan(ctx, "", time.Now().UTC().Add(-90*24*time.Hour))
		if err != nil {
			return syncTaskFailure(SyncTaskFailureStageSourceDiscovery, "local Codex evidence discovery failed", err)
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
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
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

type compactInstallationIdentity interface {
	InstallationID() string
}

func compactInstallationID(uploader Uploader) string {
	if identified, ok := uploader.(compactInstallationIdentity); ok {
		return identified.InstallationID()
	}
	return ""
}
