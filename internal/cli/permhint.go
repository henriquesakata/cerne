package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/llm"
)

// cacheRefreshError explains why touching the cache clone at repoDir failed.
// A permission failure (typically an agent sandbox that only lets the project
// dir and /tmp be written) names the folders to open and never advises
// deleting anything; any other failure keeps the "delete to re-clone" advice.
func cacheRefreshError(what, repoDir, backend string, err error) error {
	inCodex := os.Getenv("CODEX_THREAD_ID") != ""
	inCursor := os.Getenv("CURSOR_AGENT") == "1" // F-0428's agent-shell marker
	// Inside a sandbox a cut network can surface as EPERM: only a folder that
	// really refuses writes gets the permission message (F-0484, F-0512).
	if errors.Is(err, fs.ErrPermission) && (!inCodex && !inCursor || errors.Is(probeCacheDir(repoDir), fs.ErrPermission)) {
		return notWritableError(repoDir, backend, err)
	}
	// The sandbox cut the network; deleting the cache will not help.
	if inCodex {
		return fmt.Errorf("%s at %s failed (%s): %w", what, repoDir, llm.CodexSandboxHint, err)
	}
	if inCursor {
		hint := cursorSandboxHint
		if gitback.IsLocalPath(backend) { // no host for --network to allow
			hint = cursorLocalBackendHint
		}
		return fmt.Errorf("%s at %s failed (%s): %w", what, repoDir, hint, err)
	}
	return fmt.Errorf("%s at %s failed (delete that directory and re-run the command to force a fresh clone): %w", what, repoDir, err)
}

// cursorSandboxHint (F-0512): Cursor's sandbox denies the network unless
// .cursor/sandbox.json allows the host.
const cursorSandboxHint = "the Cursor sandbox may be cutting the network; to allow the backend's host for this project, run: cerne hook install --provider cursor --network"

// cursorLocalBackendHint: with a local backend the sandbox blocks a folder, not
// a host.
const cursorLocalBackendHint = "the Cursor sandbox may be blocking the local backend folder; to add it to .cursor/sandbox.json, run: cerne hook install --provider cursor"

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
	if errors.Is(probeCacheDir(repoDir), fs.ErrPermission) {
		return notWritableError(repoDir, backend, err)
	}
	return err
}

// probeCacheDir tries to create a file in the cache dir (parent of the clone);
// nil means it accepts writes.
func probeCacheDir(repoDir string) error {
	f, err := os.CreateTemp(filepath.Dir(repoDir), ".cerne-write-probe-*")
	if err == nil {
		f.Close()
		os.Remove(f.Name())
	}
	return err
}
