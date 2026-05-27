//go:build windows

package hooks

import "os/exec"

func detachBackgroundSyncCommand(cmd *exec.Cmd) {
	// No-op: keep the Windows build portable and rely on Release after Start.
}
