package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoProjectHead is the sentinel for LoadProjectHead on a project that has no
// .docs/.projecthead sidecar yet (never synced, or a pre-F-0113 clone).
var ErrNoProjectHead = errors.New("no project head")

// projectHeadFilename is the sidecar recording the user project's git HEAD at
// the last docs sync (pull/push). The drift-suspect lint rule (F-0113) diffs
// this baseline against the current HEAD to find code that changed under a
// module whose features are all settled. It lives under the gitignored .docs/
// (per-worktree, never committed), like the session snapshot sidecar.
const projectHeadFilename = ".projecthead"

// SaveProjectHead writes <projDir>/.docs/.projecthead (SHA + newline),
// creating .docs/ when needed.
func SaveProjectHead(projDir, sha string) error {
	dir := manifestDir(projDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dir, projectHeadFilename, []byte(sha+"\n"))
}

// LoadProjectHead reads <projDir>/.docs/.projecthead. ErrNoProjectHead when the
// sidecar is absent.
func LoadProjectHead(projDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(manifestDir(projDir), projectHeadFilename))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoProjectHead
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
