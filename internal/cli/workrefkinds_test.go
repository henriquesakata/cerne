package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	snap := func() string {
		c, err := exec.Command("git", "-C", repoDir, "rev-list", "--count", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		var size int64
		files, _ := filepath.Glob(filepath.Join(repoDir, "events", "*.ndjson"))
		for _, f := range files {
			st, _ := os.Stat(f)
			size += st.Size()
		}
		return fmt.Sprintf("%s commits, %d event bytes", strings.TrimSpace(string(c)), size)
	}
	before := snap()
	kinds := map[string]string{"W-0999": "work", "N-0999": "note", "RB-0999": "runbook"}
	for _, ref := range []string{"W-0999", "N-0999", "RB-0999"} {
		out, err := execCerne(t, "work", "start", "ghost "+ref, "--ref", ref, "--dir", projDir, "--force")
		if err == nil || !strings.Contains(err.Error(), kinds[ref]+" "+ref+" not found in the backend") {
			t.Fatalf("--ref %s: err = %v, want not found\n%s", ref, err, out)
		}
	}
	if works, _ := store.LoadAllWorks(repoDir); len(works) != 4 {
		t.Fatalf("works = %d, want 4 (nothing written by the refused starts)", len(works))
	}

	if after := snap(); after != before {
		t.Fatalf("refused starts wrote to the backend: %s -> %s", before, after)
	}

	// show lists W- and N- refs too, text and --json (4 = ref W-0001? see below).
	for id, ref := range map[string]string{"W-0002": "W-0001", "W-0003": "N-0001"} {
		shown, err := execCerne(t, "show", id, "--dir", projDir)
		if err != nil || !strings.Contains(shown, "refs: "+ref) {
			t.Fatalf("show %s: %v\n%s", id, err, shown)
		}
		js, err := execCerne(t, "show", id, "--json", "--dir", projDir)
		if err != nil || !strings.Contains(js, `"`+ref+`"`) {
			t.Fatalf("show --json %s: %v\n%s", id, err, js)
		}
	}

	// Pins: no case normalization; CSV with spaces works.
	_, err = execCerne(t, "work", "start", "lower", "--ref", "w-0001", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "RB-") {
		t.Fatalf("lowercase w-0001: err = %v, want the unknown-prefix message", err)
	}
	if out, err := execCerne(t, "work", "start", "csv", "--ref", "N-0001, W-0001", "--dir", projDir, "--force"); err != nil {
		t.Fatalf("csv with spaces: %v\n%s", err, out)
	}
	if w, _ := store.LoadWork(repoDir, "W-0005"); len(w.Refs) != 2 || w.Refs[0] != "N-0001" || w.Refs[1] != "W-0001" {
		t.Fatalf("csv refs = %v", w.Refs)
	}

	// Handoff path: --from inherits the origin's refs, none of which can be
	// the origin itself (a work cannot reference its own id at creation).
	if out, err := execCerne(t, "work", "start", "continued", "--from", "W-0005", "--dir", projDir, "--force"); err != nil {
		t.Fatalf("--from: %v\n%s", err, out)
	}
	if w, _ := store.LoadWork(repoDir, "W-0006"); len(w.Refs) != 2 || w.Refs[0] != "N-0001" || w.Refs[1] != "W-0001" {
		t.Fatalf("inherited refs = %v", w.Refs)
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
