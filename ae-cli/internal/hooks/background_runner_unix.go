//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

func detachBackgroundSyncCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
