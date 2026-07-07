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
	if err := Pull(repoDir); err != nil {
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
