package cli

import (
	"encoding/json"
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

// TestNoteGist (F-0459 criterion 2): replaces the gist with a note.gist
// event, an unchanged gist commits nothing, --clear removes it, bad input
// leaves the note untouched, and a retired note is history.
func TestNoteGist(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--gist", "old line", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	out, err := execCerne(t, "note", "gist", "N-0001", "Run glab from the target repo's dir.", "--dir", projDir)
	if err != nil {
		t.Fatalf("note gist: %v\n%s", err, out)
	}
	if !strings.Contains(out, "N-0001 gist: Run glab from the target repo's dir.") {
		t.Fatalf("output: %q", out)
	}
	n, _ := store.LoadNote(repoDir, "N-0001")
	if n.Gist != "Run glab from the target repo's dir." {
		t.Fatalf("gist = %q", n.Gist)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "note.gist" || last.Payload["gist"] != n.Gist {
		t.Fatalf("last event = %+v, want note.gist", last)
	}

	head := gitHead(t, repoDir)
	if _, err := execCerne(t, "note", "gist", "N-0001", "Run glab from the target  repo's dir.", "--dir", projDir); err != nil {
		t.Fatalf("unchanged gist: %v", err)
	}
	if gitHead(t, repoDir) != head {
		t.Fatal("an unchanged gist committed")
	}

	if _, err := execCerne(t, "note", "gist", "N-0001", strings.Repeat("a", domain.GistMax+1), "--dir", projDir); !errors.Is(err, domain.ErrGistTooLong) {
		t.Fatalf("too long: err = %v", err)
	}
	if _, err := execCerne(t, "note", "gist", "N-0001", "--dir", projDir); !errors.Is(err, domain.ErrEmptyGist) {
		t.Fatalf("note gist with no text and no --clear: err = %v, want ErrEmptyGist", err)
	}
	if _, err := execCerne(t, "note", "gist", "N-0001", " ", "--dir", projDir); !errors.Is(err, domain.ErrEmptyGist) {
		t.Fatalf("note gist with blank text: err = %v, want ErrEmptyGist", err)
	}
	if _, err := execCerne(t, "note", "gist", "N-0001", "x", "--clear", "--dir", projDir); err == nil {
		t.Fatal("note gist with text and --clear must fail")
	}
	if n, _ = store.LoadNote(repoDir, "N-0001"); n.Gist != "Run glab from the target repo's dir." {
		t.Fatalf("a refused note gist mutated the note: %q", n.Gist)
	}

	if _, err := execCerne(t, "note", "gist", "N-0001", "--dir", projDir, "--", "-R alone does not override origin."); err != nil {
		t.Fatalf("gist after --: %v", err)
	}
	if n, _ = store.LoadNote(repoDir, "N-0001"); n.Gist != "-R alone does not override origin." {
		t.Fatalf("gist after -- = %q", n.Gist)
	}

	if out, err := execCerne(t, "note", "gist", "N-0001", "--clear", "--dir", projDir); err != nil || !strings.Contains(out, "N-0001 has no gist.") {
		t.Fatalf("--clear: %v\n%s", err, out)
	}
	if n, _ = store.LoadNote(repoDir, "N-0001"); n.Gist != "" {
		t.Fatalf("--clear left %q", n.Gist)
	}

	if _, err := execCerne(t, "note", "retire", "N-0001", "--reason", "done", "--dir", projDir); err != nil {
		t.Fatalf("note retire: %v", err)
	}
	if _, err := execCerne(t, "note", "gist", "N-0001", "x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "is not active") {
		t.Fatalf("note gist on a retired note: err = %v, want the not-active refusal", err)
	}
}

// TestShowNoteGist (F-0459 criterion 3): the gist line in show and note show,
// and the gist field in show --json.
func TestShowNoteGist(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--when", "bash:git tag", "--gist", "Tag the merge commit.", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	for _, args := range [][]string{{"show", "N-0001"}, {"note", "show", "N-0001"}} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "\nwhen: bash:git tag\ngist: Tag the merge commit.\n") {
			t.Fatalf("%v output lacks the gist line after when:\n%s", args, out)
		}
	}
	out, err := execCerne(t, "show", "N-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v", err)
	}
	var probe struct {
		Entity struct {
			Gist string `json:"gist"`
		} `json:"entity"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil || probe.Entity.Gist != "Tag the merge commit." {
		t.Fatalf("show --json entity.gist = %q (%v):\n%s", probe.Entity.Gist, err, out)
	}
}
