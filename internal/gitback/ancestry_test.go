package gitback

import (
	"path/filepath"
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
