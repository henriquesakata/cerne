//go:build !windows

package gitback

import (
	"errors"
	"os"
)

// isContendedCreateErr reports whether err from the O_CREATE|O_EXCL create in
// acquireLock means "the lock is contended, retry" rather than "fatal".
//
// Off Windows the only contended outcome is os.ErrExist (the lock is held);
// everything else — including a genuine permission error on the lock directory —
// is fatal and surfaces unchanged. The transient access/sharing failures that
// the Windows build also tolerates do not occur here.
func isContendedCreateErr(err error) bool {
	return errors.Is(err, os.ErrExist)
}
