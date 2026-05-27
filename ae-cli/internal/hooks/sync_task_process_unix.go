//go:build !windows

package hooks

import (
	"errors"
	"syscall"
)

func syncTaskProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
