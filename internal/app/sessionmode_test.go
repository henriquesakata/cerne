package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSessionModeRecordsTheLastMode (F-0491 crit. 2): the hooks keep the last
// permission mode of the main thread, with its harness; a subagent's hook and
// an unusable session id record nothing.
func TestSessionModeRecordsTheLastMode(t *testing.T) {
	cache := t.TempDir()
	if _, ok := SessionModeOf(cache, "s1"); ok {
		t.Fatal("a session no hook saw has no mode")
	}
	if err := RecordSessionMode(cache, "s1", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	if got, ok := SessionModeOf(cache, "s1"); !ok || got != (SessionMode{Harness: "claude-code", Mode: "auto"}) {
		t.Fatalf("got %+v %v", got, ok)
	}
	if err := RecordSessionMode(cache, "s1", "", "claude-code", "bypassPermissions"); err != nil {
		t.Fatal(err)
	}
	if got, _ := SessionModeOf(cache, "s1"); got.Mode != "bypassPermissions" {
		t.Fatalf("the last mode wins, got %+v", got)
	}
	// A subagent may run in its own mode: the main thread's is what holds or
	// delivers a cross-session message.
	if err := RecordSessionMode(cache, "s1", "agent-1", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	if got, _ := SessionModeOf(cache, "s1"); got.Mode != "bypassPermissions" {
		t.Fatalf("a subagent overwrote the main thread's mode: %+v", got)
	}
	if err := RecordSessionMode(cache, "../x", "", "claude-code", "auto"); err == nil {
		t.Fatal("an unsafe session id must be refused")
	}
	// Never inside sessions/: every file there is read as a binding (N-0134).
	if _, err := os.Stat(filepath.Join(cache, sessionsSubdir)); !os.IsNotExist(err) {
		t.Fatalf("sessions/ touched: %v", err)
	}
}

// TestSessionModeDirIsPrivate (F-0491 review item 6): the mode files live in
// a directory only the user can read.
func TestSessionModeDirIsPrivate(t *testing.T) {
	cache := t.TempDir()
	if err := RecordSessionMode(cache, "s1", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(cache, sessionModesSubdir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("session-modes is %o, want 700", perm)
	}
}

// TestSessionModeTightensAnExistingDir (F-0491 security re-review): a
// session-modes directory an older build left 0755 ends up 0700.
func TestSessionModeTightensAnExistingDir(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, sessionModesSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RecordSessionMode(cache, "s1", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("session-modes is %o, want 700", perm)
	}
}
