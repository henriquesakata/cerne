package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
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
	case "decision":
		err = store.SaveDecision(other, store.Decision{ID: id, Title: title, State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now})
	case "reference":
		err = store.SaveReference(other, store.Reference{ID: id, Title: title, State: domain.ReferenceCurrent, CreatedAt: now, UpdatedAt: now})
	case "runbook":
		err = store.SaveRunbook(other, store.Runbook{ID: id, Title: title, State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now})
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

// F-0356 crit. 3: the redone mutate runs its guards on the new tip, so the
// refusal comes from the guard (not from a ConflictError) and nothing is
// minted. The parent guard is reachable too: the other session takes the same
// id AND closes the parent, which a mint never edits (so the conflict stays on
// created paths and the redo reaches validateParent, feature.go:255).
func TestMintRemintRunsGuardsOnTip(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		setup func(t *testing.T, origin string)
		want  error
	}{
		{"duplicate title", []string{"feature", "new", "Gamma", "--context", "x", "--component", "test"},
			func(t *testing.T, origin string) { publishFromOtherSession(t, origin, "feature", "F-0002", "Gamma") },
			app.ErrDuplicateTitle},
		{"closed parent", []string{"feature", "new", "Delta", "--context", "x", "--component", "test", "--parent", "F-0001"},
			func(t *testing.T, origin string) {
				other := filepath.Join(t.TempDir(), "other")
				if err := gitback.Clone(origin, other); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				for _, f := range []store.Feature{
					{ID: "F-0001", Title: "Alpha", State: domain.StateRejected, CreatedAt: now, UpdatedAt: now},
					{ID: "F-0002", Title: "theirs", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now},
				} {
					if err := store.SaveFeature(other, f); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := gitback.CommitAndPush(other, "theirs F-0002, F-0001 closed"); err != nil {
					t.Fatal(err)
				}
			},
			app.ErrParentTerminal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir, origin, _ := setupProject(t)
			newFeature(t, projDir, "Alpha", 1)
			c.setup(t, origin)
			stdout, stderr, err := execCerneSplit(t, append(c.args, "--dir", projDir)...)
			var ce *app.ConflictError
			if errors.As(err, &ce) || !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want the guard's %v (not a ConflictError)\nstdout: %s\nstderr: %s", err, c.want, stdout, stderr)
			}
			verify := filepath.Join(t.TempDir(), "verify")
			if err := gitback.Clone(origin, verify); err != nil {
				t.Fatalf("Clone verify: %v", err)
			}
			if fs, err := store.LoadAllFeatures(verify); err != nil || len(fs) != 2 {
				t.Fatalf("features = %+v err=%v, want only F-0001 and theirs F-0002", fs, err)
			}
		})
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
		{"decision new", "decision", "D-0001", "D-0002", []string{"decision", "new", "Gamma decision", "--component", "test", "--file", noteBody(t)}},
		{"reference new", "reference", "R-0001", "R-0002", []string{"reference", "new", "Gamma ref", "--url", "https://example.com/g", "--component", "test", "--file", noteBody(t)}},
		{"runbook new", "runbook", "RB-0001", "RB-0002", []string{"runbook", "new", "Gamma runbook", "--file", noteBody(t)}},
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
