package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
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
	// Pending is the cerne command the session's shell is running, from
	// its preToolUse until its postToolUse (F-0513 crit. 6), since PendingAt.
	Pending   string    `json:"pending,omitempty"`
	PendingAt time.Time `json:"pending_at,omitzero"`
}

// CursorSeen is what one Cursor hook payload says of its session.
type CursorSeen struct {
	Model, TranscriptPath string
	// Pending is the cerne command a shell preToolUse is about to run; Ran
	// the command a shell postToolUse just ran, which is pending no more.
	Pending, Ran string
}

// maxCursorModel caps a recorded model name, in bytes.
const maxCursorModel = 128

// cleanCursorModel is a model name from a Cursor payload as cerne stores and
// reads it: no control, line-breaking or bidi character — the model rides on
// the backend event that `cerne show` prints in a teammate's terminal — and
// at most maxCursorModel bytes, cut on a rune boundary.
func cleanCursorModel(m string) string {
	m = strings.TrimSpace(strings.Map(func(r rune) rune {
		if nameBreaking(r) {
			return -1
		}
		return r
	}, m))
	if len(m) > maxCursorModel {
		m = strings.ToValidUTF8(m[:maxCursorModel], "")
	}
	return m
}

// maxCursorCommand caps a recorded pending command, in bytes.
const maxCursorCommand = 4 << 10

// cleanCursorCommand is a shell command from a Cursor payload as cerne stores
// it: no control or bidi character but the newline and tab that separate the
// shell's commands, at most maxCursorCommand bytes on a rune boundary. It is
// only ever matched against this process's own command line, never printed.
func cleanCursorCommand(c string) string {
	c = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && nameBreaking(r) {
			return -1
		}
		return r
	}, c))
	if len(c) > maxCursorCommand {
		c = strings.ToValidUTF8(c[:maxCursorCommand], "")
	}
	return c
}

// RecordCursorSession stores what a Cursor hook payload said of session sess,
// stamped now. An empty model or transcript path keeps the value already
// recorded (sessionStart names no transcript path). Atomic: parallel tool
// calls record at once, and a reader never sees half a file.
func RecordCursorSession(cacheDir, sess string, seen CursorSeen) error {
	if err := safeSessionID(sess); err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	var rec cursorSession
	if raw, err := os.ReadFile(filepath.Join(dir, sess+".json")); err == nil {
		_ = json.Unmarshal(raw, &rec)
	}
	if model := cleanCursorModel(seen.Model); model != "" {
		rec.Model = model
	}
	if seen.TranscriptPath != "" {
		rec.TranscriptPath = seen.TranscriptPath
	}
	if ran := cleanCursorCommand(seen.Ran); ran != "" && ran == rec.Pending {
		rec.Pending, rec.PendingAt = "", time.Time{}
	}
	if pending := cleanCursorCommand(seen.Pending); pending != "" {
		rec.Pending, rec.PendingAt = pending, time.Now().UTC()
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
// before any feature start binds). A record that does not parse, or is
// stamped in the future, is not in play; a file untouched for longer than
// the window is skipped unread.
func cursorSessions(cacheDir string, now time.Time) map[string]cursorSession {
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	entries, _ := os.ReadDir(dir)
	recent := map[string]cursorSession{}
	for _, e := range entries {
		sess, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || safeSessionID(sess) != nil {
			continue // a write's temp file, or nothing of ours
		}
		if info, err := e.Info(); err != nil || now.Sub(info.ModTime()) > cursorRecent {
			continue
		}
		var rec cursorSession
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		// A minute of clock skew still counts as now.
		if err != nil || json.Unmarshal(raw, &rec) != nil || now.Sub(rec.UpdatedAt) > cursorRecent || rec.UpdatedAt.After(now.Add(time.Minute)) {
			continue
		}
		rec.Model = cleanCursorModel(rec.Model) // a record written by hand, or by an older build
		recent[sess] = rec
	}
	return recent
}

// cursorPendingWriter is the one session in play whose pending shell command
// is this process's cerne call, args being its os.Args[1:] (F-0513 crit. 6):
// the rule cost.AuthorModel applies to Claude Code's pending tool_use. A
// pending command older than cursorRecent counts for nothing; "" when no
// session, or more than one, runs it.
func cursorPendingWriter(sessions map[string]cursorSession, args []string, now time.Time) string {
	var match []string
	for sess, rec := range sessions {
		if rec.Pending != "" && now.Sub(rec.PendingAt) <= cursorRecent && cost.Runs(rec.Pending, args) {
			match = append(match, sess)
		}
	}
	if len(match) != 1 {
		return ""
	}
	return match[0]
}

// cursorOwnSession is the session Cursor names in this shell's
// CURSOR_CONVERSATION_ID when its record is fresh (F-0525 crit. 2): the shell's
// own word, so it decides before any heuristic.
func cursorOwnSession(sessions map[string]cursorSession) (string, bool) {
	id := os.Getenv("CURSOR_CONVERSATION_ID")
	if id == "" || safeSessionID(id) != nil {
		return "", false
	}
	_, ok := sessions[id]
	return id, ok
}

// cursorAuthor is the writer of a document from a Cursor agent shell (F-0513),
// args being this process's os.Args[1:]: provider cursor, with the model of
// the session CURSOR_CONVERSATION_ID names when its record is fresh (F-0525,
// no model when that record has none yet), else the model of the one session
// whose pending shell command this is (criterion 6), else the model every
// Cursor session in play reported, and none otherwise.
func cursorAuthor(cacheDir string, args []string) DocAuthor {
	now := time.Now()
	sessions := cursorSessions(cacheDir, now)
	if own, ok := cursorOwnSession(sessions); ok {
		return DocAuthor{Provider: "cursor", Model: sessions[own].Model, Source: "hook"}
	}
	if w := cursorPendingWriter(sessions, args, now); sessions[w].Model != "" {
		return DocAuthor{Provider: "cursor", Model: sessions[w].Model, Source: "hook"}
	}
	a := DocAuthor{Provider: "cursor", Source: "hook"}
	for _, rec := range sessions {
		if rec.Model == "" || (a.Model != "" && rec.Model != a.Model) {
			return DocAuthor{Provider: "cursor", Source: "hook"}
		}
		a.Model = rec.Model
	}
	return a
}

// cursorWriterSession is the session a Cursor shell's write is marked on
// (F-0513), args being this process's os.Args[1:]: the session named by
// CURSOR_CONVERSATION_ID when it has a fresh record (F-0525), else the one
// whose pending shell command this is (criterion 6), else the only Cursor
// session in play, bound or not. "" when none of these is single.
func cursorWriterSession(cacheDir string, args []string) string {
	now := time.Now()
	sessions := cursorSessions(cacheDir, now)
	if own, ok := cursorOwnSession(sessions); ok {
		return own
	}
	if w := cursorPendingWriter(sessions, args, now); w != "" {
		return w
	}
	if len(sessions) != 1 {
		return ""
	}
	for sess := range sessions {
		return sess
	}
	return ""
}
