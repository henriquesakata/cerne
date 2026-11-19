package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
)

// The read side of the mirror (F-0465): what the Mirror tab shows. Nothing here
// writes to a tracker or to the backend.

// MirrorTrackerRun is what one sync did on one tracker: the cards it brought
// to their state, the ideas it imported, and the lines of what failed.
type MirrorTrackerRun struct {
	Name     string   `json:"name"`
	Synced   int      `json:"synced"`
	Imported int      `json:"imported"`
	Errors   []string `json:"errors"`
}

// MirrorLastSync is the record of the last sync on this computer.
type MirrorLastSync struct {
	At       time.Time          `json:"at"`
	Trackers []MirrorTrackerRun `json:"trackers"`
}

// lastSyncPath is the record's file, next to the project's index.db: cache, not
// backend, so a sync in a loop commits nothing.
func lastSyncPath(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), "mirror-sync.json")
}

// recordLastSync writes the record of rep.
func recordLastSync(repoDir string, rep MirrorSyncReport) error {
	data, err := json.Marshal(MirrorLastSync{At: time.Now().UTC(), Trackers: rep.Trackers})
	if err != nil {
		return err
	}
	p := lastSyncPath(repoDir)
	return project.WriteFileAtomic(filepath.Dir(p), filepath.Base(p), data)
}

// LastMirrorSync reads the record; nil when there is none or it is unreadable.
func LastMirrorSync(repoDir string) *MirrorLastSync {
	b, err := os.ReadFile(lastSyncPath(repoDir))
	if err != nil {
		return nil
	}
	var l MirrorLastSync
	if json.Unmarshal(b, &l) != nil {
		return nil
	}
	return &l
}
