//go:build !windows

package session

import (
	"errors"
	"os"
	"syscall"
)

func tryLockToolPaneFile(lockFile *os.File) error {
	return syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockToolPaneFile(lockFile *os.File) error {
	return syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
}

func isToolPaneLockContention(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
