package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchUpdatesTrackingHead(t *testing.T) {
	origin, clone1, sha := fixture(t)
	got, err := TrackingHeadSHA(clone1)
	if err != nil {
		t.Fatalf("TrackingHeadSHA: %v", err)
	}
	if got != sha {
		t.Fatalf("tracking head after clone = %q, want %q", got, sha)
	}

	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, clone2, "n.txt", "x")
	sha2, err := CommitAndPush(clone2, "advance")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	// Before fetch, clone1's tracking ref is stale.
	if got, _ = TrackingHeadSHA(clone1); got != sha {
		t.Fatalf("pre-fetch tracking = %q, want stale %q", got, sha)
	}
	if err := Fetch(clone1); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got, err = TrackingHeadSHA(clone1); err != nil || got != sha2 {
		t.Fatalf("post-fetch tracking = %q (err %v), want %q", got, err, sha2)
	}
	// Fetch must not move local HEAD.
	if head, _ := HeadSHA(clone1); head != sha {
		t.Fatalf("HEAD moved on fetch: %q, want %q", head, sha)
	}
}

func TestFileBlobAt(t *testing.T) {
	_, clone1, sha := fixture(t)
	data, ok, err := FileBlobAt(clone1, sha, "hello.txt")
	if err != nil || !ok {
		t.Fatalf("FileBlobAt hello.txt: ok=%v err=%v", ok, err)
	}
	if string(data) != "hello" {
		t.Fatalf("blob = %q, want hello", data)
	}
	if _, ok, err := FileBlobAt(clone1, sha, "missing.txt"); err != nil || ok {
		t.Fatalf("FileBlobAt missing: ok=%v err=%v, want absent", ok, err)
	}
}

func TestResetHard(t *testing.T) {
	_, clone1, sha := fixture(t)
	writeFile(t, clone1, "hello.txt", "changed") // modify tracked
	writeFile(t, clone1, "untracked.txt", "u")   // untracked add

	if err := ResetHard(clone1, sha); err != nil {
		t.Fatalf("ResetHard: %v", err)
	}
	// Tracked file is restored to the target commit.
	if data, _ := os.ReadFile(filepath.Join(clone1, "hello.txt")); string(data) != "hello" {
		t.Fatalf("hello.txt after reset = %q, want hello", data)
	}
	// go-git HardReset matches the tree exactly: untracked files are removed,
	// so the reconcile loop lands on a fully clean tree without a separate clean.
	if _, err := os.Stat(filepath.Join(clone1, "untracked.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untracked.txt should be removed by ResetHard, got err=%v", err)
	}
}

func TestWorktreeChanges(t *testing.T) {
	_, clone1, _ := fixture(t)
	writeFile(t, clone1, "keep.txt", "k")
	if _, err := CommitAll(clone1, "add keep"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	writeFile(t, clone1, "hello.txt", "changed") // modify tracked
	writeFile(t, clone1, "new.txt", "n")         // untracked add
	if err := os.Remove(filepath.Join(clone1, "keep.txt")); err != nil {
		t.Fatalf("rm keep.txt: %v", err)
	}

	changes, err := WorktreeChanges(clone1)
	if err != nil {
		t.Fatalf("WorktreeChanges: %v", err)
	}
	got := map[string]bool{}
	for _, c := range changes {
		got[c.Path] = c.Deleted
	}
	if d, ok := got["hello.txt"]; !ok || d {
		t.Fatalf("hello.txt: present=%v deleted=%v, want modify", ok, d)
	}
	if d, ok := got["new.txt"]; !ok || d {
		t.Fatalf("new.txt: present=%v deleted=%v, want add", ok, d)
	}
	if d, ok := got["keep.txt"]; !ok || !d {
		t.Fatalf("keep.txt: present=%v deleted=%v, want deletion", ok, d)
	}
}

func TestCommitAllDoesNotPush(t *testing.T) {
	_, clone1, sha := fixture(t)
	writeFile(t, clone1, "local.txt", "l")
	local, err := CommitAll(clone1, "local only")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if local == sha {
		t.Fatal("CommitAll should create a new commit")
	}
	if rh, err := RemoteHeadSHA(clone1); err != nil || rh != sha {
		t.Fatalf("remote head = %q (err %v), want unchanged %q (CommitAll must not push)", rh, err, sha)
	}
}

func TestPushNonFastForwardClassified(t *testing.T) {
	origin, clone1, _ := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, clone2, "theirs.txt", "t")
	if _, err := CommitAndPush(clone2, "advance"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}
	// clone1 commits on the stale base, then pushes: the remote advanced.
	writeFile(t, clone1, "mine.txt", "m")
	if _, err := CommitAll(clone1, "mine"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	err := Push(clone1)
	if !IsNonFastForward(err) {
		t.Fatalf("Push err = %v, want classified non-fast-forward", err)
	}
	_ = origin
}

func TestIsNonFastForwardClassification(t *testing.T) {
	if IsNonFastForward(nil) {
		t.Fatal("nil err should not be non-fast-forward")
	}
	if IsNonFastForward(errors.New("some other failure")) {
		t.Fatal("unrelated error should not be non-fast-forward")
	}
}
