package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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
	id, ok, err := CurrentBinding(cacheDir, repoDir, "sess-abc123")
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
	_, ok, err := CurrentBinding(cacheDir, repoDir, "sess-x")
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
	_, ok, err := CurrentBinding(cacheDir, repoDir, "sess-none")
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
	id, ok, err := CurrentBinding(cacheDir, repoDir, "sess-f")
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
	if _, ok, _ := CurrentBinding(cacheDir, repoDir, "s1"); ok {
		t.Fatal("s1 still bound after clear")
	}
	if _, ok, _ := CurrentBinding(cacheDir, repoDir, "s2"); ok {
		t.Fatal("s2 still bound after clear")
	}
	if id, ok, _ := CurrentBinding(cacheDir, repoDir, "s3"); !ok || id != "W-0004" {
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
