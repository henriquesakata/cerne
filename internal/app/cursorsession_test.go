package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
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
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Model: "grok-4.7"}); err != nil {
		t.Fatal(err)
	}
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{TranscriptPath: "/home/u/.cursor/projects/p/agent-transcripts/c1/c1.jsonl"}); err != nil {
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
		if err := RecordCursorSession(cacheDir, sess, CursorSeen{Model: "grok-4.7"}); !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("RecordCursorSession(%q) = %v, want ErrUnsafeSessionID", sess, err)
		}
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("cache dir = %v, want nothing written", entries)
	}
}

// TestCursorSessionCleansModel (F-0513 crit. 1): the model rides on the
// backend event that `cerne show` prints in a teammate's terminal, so an
// escape sequence or an over-long value from a payload is cleaned when it is
// stored, and again when a record is read back.
func TestCursorSessionCleansModel(t *testing.T) {
	cacheDir := t.TempDir()
	long := strings.Repeat("é", 100) // 200 bytes
	for _, tc := range []struct{ in, want string }{
		{"grok\x1b[31m-4.7\n‮", "grok[31m-4.7"},
		{long, strings.Repeat("é", 64)},
	} {
		if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Model: tc.in}); err != nil {
			t.Fatal(err)
		}
		if rec, _ := readCursorSession(t, cacheDir, "c1"); rec.Model != tc.want {
			t.Errorf("stored model = %q, want %q", rec.Model, tc.want)
		}
	}
	raw, _ := json.Marshal(cursorSession{Model: "gpt\x1b]8;;x\x07-6" + long, UpdatedAt: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(cacheDir, cursorSessionsSubdir, "c1.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got := cursorSessions(cacheDir, time.Now())["c1"].Model
	if strings.ContainsFunc(got, unicode.IsControl) || len(got) > 128 || !strings.HasPrefix(got, "gpt]8;;x-6é") {
		t.Fatalf("read-back model = %q, want no control character and at most 128 bytes", got)
	}
}

// TestCursorSessionPending (F-0513 crit. 6): the cerne command a session's
// shell is about to run is recorded with its time, until the postToolUse of
// that same command clears it; it is cleaned like the model (no control
// character but the shell's newline and tab, at most 4 KiB) and never read
// as anything but a command to match.
func TestCursorSessionPending(t *testing.T) {
	cacheDir := t.TempDir()
	before := time.Now().Add(-time.Second)
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Model: "grok-4.7", Pending: "cerne push"}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := readCursorSession(t, cacheDir, "c1"); rec.Pending != "cerne push" || rec.PendingAt.Before(before) || rec.Model != "grok-4.7" {
		t.Fatalf("record = %+v, want cerne push pending since now", rec)
	}
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Ran: "ls"}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := readCursorSession(t, cacheDir, "c1"); rec.Pending != "cerne push" {
		t.Fatalf("after another command ran: pending = %q, want cerne push still", rec.Pending)
	}
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Ran: "cerne push"}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := readCursorSession(t, cacheDir, "c1"); rec.Pending != "" || !rec.PendingAt.IsZero() {
		t.Fatalf("after cerne push ran: record = %+v, want nothing pending", rec)
	}
	long := "cerne push\x1b[31m‮\ncerne pull " + strings.Repeat("é", 3000)
	if err := RecordCursorSession(cacheDir, "c1", CursorSeen{Pending: long}); err != nil {
		t.Fatal(err)
	}
	rec, _ := readCursorSession(t, cacheDir, "c1")
	if !strings.HasPrefix(rec.Pending, "cerne push[31m\ncerne pull é") || len(rec.Pending) > 4096 || !utf8.ValidString(rec.Pending) {
		t.Fatalf("pending = %q (%d bytes), want no escape or bidi, the newline kept, at most 4096 bytes", rec.Pending[:40], len(rec.Pending))
	}
}

// TestCursorSessionsFreshness (F-0513 crit. 2): a record stamped in the
// future is stale, not recent forever; a file untouched for longer than the
// window is skipped by its mtime, whatever its content says, so a document
// write does not read every record ever written.
func TestCursorSessionsFreshness(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Now()
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for sess, at := range map[string]time.Time{"cfut": now.Add(30 * 24 * time.Hour), "cold": now.Add(-time.Minute), "cnow": now.Add(-time.Minute)} {
		raw, _ := json.Marshal(cursorSession{Model: "m-" + sess, UpdatedAt: at.UTC()})
		if err := os.WriteFile(filepath.Join(dir, sess+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-13 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "cold.json"), old, old); err != nil {
		t.Fatal(err)
	}
	got := cursorSessions(cacheDir, now)
	if _, ok := got["cnow"]; len(got) != 1 || !ok {
		t.Fatalf("sessions in play = %v, want only cnow", got)
	}
}
