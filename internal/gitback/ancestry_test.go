package gitback

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsAncestorLinear(t *testing.T) {
	_, clone1, a := fixture(t)
	writeFile(t, clone1, "b.txt", "b")
	b, err := CommitAndPush(clone1, "second")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	cases := []struct {
		name string
		x, y string
		want bool
	}{
		{"parent is ancestor of child", a, b, true},
		{"child is not ancestor of parent", b, a, false},
		{"commit is ancestor of itself", b, b, true},
	}
	for _, tc := range cases {
		got, err := IsAncestor(clone1, tc.x, tc.y)
		if err != nil {
			t.Fatalf("%s: IsAncestor: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: IsAncestor(%s,%s) = %v, want %v", tc.name, tc.x[:8], tc.y[:8], got, tc.want)
		}
	}
}

// AheadTouchesOnly gates pin publication (F-0180): true only when every
// commit in origin/<branch>..HEAD touches nothing but the given path.
func TestAheadTouchesOnly(t *testing.T) {
	_, clone1, _ := fixture(t)

	ok, err := AheadTouchesOnly(clone1, "main", "docs.lock")
	if err != nil {
		t.Fatalf("in sync: %v", err)
	}
	if !ok {
		t.Fatal("in sync with origin: want true (empty range)")
	}

	writeFile(t, clone1, "docs.lock", `{"v":1}`)
	if _, err := CommitAll(clone1, "pin 1"); err != nil {
		t.Fatalf("CommitAll pin 1: %v", err)
	}
	writeFile(t, clone1, "docs.lock", `{"v":2}`)
	if _, err := CommitAll(clone1, "pin 2"); err != nil {
		t.Fatalf("CommitAll pin 2: %v", err)
	}
	ok, err = AheadTouchesOnly(clone1, "main", "docs.lock")
	if err != nil {
		t.Fatalf("pin-only ahead: %v", err)
	}
	if !ok {
		t.Fatal("two pin-only commits ahead: want true")
	}

	writeFile(t, clone1, "work.txt", "user work")
	if _, err := CommitAll(clone1, "user commit"); err != nil {
		t.Fatalf("CommitAll user: %v", err)
	}
	ok, err = AheadTouchesOnly(clone1, "main", "docs.lock")
	if err != nil {
		t.Fatalf("mixed ahead: %v", err)
	}
	if ok {
		t.Fatal("non-pin commit ahead: want false")
	}
}

// IsAncestor on a SHA absent from the local clone must be classifiable as
// unknown-commit, so the lock merge driver (F-0180) can fetch and retry.
func TestIsUnknownCommit(t *testing.T) {
	_, clone1, a := fixture(t)
	unknown := strings.Repeat("d", 40)
	_, err := IsAncestor(clone1, unknown, a)
	if err == nil {
		t.Fatal("IsAncestor with unknown SHA: want error")
	}
	if !IsUnknownCommit(err) {
		t.Fatalf("IsUnknownCommit(%v) = false, want true", err)
	}
	if IsUnknownCommit(errors.New("some other failure")) {
		t.Fatal("IsUnknownCommit(unrelated error) = true, want false")
	}
}

// A repo with no origin/<branch> tracking ref must never be pushed: false, no error.
func TestAheadTouchesOnlyMissingTrackingRef(t *testing.T) {
	dir := t.TempDir()
	if err := InitBare(filepath.Join(dir, "unused.git")); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir := filepath.Join(dir, "repo")
	if err := Clone(filepath.Join(dir, "unused.git"), repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, repoDir, "a.txt", "a")
	if _, err := CommitAll(repoDir, "local only"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	ok, err := AheadTouchesOnly(repoDir, "main", "docs.lock")
	if err != nil {
		t.Fatalf("missing tracking ref: %v", err)
	}
	if ok {
		t.Fatal("no origin/main tracking ref: want false")
	}
}

// A diverged branch (tracking ref not an ancestor of HEAD) must not be pushed.
func TestAheadTouchesOnlyDiverged(t *testing.T) {
	origin, clone1, _ := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, clone2, "b.txt", "remote work")
	if _, err := CommitAndPush(clone2, "remote advance"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}
	writeFile(t, clone1, "docs.lock", `{"v":1}`)
	if _, err := CommitAll(clone1, "local pin"); err != nil {
		t.Fatalf("CommitAll clone1: %v", err)
	}
	if err := Fetch(clone1); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	ok, err := AheadTouchesOnly(clone1, "main", "docs.lock")
	if err != nil {
		t.Fatalf("diverged: %v", err)
	}
	if ok {
		t.Fatal("diverged from origin/main: want false")
	}
}

// Fetch must download a remote commit's objects so IsAncestor can resolve it,
// without moving the local worktree HEAD.
func TestFetchBringsObjectsWithoutMovingHead(t *testing.T) {
	origin, clone1, a := fixture(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeFile(t, clone2, "b.txt", "from clone2")
	b, err := CommitAndPush(clone2, "external")
	if err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}
	if err := Fetch(clone1); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	head, err := HeadSHA(clone1)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != a {
		t.Fatalf("HeadSHA after fetch = %q, want unchanged %q", head, a)
	}
	anc, err := IsAncestor(clone1, a, b)
	if err != nil {
		t.Fatalf("IsAncestor after fetch: %v", err)
	}
	if !anc {
		t.Fatalf("IsAncestor(%s,%s) = false, want true", a[:8], b[:8])
	}
}
