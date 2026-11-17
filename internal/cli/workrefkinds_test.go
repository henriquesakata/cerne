package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0019: --ref takes W-, N- and RB- ids too, validated by existence.
func TestWorkStartRefKinds(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	t.Setenv("USER", "tester")
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "a note", State: domain.NoteActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "a runbook"}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "work", "start", "first investigation", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	for _, ref := range []string{"W-0001", "N-0001", "RB-0001"} {
		out, err := execCerne(t, "work", "start", "second "+ref, "--ref", ref, "--dir", projDir, "--force")
		if err != nil {
			t.Fatalf("--ref %s: %v\n%s", ref, err, out)
		}
	}
	shown, err := execCerne(t, "show", "W-0004", "--dir", projDir)
	if err != nil || !strings.Contains(shown, "refs: RB-0001") {
		t.Fatalf("show W-0004: %v\n%s", err, shown)
	}

	for _, ref := range []string{"W-0999", "N-0999", "RB-0999"} {
		out, err := execCerne(t, "work", "start", "ghost "+ref, "--ref", ref, "--dir", projDir, "--force")
		if err == nil || !strings.Contains(err.Error(), ref+" not found") {
			t.Fatalf("--ref %s: err = %v, want not found\n%s", ref, err, out)
		}
	}
	if works, _ := store.LoadAllWorks(repoDir); len(works) != 4 {
		t.Fatalf("works = %d, want 4 (nothing written by the refused starts)", len(works))
	}

	_, err = execCerne(t, "work", "start", "bad", "--ref", "X-0001", "--dir", projDir)
	if err == nil {
		t.Fatal("unknown prefix accepted")
	}
	for _, p := range []string{"F-", "E-", "T-", "B-", "D-", "W-", "N-", "RB-"} {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("message %q omits prefix %s", err, p)
		}
	}
}
