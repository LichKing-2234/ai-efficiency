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
	if err := h.FlushResolved(ctx, execCtx); err != nil {
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	}

	syncClient := h.attributionSyncClient()
	if syncClient == nil {
		err := fmt.Errorf("sync uploader does not expose tool usage client")
		_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
		return err
	}

	if err := runAttributionSync(ctx, attributionlocal.RunOptions{
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
