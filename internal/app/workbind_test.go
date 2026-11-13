package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func bindTestWork(t *testing.T, repoDir, id string, state domain.WorkState) {
	t.Helper()
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if err := store.SaveWork(repoDir, store.Work{
		ID: id, Title: "atividade " + id, State: state, Actor: "agent",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveWork(%s): %v", id, err)
	}
}

// TestBindSessionAndCurrent (F-0151 crit. 1/7): a bound session resolves to its
// id while the work is still active.
func TestBindSessionAndCurrent(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	if err := BindSession(cacheDir, "sess-abc123", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	id, ok, err := CurrentBinding(cacheDir, repoDir, "sess-abc123", "")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if !ok || id != "W-0001" {
		t.Fatalf("got (%q, %v), want (W-0001, true)", id, ok)
	}
}

// TestCurrentBindingStaleWork (crit. 5 self-heal): a binding pointing at a
// terminal work reads as absent, and the stale file is removed.
func TestCurrentBindingStaleWork(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0002", domain.WorkDone)
	if err := BindSession(cacheDir, "sess-x", "W-0002"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	_, ok, err := CurrentBinding(cacheDir, repoDir, "sess-x", "")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if ok {
		t.Fatal("stale binding resolved as live, want absent")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "sessions", "sess-x")); !os.IsNotExist(err) {
		t.Fatalf("stale binding file not removed: %v", err)
	}
}

// TestCurrentBindingAbsent: an unbound session reads as absent without error.
func TestCurrentBindingAbsent(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	_, ok, err := CurrentBinding(cacheDir, repoDir, "sess-none", "")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if ok {
		t.Fatal("unbound session resolved as live")
	}
}

// TestCurrentBindingInDevelopmentFeature (crit. 1): `cerne feature start` binds
// an F- id; it stays live while the feature is in_development.
func TestCurrentBindingInDevelopmentFeature(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if err := store.SaveFeature(repoDir, store.Feature{
		ID: "F-0001", Title: "feature viva", State: domain.StateInDevelopment,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := BindSession(cacheDir, "sess-f", "F-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	id, ok, err := CurrentBinding(cacheDir, repoDir, "sess-f", "")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if !ok || id != "F-0001" {
		t.Fatalf("got (%q, %v), want (F-0001, true)", id, ok)
	}
}

// TestClearBindings (crit. 5): closing a work removes every session binding
// pointing at it, leaving other bindings intact.
func TestClearBindings(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0003", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0004", domain.WorkActive)
	for sess, id := range map[string]string{"s1": "W-0003", "s2": "W-0003", "s3": "W-0004"} {
		if err := BindSession(cacheDir, sess, id); err != nil {
			t.Fatalf("BindSession(%s): %v", sess, err)
		}
	}
	if err := ClearBindings(cacheDir, "W-0003"); err != nil {
		t.Fatalf("ClearBindings: %v", err)
	}
	if _, ok, _ := CurrentBinding(cacheDir, repoDir, "s1", ""); ok {
		t.Fatal("s1 still bound after clear")
	}
	if _, ok, _ := CurrentBinding(cacheDir, repoDir, "s2", ""); ok {
		t.Fatal("s2 still bound after clear")
	}
	if id, ok, _ := CurrentBinding(cacheDir, repoDir, "s3", ""); !ok || id != "W-0004" {
		t.Fatalf("s3 = (%q, %v), want (W-0004, true)", id, ok)
	}
}

// TestBindSessionRejectsUnsafeID: a session id that could escape the sessions
// dir (path separators, dot-dot) is refused.
func TestBindSessionRejectsUnsafeID(t *testing.T) {
	cacheDir := t.TempDir()
	for _, sess := range []string{"", "../evil", "a/b", `a\b`} {
		if err := BindSession(cacheDir, sess, "W-0001"); err == nil {
			t.Errorf("BindSession(%q): want error, got nil", sess)
		}
	}
}

// TestLiveBindings (F-0173 crit. 1/2 base): the project's live bindings are
// enumerated deduplicated; dead ones self-heal off disk; a missing sessions
// dir is an empty result, not an error.
func TestLiveBindings(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0002", domain.WorkDone)
	if err := BindSession(cacheDir, "sess-a", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if err := BindSession(cacheDir, "sess-b", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if err := BindSession(cacheDir, "sess-dead", "W-0002"); err != nil {
		t.Fatal(err)
	}

	got, err := LiveBindings(cacheDir, repoDir)
	if err != nil {
		t.Fatalf("LiveBindings: %v", err)
	}
	if len(got) != 1 || got[0] != "W-0001" {
		t.Fatalf("LiveBindings = %v, want [W-0001]", got)
	}
	// Dead binding self-healed off disk.
	if _, err := os.Stat(filepath.Join(cacheDir, "sessions", "sess-dead")); !os.IsNotExist(err) {
		t.Fatalf("dead binding file not removed: %v", err)
	}

	// Missing sessions dir: empty, no error.
	got, err = LiveBindings(t.TempDir(), repoDir)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty cache: got %v, %v", got, err)
	}
}

// readCostLedger returns the raw ledger and its lines decoded.
func readCostLedger(t *testing.T, cacheDir string) ([]byte, []cost.Bind) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cacheDir, "costbind.ndjson"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var binds []cost.Bind
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var b cost.Bind
		if err := json.Unmarshal(line, &b); err != nil {
			t.Fatalf("ledger line %q: %v", line, err)
		}
		binds = append(binds, b)
	}
	return raw, binds
}

// TestBindSessionAppendsCostLedger (F-0309 crit. 1): every bind appends one
// {session_id, entity_id, ts} line (plus harness) to <cache>/costbind.ndjson,
// never rewriting earlier lines.
func TestBindSessionAppendsCostLedger(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	cacheDir := t.TempDir()
	if err := BindSession(cacheDir, "sess-A", "W-0001"); err != nil {
		t.Fatal(err)
	}
	first, _ := readCostLedger(t, cacheDir)
	if err := BindSession(cacheDir, "sess-A", "F-0002"); err != nil {
		t.Fatal(err)
	}
	raw, binds := readCostLedger(t, cacheDir)
	if len(binds) != 2 {
		t.Fatalf("ledger has %d lines, want 2:\n%s", len(binds), raw)
	}
	for i, want := range []string{"W-0001", "F-0002"} {
		b := binds[i]
		if b.Session != "sess-A" || b.Entity != want || b.Harness != cost.HarnessClaudeCode || b.Agent != "" || b.TS.IsZero() {
			t.Fatalf("line %d = %+v, want sess-A/%s/claude-code, no agent, non-zero ts", i, b, want)
		}
	}
	if binds[1].TS.Before(binds[0].TS) {
		t.Fatalf("ts decreased: %v then %v", binds[0].TS, binds[1].TS)
	}
	if !bytes.HasPrefix(raw, first) {
		t.Fatalf("first line rewritten:\nbefore %q\nafter  %q", first, raw)
	}
}

// TestCostLedgerSurvivesClearAndSelfHeal (F-0309 crit. 2): ClearBindings and
// the self-heal of CurrentBinding/LiveBindings remove binding files only; the
// ledger stays byte-identical.
func TestCostLedgerSurvivesClearAndSelfHeal(t *testing.T) {
	cacheDir, repoDir := t.TempDir(), t.TempDir()
	bindTestWork(t, repoDir, "W-0001", domain.WorkActive)
	bindTestWork(t, repoDir, "W-0002", domain.WorkDone)
	if err := BindSession(cacheDir, "s1", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if err := BindSession(cacheDir, "s2", "W-0002"); err != nil {
		t.Fatal(err)
	}
	before, _ := readCostLedger(t, cacheDir)

	if err := ClearBindings(cacheDir, "W-0001"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := CurrentBinding(cacheDir, repoDir, "s2", ""); err != nil || ok {
		t.Fatalf("CurrentBinding(s2) = %v, %v, want stale", ok, err)
	}
	if _, err := LiveBindings(cacheDir, repoDir); err != nil {
		t.Fatal(err)
	}

	after, _ := readCostLedger(t, cacheDir)
	if !bytes.Equal(before, after) {
		t.Fatalf("ledger changed:\nbefore %q\nafter  %q", before, after)
	}
	entries, err := os.ReadDir(filepath.Join(cacheDir, "sessions"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("sessions/ = %v (%v), want empty", entries, err)
	}
}

func oldBinding(t *testing.T, cacheDir, sess, id string) (string, time.Time) {
	t.Helper()
	if err := BindSession(cacheDir, sess, id); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cacheDir, sessionsSubdir, sess)
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	return path, old
}

// TestTouchBindingRefreshesMtimeKeepsContent (F-0361 crit. 1).
func TestTouchBindingRefreshesMtimeKeepsContent(t *testing.T) {
	cacheDir := t.TempDir()
	path, _ := oldBinding(t, cacheDir, "sess-1", "W-0001")
	before := time.Now().Add(-2 * time.Second)
	if err := TouchBinding(cacheDir, "sess-1", ""); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mt := fi.ModTime(); mt.Before(before) || mt.After(time.Now()) {
		t.Fatalf("mtime %v not in [%v, now]", mt, before)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "W-0001\n" {
		t.Fatalf("content changed: %q", raw)
	}
}

// TestTouchBindingNoFileCreatesNothing (F-0361 crit. 2) and unsafe id errors.
func TestTouchBindingNoFileCreatesNothing(t *testing.T) {
	cacheDir := t.TempDir()
	if err := TouchBinding(cacheDir, "sess-none", ""); err != nil {
		t.Fatalf("missing binding must be a no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, sessionsSubdir)); !os.IsNotExist(err) {
		t.Fatalf("sessions dir created: %v", err)
	}
	if err := TouchBinding(cacheDir, "../x", ""); !errors.Is(err, ErrUnsafeSessionID) {
		t.Fatalf("err = %v, want ErrUnsafeSessionID", err)
	}
}

// TestSessionSignalsReadsMtimeSkipsDirs (F-0361 crit. 6): read-only listing.
func TestSessionSignalsReadsMtimeSkipsDirs(t *testing.T) {
	cacheDir := t.TempDir()
	_, old := oldBinding(t, cacheDir, "sess-1", "W-0001")
	if err := os.Mkdir(filepath.Join(cacheDir, sessionsSubdir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := SessionSignals(cacheDir)
	if len(got) != 1 || got[0].Session != "sess-1" || got[0].Entity != "W-0001" || !got[0].LastSignal.Equal(old) {
		t.Fatalf("got %+v", got)
	}
	if SessionSignals(t.TempDir()) != nil {
		t.Fatal("missing dir must give nil")
	}
}
