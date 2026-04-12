//go:build windows

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryLockToolPaneFile(lockFile *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(lockFile.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
}

func unlockToolPaneFile(lockFile *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(lockFile.Fd()), 0, 1, 0, &overlapped)
}

func isToolPaneLockContention(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
