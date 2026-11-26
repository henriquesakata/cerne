package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// cursorRecent is how long a Cursor session's record keeps it in play: a
// session no hook has heard from in 12 h is not the one writing.
const cursorRecent = 12 * time.Hour

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

// cursorSessions are the Cursor sessions in play for the project at now:
// every one recorded within cursorRecent, bound or not — the shell names no
// session, and an unbound one may be the writer (feature new --file runs
// before any feature start binds). A record that does not parse is not in
// play.
func cursorSessions(cacheDir string, now time.Time) map[string]cursorSession {
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	entries, _ := os.ReadDir(dir)
	recent := map[string]cursorSession{}
	for _, e := range entries {
		sess, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || safeSessionID(sess) != nil {
			continue // a write's temp file, or nothing of ours
		}
		var rec cursorSession
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || json.Unmarshal(raw, &rec) != nil || now.Sub(rec.UpdatedAt) > cursorRecent {
			continue
		}
		recent[sess] = rec
	}
	return recent
}

// cursorBound reports a live binding of Cursor session sess (a session at
// work under a W- or an F-).
func cursorBound(cacheDir, sess string) bool {
	id, err := os.ReadFile(filepath.Join(cacheDir, sessionsSubdir, sess))
	return err == nil && bindingLive(filepath.Join(cacheDir, "repo"), strings.TrimSpace(string(id)))
}

// cursorAuthor is the writer of a document from a Cursor agent shell (F-0513):
// provider cursor, with the model the hooks recorded when every Cursor session
// in play reported the same one, and none otherwise — the shell names no
// session, so two concurrent sessions on different models are never told
// apart by a guess (cost.AuthorModel's rule).
func cursorAuthor(cacheDir string) DocAuthor {
	a := DocAuthor{Provider: "cursor", Source: "hook"}
	for _, rec := range cursorSessions(cacheDir, time.Now()) {
		if rec.Model == "" || (a.Model != "" && rec.Model != a.Model) {
			return DocAuthor{Provider: "cursor", Source: "hook"}
		}
		a.Model = rec.Model
	}
	return a
}

// boundCursorSession is the session a Cursor shell's write is marked on
// (F-0513): the only Cursor session in play, when it has a live binding. ""
// when another session, bound or not, is in play too — either may be the
// writer — or the only one is not bound.
func boundCursorSession(cacheDir string) string {
	sessions := cursorSessions(cacheDir, time.Now())
	if len(sessions) != 1 {
		return ""
	}
	for sess := range sessions {
		if cursorBound(cacheDir, sess) {
			return sess
		}
	}
	return ""
}
