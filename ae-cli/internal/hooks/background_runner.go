package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
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

	h := NewHandler(uploader)
	if err := h.FlushUnresolvedResolved(ctx, execCtx); err != nil {
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	}
	if err := h.FlushResolved(ctx, execCtx); err != nil {
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	}

	syncClient := h.attributionSyncClient()
	compactClient := h.compactAttributionSyncClient()
	if compactClient != nil {
		if err := (&attributionlocal.CompactSyncEngine{Client: compactClient}).Run(ctx, attributionlocal.CompactRunOptions{
			InstallationID: compactInstallationID(uploader),
			RepoRoot:       execCtx.RepoRoot,
			RepoConfigID:   execCtx.RepoConfigID,
			RepoKey:        execCtx.RepoKey,
			WorkspaceID:    execCtx.WorkspaceID,
			CommitSHA:      task.TriggerCommitSHA,
			Branch:         task.TriggerBranch,
			TriggerKind:    task.TriggerKind,
			Cutoff:         task.LastRequestedAt,
		}); err != nil {
			_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
			return err
		}
		if err := runV2ClaimSync(ctx, uploader, execCtx, task); err != nil {
			_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
			return err
		}
	} else if syncClient == nil {
		err := fmt.Errorf("sync uploader does not expose tool usage client")
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	} else if err := runAttributionSync(ctx, attributionlocal.RunOptions{
		WorkspaceRoot: execCtx.RepoRoot,
		WorkspaceID:   execCtx.WorkspaceID,
		ServerURL:     execCtx.ServerURL,
		AuthSubject:   execCtx.AuthSubject,
		RepoConfigID:  execCtx.RepoConfigID,
		RepoKey:       execCtx.RepoKey,
		DurableReplay: execCtx.DurableReplay,
		ManagedUpload: true,
	}, syncClient); err != nil {
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	}

	current, err := LoadSyncTask(execCtx.WorkspaceID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.LastRequestedAt.After(startedAt) {
		return MarkSyncTaskSuccess(current, time.Now().UTC())
	}
	return DeleteSyncTask(execCtx.WorkspaceID)
}

type v2ClaimUploader interface {
	V2ClaimClient() attributionlocal.V2ClaimBackendClient
	RelayProviderID() int
}

func runV2ClaimSync(ctx context.Context, uploader Uploader, execCtx ExecutionContext, task *SyncTask) error {
	v2, ok := uploader.(v2ClaimUploader)
	if !ok || v2.V2ClaimClient() == nil || v2.RelayProviderID() <= 0 || task == nil || task.TriggerEventID == "" || task.TriggerCommitSHA == "" {
		return nil
	}
	candidates, err := attributionlocal.ScanCodexV2ClaimsFromHome(ctx, "", attributionlocal.V2ClaimScanOptions{
		RepoRoot: execCtx.RepoRoot, CommitSHA: task.TriggerCommitSHA, RelayProviderID: v2.RelayProviderID(),
		RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey, WorkspaceID: execCtx.WorkspaceID,
		CheckpointEventID: task.TriggerEventID,
	})
	if err != nil {
		return err
	}
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		return err
	}
	attributionlocal.MergeV2ClaimState(state, candidates, time.Now().UTC())
	if err := attributionlocal.SaveV2ClaimState(state); err != nil {
		return err
	}
	groups := attributionlocal.UploadableV2ClaimGroups(state.Claims)
	if len(groups) == 0 {
		return nil
	}
	result, err := v2.V2ClaimClient().SendAttributionV2Claims(ctx, groups)
	if err != nil {
		return err
	}
	if result == nil || result.LedgerEpoch != "shadow_v2" || len(result.Results) != len(groups) {
		return fmt.Errorf("invalid v2 claim acknowledgement")
	}
	for _, item := range result.Results {
		if item.Group.Status != "persisted" && item.Group.Status != "duplicate_identical" {
			return fmt.Errorf("v2 claim %s was not acknowledged: %s", item.Group.ID, item.Group.Status)
		}
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
