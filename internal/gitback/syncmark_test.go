package gitback

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// syncFixture: bare origin + clone with one pushed commit.
func syncFixture(t *testing.T) (origin, repoDir string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(root, "repo")
	if err := Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "seed.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return origin, repoDir
}

func withFakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Now()
	prev := syncNow
	syncNow = func() time.Time { return now }
	t.Cleanup(func() { syncNow = prev; resetSyncMarks() })
	resetSyncMarks()
	return &now
}

// TestFetchMarksSync pins criterion 4 (F-0156): a successful remote talk
// stamps the in-process sync mark; the window math honors the clock.
func TestFetchMarksSync(t *testing.T) {
	now := withFakeClock(t)
	_, repoDir := syncFixture(t)
	resetSyncMarks() // drop the fixture's CommitAndPush mark
	if SyncedWithin(repoDir, 30*time.Second) {
		t.Fatal("fresh fixture must not be marked before an explicit sync call in this test scope")
	}
	if err := Fetch(repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !SyncedWithin(repoDir, 30*time.Second) {
		t.Fatal("successful fetch must mark the sync")
	}
	*now = now.Add(31 * time.Second)
	if SyncedWithin(repoDir, 30*time.Second) {
		t.Fatal("mark must expire past the window")
	}
}

// TestPushAndPullMarkSync: the other two remote talks also stamp.
func TestPushAndPullMarkSync(t *testing.T) {
	withFakeClock(t)
	_, repoDir := syncFixture(t)
	resetSyncMarks() // drop the fixture's CommitAndPush mark
	if err := SafePull(repoDir); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !SyncedWithin(repoDir, time.Minute) {
		t.Fatal("successful pull must mark the sync")
	}
	resetSyncMarks()
	if err := os.WriteFile(filepath.Join(repoDir, "seed.md"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAndPush(repoDir, "again"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if !SyncedWithin(repoDir, time.Minute) {
		t.Fatal("successful push must mark the sync")
	}
}

// TestFailedFetchDoesNotMark: a broken remote leaves no mark.
func TestFailedFetchDoesNotMark(t *testing.T) {
	withFakeClock(t)
	origin, repoDir := syncFixture(t)
	resetSyncMarks()
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	if err := Fetch(repoDir); err == nil {
		t.Fatal("fetch against a removed origin must fail")
	}
	if SyncedWithin(repoDir, time.Minute) {
		t.Fatal("failed fetch must not mark the sync")
	}
}

// TestSyncMarkSurvivesProcessRestart pins criterion 4 (F-0200): every CLI
// command is a fresh process with an empty map, so an in-process-only mark can
// never elide. The mark therefore has an on-disk lastro: forgetting the map
// (what a new process sees) must still find the previous process's sync, and
// the window must still expire against the injected clock.
func TestSyncMarkSurvivesProcessRestart(t *testing.T) {
	now := withFakeClock(t)
	_, repoDir := syncFixture(t)
	resetSyncMarks()

	markSynced(repoDir)
	forgetSyncMemory() // a fresh process: empty map, stamp still on disk
	if !SyncedWithin(repoDir, time.Minute) {
		t.Fatal("a fresh process must inherit the previous process's sync mark")
	}

	*now = now.Add(61 * time.Second)
	if SyncedWithin(repoDir, time.Minute) {
		t.Fatal("the on-disk mark must expire past the window like the in-process one")
	}
}

// TestSyncStampIsInvisibleToWorktreeChanges pins criterion 5 (F-0200): the
// stamp lives inside .git/, never in the worktree. A stamp in the tree would
// make WorktreeChanges non-empty and so poison SafePull's own dirty guard —
// the mark would permanently convince SafePull that a synthesis is in flight.
func TestSyncStampIsInvisibleToWorktreeChanges(t *testing.T) {
	withFakeClock(t)
	_, repoDir := syncFixture(t)

	markSynced(repoDir)
	changes, err := WorktreeChanges(repoDir)
	if err != nil {
		t.Fatalf("WorktreeChanges: %v", err)
	}
	if len(changes) > 0 {
		t.Fatalf("the sync stamp dirtied the worktree: %v", changes)
	}
}

// TestResetSyncMarksClearsTheStamp: resetSyncMarks means "nothing ever synced",
// so it must drop the on-disk lastro too — otherwise every test that resets to
// simulate a cold start would silently inherit the fixture's push mark.
func TestResetSyncMarksClearsTheStamp(t *testing.T) {
	withFakeClock(t)
	_, repoDir := syncFixture(t)

	markSynced(repoDir)
	resetSyncMarks()
	forgetSyncMemory() // prove the answer comes from disk, not the map
	if SyncedWithin(repoDir, time.Minute) {
		t.Fatal("resetSyncMarks must remove the on-disk stamp, not just the map")
	}
}

// TestSyncMarksArePerRepo: one repo's mark says nothing about another.
func TestSyncMarksArePerRepo(t *testing.T) {
	withFakeClock(t)
	_, repoA := syncFixture(t)
	_, repoB := syncFixture(t)
	resetSyncMarks()
	if err := Fetch(repoA); err != nil {
		t.Fatal(err)
	}
	if !SyncedWithin(repoA, time.Minute) || SyncedWithin(repoB, time.Minute) {
		t.Fatal("marks must be per repoDir")
	}
}
