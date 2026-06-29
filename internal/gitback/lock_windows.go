//go:build windows

package gitback

import (
	"errors"
	"os"
	"syscall"
)

// errorSharingViolation is the Win32 ERROR_SHARING_VIOLATION (32). The Go
// syscall package exports ERROR_ACCESS_DENIED but not this one, so name it here
// (value per the Win32 System Error Codes) rather than pull in
// golang.org/x/sys/windows for a single constant.
const errorSharingViolation syscall.Errno = 32

// isContendedCreateErr reports whether err from the O_CREATE|O_EXCL create in
// acquireLock means "the lock is contended, retry" rather than "fatal, give up".
//
// On Windows a holder releasing the lock (os.Remove) leaves the file briefly in
// a pending-delete state; a concurrent create against it fails with
// ERROR_ACCESS_DENIED, and an open sharing conflict fails with
// ERROR_SHARING_VIOLATION — neither is ERROR_ALREADY_EXISTS. Both mean the lock
// is momentarily held by a racing process and the caller should poll, exactly
// like os.ErrExist. Any other error is genuine and surfaces unchanged.
func isContendedCreateErr(err error) bool {
	return errors.Is(err, os.ErrExist) ||
		errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errorSharingViolation)
}
