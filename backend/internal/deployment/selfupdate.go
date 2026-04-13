package deployment

import (
	"context"
	"fmt"
	"os"
)

func ApplyBinarySwap(_ context.Context, paths RuntimePaths, newBinaryPath string) error {
	_ = os.Remove(paths.BackupBinary)
	if err := os.Rename(paths.RuntimeBinary, paths.BackupBinary); err != nil {
		return fmt.Errorf("backup current runtime binary: %w", err)
	}
	if err := os.Rename(newBinaryPath, paths.RuntimeBinary); err != nil {
		if restoreErr := os.Rename(paths.BackupBinary, paths.RuntimeBinary); restoreErr != nil {
			return fmt.Errorf("replace runtime binary: %w (restore backup: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace runtime binary: %w", err)
	}
	return nil
}

func RollbackBinarySwap(paths RuntimePaths) error {
	if _, err := os.Stat(paths.BackupBinary); err != nil {
		return fmt.Errorf("stat backup binary: %w", err)
	}
	_ = os.Remove(paths.RuntimeBinary)
	if err := os.Rename(paths.BackupBinary, paths.RuntimeBinary); err != nil {
		return fmt.Errorf("restore backup binary: %w", err)
	}
	return nil
}
