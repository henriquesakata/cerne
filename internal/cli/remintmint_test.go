package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// publishFromOtherSession takes id for another session's entity of the kind
// (feature/note/work) AFTER the local snapshot, so the local clone mints the
// same id on its stale base.
func publishFromOtherSession(t *testing.T, origin, kind, id, title string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	now := time.Now().UTC()
	var err error
	switch kind {
	case "feature":
		err = store.SaveFeature(other, store.Feature{ID: id, Title: title, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now})
	case "note":
		err = store.SaveNote(other, store.Note{ID: id, Title: title, State: domain.NoteActive, CreatedAt: now, UpdatedAt: now})
	case "work":
		err = store.SaveWork(other, store.Work{ID: id, Title: title, State: domain.WorkActive, Actor: "other@host", CreatedAt: now, UpdatedAt: now})
	}
	if err != nil {
		t.Fatalf("save %s: %v", kind, err)
	}
	if _, err := gitback.CommitAndPush(other, "theirs "+id); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

// F-0356 crit. 3: the redone mutate runs its duplicate-title guard on the new
// tip, so a title the other session just registered is refused, not re-minted.
func TestMintRemintRunsDuplicateGuardOnTip(t *testing.T) {
	projDir, origin, _ := setupProject(t)
	newFeature(t, projDir, "Alpha", 1)
	publishFromOtherSession(t, origin, "feature", "F-0002", "Gamma")
	stdout, stderr, err := execCerneSplit(t, "feature", "new", "Gamma", "--context", "x", "--component", "test", "--dir", projDir)
	if err == nil {
		t.Fatalf("want the duplicate guard to refuse\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone verify: %v", err)
	}
	if fs, err := store.LoadAllFeatures(verify); err != nil || len(fs) != 2 {
		t.Fatalf("features = %+v err=%v, want only F-0001 and theirs F-0002", fs, err)
	}
}

func noteBody(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte("um fato durável sobre o projeto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// F-0356 crit. 1+2: a mint whose id another session took after our snapshot is
// redone on the new tip; the confirmation names the final id and stderr says
// which id was taken.
func TestMintRemintsIDTakenUpstream(t *testing.T) {
	cases := []struct {
		name, kind, oldID, newID string
		args                     []string
	}{
		{"feature new", "feature", "F-0002", "F-0003", []string{"feature", "new", "Gamma", "--context", "x", "--component", "test"}},
		{"note new", "note", "N-0001", "N-0002", []string{"note", "new", "Gamma note", "--facet", "code", "--informs", "F-0001", "--component", "test", "--file", noteBody(t)}},
		{"work start", "work", "W-0001", "W-0002", []string{"work", "start", "Gamma work"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir, origin, _ := setupProject(t)
			newFeature(t, projDir, "Alpha", 1)
			first := c.oldID
			if c.kind == "feature" {
				first = "F-0002"
			}
			publishFromOtherSession(t, origin, c.kind, first, "theirs")
			stdout, stderr, err := execCerneSplit(t, append(c.args, "--dir", projDir)...)
			if err != nil {
				t.Fatalf("%v: %v\nstdout: %s\nstderr: %s", c.args, err, stdout, stderr)
			}
			if want := "cerne: " + c.oldID + " was taken upstream; registered as " + c.newID + "\n"; !strings.Contains(stderr, want) {
				t.Fatalf("stderr missing %q:\n%s", want, stderr)
			}
			if !strings.Contains(stdout, c.newID) || strings.Contains(stdout, c.oldID) {
				t.Fatalf("stdout must cite only the final id %s:\n%s", c.newID, stdout)
			}
		})
	}
}
