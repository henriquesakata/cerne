package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoSessionSnapshot is the sentinel for a LoadSessionSnapshot on a project
// that has no .docs/.snapshot sidecar yet (fresh clone/checkout).
var ErrNoSessionSnapshot = errors.New("no session snapshot")

// snapshotFilename is the session snapshot sidecar: the backend commit the
// local .docs/ was last synced to. It lives under the gitignored .docs/
// (per-worktree, never committed) so feature branches track their snapshot
// without touching the committed docs.lock pin — which, with pin_branch set,
// only advances on the default branch (F-0102).
const snapshotFilename = ".snapshot"

// SaveSessionSnapshot writes <projDir>/.docs/.snapshot (SHA + newline),
// creating .docs/ when needed.
func SaveSessionSnapshot(projDir, sha string) error {
	dir := manifestDir(projDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dir, snapshotFilename, []byte(sha+"\n"))
}

// LoadSessionSnapshot reads <projDir>/.docs/.snapshot. ErrNoSessionSnapshot
// when the sidecar is absent.
func LoadSessionSnapshot(projDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(manifestDir(projDir), snapshotFilename))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoSessionSnapshot
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
