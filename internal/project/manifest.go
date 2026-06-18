package project

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// manifestFilename is the materialized-docs manifest sidecar. It lives under
// the gitignored .docs/ (per-worktree, never committed): a map of each
// materialized path (slash-separated, relative to .docs/) to its sha256 hex.
// Splitting it out of docs.lock keeps the committed file down to the snapshot
// pin, which is what makes docs.lock merge-conflict-free.
const manifestFilename = ".manifest.json"

// manifestDir is the .docs directory under projDir that holds the sidecar.
func manifestDir(projDir string) string {
	return filepath.Join(projDir, ".docs")
}

// SaveManifest writes <projDir>/.docs/.manifest.json as pretty-printed JSON
// (2-space indent, trailing newline), creating .docs/ when needed.
func SaveManifest(projDir string, m map[string]string) error {
	if m == nil {
		m = map[string]string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dir := manifestDir(projDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(dir, manifestFilename, append(data, '\n'))
}

// LoadManifest reads <projDir>/.docs/.manifest.json. When the sidecar is
// absent it falls back to the legacy `materialized` map a pre-0030 docs.lock
// still embeds (one-time migration: the next SaveManifest writes the sidecar
// and the next SaveLock drops the field). With neither present it returns an
// empty map.
func LoadManifest(projDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(manifestDir(projDir), manifestFilename))
	if err == nil {
		m := map[string]string{}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return legacyManifestFromLock(projDir)
}

// legacyManifestFromLock extracts the `materialized` map from a pre-0030
// docs.lock by raw JSON parse, so it stays decoupled from the Lock struct
// after the field is removed. A missing lock yields an empty map.
func legacyManifestFromLock(projDir string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(projDir, lockFilename))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var legacy struct {
		Materialized map[string]string `json:"materialized"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, err
	}
	if legacy.Materialized == nil {
		return map[string]string{}, nil
	}
	return legacy.Materialized, nil
}
