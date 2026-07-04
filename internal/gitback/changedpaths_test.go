package gitback

import (
	"errors"
	"reflect"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// commitAll stages every path in files (write + add) and commits, returning the
// new HEAD sha.
func commitAll(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for name, content := range files {
		writeFile(t, dir, name, content)
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	h, err := wt.Commit("change", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return h.String()
}

func TestChangedPaths(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.go": "1", "b.go": "1"})
	from := headOf(t, dir)
	// Modify a.go, leave b.go, add c.go.
	to := commitAll(t, dir, map[string]string{"a.go": "2", "c.go": "1"})

	got, err := ChangedPaths(dir, from, to)
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	want := []string{"a.go", "c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedPaths = %v, want %v", got, want)
	}
}

func TestChangedPathsNoBaseline(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.go": "1"})
	to := headOf(t, dir)

	if _, err := ChangedPaths(dir, "", to); !errors.Is(err, ErrNoBaseline) {
		t.Fatalf("empty from: err = %v, want ErrNoBaseline", err)
	}
	if _, err := ChangedPaths(dir, "0000000000000000000000000000000000000000", to); !errors.Is(err, ErrNoBaseline) {
		t.Fatalf("unknown from: err = %v, want ErrNoBaseline", err)
	}
}
