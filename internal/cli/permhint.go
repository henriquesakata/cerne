package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/llm"
)

// cacheRefreshError explains why touching the cache clone at repoDir failed.
// A permission failure (typically an agent sandbox that only lets the project
// dir and /tmp be written) names the folders to open and never advises
// deleting anything; any other failure keeps the "delete to re-clone" advice.
func cacheRefreshError(what, repoDir, backend string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return notWritableError(repoDir, backend, err)
	}
	if os.Getenv("CODEX_THREAD_ID") != "" { // F-0484: the sandbox cut the network; deleting the cache will not help
		return fmt.Errorf("%s at %s failed (%s): %w", what, repoDir, llm.CodexSandboxHint, err)
	}
	return fmt.Errorf("%s at %s failed (delete that directory and re-run the command to force a fresh clone): %w", what, repoDir, err)
}

// notWritableError is the shared permission explanation: the cache dir (parent
// of the clone) and, when the backend is a local directory, the backend itself.
func notWritableError(repoDir, backend string, err error) error {
	dirs := filepath.Dir(repoDir)
	if st, serr := os.Stat(backend); serr == nil && st.IsDir() {
		dirs += " and " + backend
	}
	return fmt.Errorf("cerne cannot write to its cache: the folder does not accept writes (an agent sandbox may be blocking it). Make writable: %s: %w", dirs, err)
}

// unwritableCacheErr is for steps whose error is not a wrapped fs.ErrPermission
// (sqlite says "unable to open database file (14)"): when the cache dir really
// refuses writes, explain that instead; otherwise return err untouched.
func unwritableCacheErr(repoDir, backend string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrPermission) {
		return notWritableError(repoDir, backend, err)
	}
	f, perr := os.CreateTemp(filepath.Dir(repoDir), ".cerne-write-probe-*")
	if perr == nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if errors.Is(perr, fs.ErrPermission) {
		return notWritableError(repoDir, backend, err)
	}
	return err
}
