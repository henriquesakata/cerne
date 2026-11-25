//go:build !windows

package cli

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive flock on f without waiting; a lock another
// open file holds is errCheckpointLocked. flock locks belong to the open file,
// so two opens in one process exclude each other too.
func tryLockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errCheckpointLocked
	}
	return err
}

// lockFile is tryLockFile that waits for the lock (B-0084: the stored room
// sessions' read-modify-write).
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}
