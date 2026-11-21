//go:build !windows

package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// socketTrust is why the inbox socket at path is not dialed (F-0491 review
// item 4), "" when it is: it must be a socket — not a symlink, not a file —
// owned by this user, in a directory owned by this user. The path comes from
// a registry file anyone with write access to it could plant.
func socketTrust(path string) string {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return WakeSocketMissing
	}
	if err != nil || info.Mode()&fs.ModeSocket == 0 || !ownedByMe(info) {
		return WakeUntrustedSocket
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || !ownedByMe(dir) {
		return WakeUntrustedSocket
	}
	return ""
}

func ownedByMe(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
