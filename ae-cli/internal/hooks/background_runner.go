package hooks

import (
	"context"
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
	var scanned []attributionlocal.V2ClaimCandidate
	for _, trigger := range triggers {
		if trigger.Kind != "post-commit" || trigger.EventID == "" || trigger.CommitSHA == "" {
			continue
		}
		candidates, err := attributionlocal.ScanCodexV2ClaimsFromHome(ctx, "", attributionlocal.V2ClaimScanOptions{
			RepoRoot: execCtx.RepoRoot, CommitSHA: trigger.CommitSHA, RelayProviderID: v2.RelayProviderID(),
			RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey, WorkspaceID: execCtx.WorkspaceID,
			CheckpointEventID: trigger.EventID,
		})
		if err != nil {
			return err
		}
		scanned = append(scanned, candidates...)
	}
	var groups []client.AttributionV2ClaimGroup
	var summary attributionlocal.V2DeliverySummary
	err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		attributionlocal.MergeV2ClaimState(state, scanned, time.Now().UTC())
		groups = append(groups, attributionlocal.UploadableV2ClaimGroups(state.Claims)...)
		summary = attributionlocal.SummarizeV2ClaimDelivery(state)
		return nil
	})
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		if summary.Conflict > 0 || summary.UpgradeRequired > 0 {
			return fmt.Errorf("v2 claim delivery requires recovery: conflicts=%d upgrade_required=%d", summary.Conflict, summary.UpgradeRequired)
		}
		return nil
	}
	result, err := v2.V2ClaimClient().SendAttributionV2Claims(ctx, groups)
	if err != nil {
		return err
	}
	var ackErr error
	if err := attributionlocal.UpdateV2ClaimState(ctx, func(state *attributionlocal.V2ClaimState) error {
		ackErr = attributionlocal.ApplyV2ClaimAcknowledgements(state, groups, result, protocol, time.Now().UTC())
		return nil
	}); err != nil {
		return err
	}
	return ackErr
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
