package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestNoteNewGist (F-0459 criterion 1): --gist stores the normalized line on
// the entity.
func TestNoteNewGist(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "Tag goes on the merge commit", "--gist", "  Tag the merge\ncommit, never the pin.  ", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new --gist: %v\n%s", err, out)
	}
	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Tag the merge commit, never the pin."; n.Gist != want {
		t.Fatalf("gist = %q, want %q", n.Gist, want)
	}
}

// TestNoteNewGistRefused (F-0459 criterion 1): an empty gist or one past the
// cap fails with its sentinel and writes nothing — no note, no body.
func TestNoteNewGistRefused(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cases := []struct {
		gist string
		want error
	}{
		{" \n ", domain.ErrEmptyGist},
		{strings.Repeat("a", domain.GistMax+1), domain.ErrGistTooLong},
	}
	for _, c := range cases {
		out, err := execCerne(t, "note", "new", "T", "--gist", c.gist, "--dir", projDir)
		if !errors.Is(err, c.want) {
			t.Fatalf("gist %q: err = %v, want %v\n%s", c.gist, err, c.want, out)
		}
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("refused note new wrote %d note(s)", len(notes))
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "notes", "N-0001.md")); err == nil {
		t.Fatal("refused note new wrote a body")
	}
}
