package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoLock is the sentinel for a LoadLock call with no docs.lock in dir.
var ErrNoLock = errors.New("no docs.lock")

const lockFilename = "docs.lock"

// Lock is the persisted form of docs.lock (JSON, committed by the user).
type Lock struct {
	Version      int               `json:"version"`      // 1
	Backend      string            `json:"backend"`      // git URL or local path
	Snapshot     string            `json:"snapshot"`     // backend HEAD SHA
	Materialized map[string]string `json:"materialized"` // rel path (slash-separated) -> sha256 hex
}

// SaveLock writes <dir>/docs.lock as pretty-printed JSON (2-space indent,
// trailing newline).
func SaveLock(dir string, l Lock) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, lockFilename, append(data, '\n'))
}

// LoadLock reads <dir>/docs.lock; ErrNoLock when missing.
func LoadLock(dir string) (Lock, error) {
	var l Lock
	data, err := os.ReadFile(filepath.Join(dir, lockFilename))
	if errors.Is(err, os.ErrNotExist) {
		return l, fmt.Errorf("%w: %s", ErrNoLock, dir)
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return l, err
	}
	return l, nil
}
