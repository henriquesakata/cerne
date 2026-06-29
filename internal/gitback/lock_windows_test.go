//go:build windows

package gitback

import (
	"os"
	"syscall"
	"testing"
)

// On Windows a lockfile caught mid-removal by another holder fails the
// O_CREATE|O_EXCL create with ERROR_ACCESS_DENIED (the file is in a
// pending-delete state) or ERROR_SHARING_VIOLATION — NOT ERROR_ALREADY_EXISTS.
// acquireLock must read those as contention (fall through to steal+poll), not a
// fatal error; otherwise TestWithLockSerializes loses the race with "Access is
// denied." This pins isContendedCreateErr so that regression cannot return.
func TestIsContendedCreateErrWindows(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"already-exists sentinel", os.ErrExist, true},
		{"access-denied (pending-delete)", syscall.ERROR_ACCESS_DENIED, true},
		{"sharing-violation", errorSharingViolation, true},
		// The real shape os.OpenFile returns: errno wrapped in a *PathError.
		{"wrapped access-denied", &os.PathError{Op: "open", Path: "x.lock", Err: syscall.ERROR_ACCESS_DENIED}, true},
		{"wrapped sharing-violation", &os.PathError{Op: "open", Path: "x.lock", Err: errorSharingViolation}, true},
		// A genuine non-contention failure must still surface, not be retried.
		{"file-not-found is fatal", syscall.ERROR_FILE_NOT_FOUND, false},
		{"unrelated error is fatal", os.ErrInvalid, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isContendedCreateErr(tc.err); got != tc.want {
				t.Fatalf("isContendedCreateErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
