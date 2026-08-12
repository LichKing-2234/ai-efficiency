package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

var syncTaskLeaseTTL = time.Hour
var syncTaskSpawnCooldown = 30 * time.Second
var syncTaskRunTimeout = 5 * time.Minute
var scanCodexV2ClaimSource = func(scan *attributionlocal.CodexV2ClaimScan, ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
	return scan.ScanSource(ctx, sourceKey, options)
}

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
		options = append(options, attributionlocal.V2ClaimScanOptions{
			RepoRoot: execCtx.RepoRoot, CommitSHA: trigger.CommitSHA, RelayProviderID: v2.RelayProviderID(),
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
		if progress == nil || progress.ContextID != contextID {
			progress = &V2ClaimScanProgress{Version: 1, WorkspaceID: execCtx.WorkspaceID, ContextID: contextID, StartedAt: time.Now().UTC()}
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
				return syncTaskFailure(SyncTaskFailureStageSourceScan, "local Codex evidence scan failed", err)
			}
			if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
				attributionlocal.MergeV2ClaimState(state, candidates, time.Now().UTC())
				return nil
			}); err != nil {
				return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
			}
			turnKeys := attributionlocal.MergeV2ClaimTurnKeys(progress.SourceTurnKeys[sourceKey], attributionlocal.V2ClaimTurnKeys(candidates))
			progress.SourceTurnKeys[sourceKey] = turnKeys
			progress.SourceEvidenceKeys[sourceKey] = scan.SourceEvidenceKey(turnKeys)
			pendingSet := v2CompletedSourceSet(pendingUnits)
			keptUnits := progress.CompletedUnits[:0]
			for _, unitID := range progress.CompletedUnits {
				if _, replace := pendingSet[unitID]; !replace {
					keptUnits = append(keptUnits, unitID)
				}
			}
			progress.CompletedUnits = keptUnits
			progress.CompletedUnits = append(progress.CompletedUnits, pendingUnits...)
			for _, unitID := range pendingUnits {
				completed[unitID] = struct{}{}
			}
			if err := SaveV2ClaimScanProgress(progress); err != nil {
				return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
			}
		}
		progress.Complete = true
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim progress could not be saved", err)
		}
	}
	var groups []client.AttributionV2ClaimGroup
	var summary attributionlocal.V2DeliverySummary
	err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		groups = append(groups, attributionlocal.UploadableV2ClaimGroups(state.Claims)...)
		summary = attributionlocal.SummarizeV2ClaimDelivery(state)
		return nil
	})
	if err != nil {
		return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim state could not be loaded", err)
	}
	if len(groups) == 0 {
		if summary.Conflict > 0 || summary.UpgradeRequired > 0 {
			return syncTaskFailure(SyncTaskFailureStageAcknowledgement, "backend acknowledgement requires recovery", fmt.Errorf("v2 claim delivery requires recovery: conflicts=%d upgrade_required=%d", summary.Conflict, summary.UpgradeRequired))
		}
		return finishV2ClaimScan(execCtx.WorkspaceID, len(options) > 0)
	}
	result, err := v2.V2ClaimClient().SendAttributionV2Claims(ctx, groups)
	if err != nil {
		return syncTaskFailure(SyncTaskFailureStageBackendDelivery, "backend claim delivery failed", err)
	}
	var ackErr error
	if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		ackErr = attributionlocal.ApplyV2ClaimAcknowledgements(state, groups, result, protocol, time.Now().UTC())
		return nil
	}); err != nil {
		return syncTaskFailure(SyncTaskFailureStageLocalState, "local claim acknowledgement could not be saved", err)
	}
	if ackErr != nil {
		return syncTaskFailure(SyncTaskFailureStageAcknowledgement, "backend acknowledgement requires recovery", ackErr)
	}
	return finishV2ClaimScan(execCtx.WorkspaceID, len(options) > 0)
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
