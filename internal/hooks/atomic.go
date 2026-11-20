package hooks

import (
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to a temp file beside path and renames it over
// path, so a process reading the old file (a shell running a hook script) keeps
// the old inode and never sees a half-new one (F-0477). An existing file keeps
// its mode, a symlink is written through, and the temp file never outlives an error.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cerne-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	for _, e := range []error{werr, cerr, os.Chmod(tmp, mode)} {
		if e != nil {
			os.Remove(tmp)
			return e
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
