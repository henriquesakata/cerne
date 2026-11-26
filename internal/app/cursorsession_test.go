package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readCursorSession is the record of sess under cacheDir; false when absent.
func readCursorSession(t *testing.T, cacheDir, sess string) (cursorSession, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cacheDir, cursorSessionsSubdir, sess+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return cursorSession{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var rec cursorSession
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record %s: %v\n%s", sess, err, raw)
	}
	return rec, true
}

// TestCursorSessionRecord (F-0513 crit. 1): a Cursor session's record holds
// its model, transcript path and the time of the observation; a later payload
// without one of them keeps the recorded value.
func TestCursorSessionRecord(t *testing.T) {
	cacheDir := t.TempDir()
	before := time.Now().Add(-time.Second)
	if err := RecordCursorSession(cacheDir, "c1", "grok-4.7", ""); err != nil {
		t.Fatal(err)
	}
	if err := RecordCursorSession(cacheDir, "c1", "", "/home/u/.cursor/projects/p/agent-transcripts/c1/c1.jsonl"); err != nil {
		t.Fatal(err)
	}
	rec, ok := readCursorSession(t, cacheDir, "c1")
	if !ok || rec.Model != "grok-4.7" || rec.TranscriptPath != "/home/u/.cursor/projects/p/agent-transcripts/c1/c1.jsonl" || rec.UpdatedAt.Before(before) {
		t.Fatalf("record = %+v (found %v), want grok-4.7, the transcript path and a fresh updated_at", rec, ok)
	}
}

// TestCursorSessionRejectsUnsafeID (F-0513 crit. 1): a session id from the
// payload that could leave the directory is refused, and nothing is written.
func TestCursorSessionRejectsUnsafeID(t *testing.T) {
	cacheDir := t.TempDir()
	for _, sess := range []string{"", "../x", "a/b", `a\b`, ".."} {
		if err := RecordCursorSession(cacheDir, sess, "grok-4.7", ""); !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("RecordCursorSession(%q) = %v, want ErrUnsafeSessionID", sess, err)
		}
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("cache dir = %v, want nothing written", entries)
	}
}
