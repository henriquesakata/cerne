package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
)

// Cursor session record (F-0513): Cursor's agent shells carry CURSOR_AGENT=1
// but no session id and no model (R-0037), while every hook payload Cursor
// sends names both. The hooks keep what each Cursor session last reported, one
// file per session under <cacheDir>/cursor-sessions/, so that a document
// written from a Cursor shell can name its model when only one is in play.
// Local cache only, outside sessions/: every file there reads as a binding.
const cursorSessionsSubdir = "cursor-sessions"

// cursorSession is one Cursor session's record.
type cursorSession struct {
	Model          string    `json:"model"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// RecordCursorSession stores what a Cursor hook payload said of session sess:
// its model and transcript path, stamped now. An empty one keeps the value
// already recorded (sessionStart names no transcript path). Atomic: parallel
// tool calls record at once, and a reader never sees half a file.
func RecordCursorSession(cacheDir, sess, model, transcriptPath string) error {
	if err := safeSessionID(sess); err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	var rec cursorSession
	if raw, err := os.ReadFile(filepath.Join(dir, sess+".json")); err == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	if model != "" {
		rec.Model = model
	}
	if transcriptPath != "" {
		rec.TranscriptPath = transcriptPath
	}
	rec.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return project.WriteFileAtomic(dir, sess+".json", raw)
}
