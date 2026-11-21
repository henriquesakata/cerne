package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// Session permission mode (F-0491): the hooks keep the last permission_mode
// Claude Code (or Codex) sent them, so the channel can tell whether an idle
// session takes a cross-session message or holds it. One file per session
// under <cacheDir>/session-modes/, outside sessions/: every file there is read
// as a binding, by this binary and by the release the hooks run (N-0134).
const sessionModesSubdir = "session-modes"

// SessionMode is what the hooks last saw of a session's main thread.
type SessionMode struct {
	Harness string `json:"harness"`
	Mode    string `json:"permission_mode,omitempty"`
}

// RecordSessionMode stores session sess's harness and permission mode. A
// subagent's hook (agent != "") records nothing: a subagent may run in a mode
// of its own, and the main thread's mode is what holds or delivers a message.
func RecordSessionMode(cacheDir, sess, agent, harness, mode string) error {
	if agent != "" {
		return nil
	}
	if err := safeSessionID(sess); err != nil {
		return err
	}
	raw, err := json.Marshal(SessionMode{Harness: harness, Mode: mode})
	if err != nil {
		return err
	}
	dir := filepath.Join(cacheDir, sessionModesSubdir)
	path := filepath.Join(dir, sess)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, raw) {
		return nil // every tool call passes here: no rewrite when nothing changed
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Parallel tool calls record at once: a reader never sees half a file.
	tmp, err := os.CreateTemp(dir, ".mode-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SessionModeOf is session sess's recorded mode; false when no hook recorded
// one, or the file does not parse.
func SessionModeOf(cacheDir, sess string) (SessionMode, bool) {
	if safeSessionID(sess) != nil {
		return SessionMode{}, false
	}
	raw, err := os.ReadFile(filepath.Join(cacheDir, sessionModesSubdir, sess))
	if err != nil {
		return SessionMode{}, false
	}
	var m SessionMode
	if json.Unmarshal(raw, &m) != nil {
		return SessionMode{}, false
	}
	return m, true
}
