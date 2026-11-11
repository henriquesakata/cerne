package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// incompleteCard registers, in a project that curates a capability taxonomy, a
// candidate with a valid spec but no tag and no capability (F-0336) — the
// F-0335 shape: approvable on its spec alone, invisible to the filters.
func incompleteCard(t *testing.T) (projDir, repoDir, id string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	if out, err := execCerne(t, "feature", "new", "Card incompleto", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	id = "F-0001"
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	return projDir, repoDir, id
}

// TestApproveRefusesIncompleteCard (F-0336 crit. 5): without --force, the CLI
// approve of a card with no tag and no capability is refused with
// ErrIncompleteCard, naming each impediment with the command that fixes it,
// and nothing is written — a lowercase id included; --force crosses it.
func TestApproveRefusesIncompleteCard(t *testing.T) {
	projDir, repoDir, id := incompleteCard(t)
	for _, arg := range []string{id, strings.ToLower(id)} {
		_, err := execCerne(t, "feature", "approve", arg, "--dir", projDir)
		if !errors.Is(err, app.ErrIncompleteCard) {
			t.Fatalf("approve %s err = %v, want ErrIncompleteCard", arg, err)
		}
		for _, want := range []string{
			app.ImpedimentUntagged, "cerne feature retag " + id + " --facet <f> --component <c>",
			app.ImpedimentCapabilityMissing, "cerne feature capability " + id + " C-",
			"cerne feature approve " + id + "' again", "--force",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("refusal lacks %q:\n%v", want, err)
			}
		}
		if f, err := store.LoadFeature(repoDir, id); err != nil || f.State != domain.StateCandidate {
			t.Fatalf("after the refusal: %+v, %v; want candidate", f, err)
		}
	}
	if out, err := execCerne(t, "feature", "approve", id, "--force", "--dir", projDir); err != nil {
		t.Fatalf("approve --force: %v\noutput: %s", err, out)
	}
	if f, err := store.LoadFeature(repoDir, id); err != nil || f.State != domain.StateApproved {
		t.Fatalf("after --force: %+v, %v; want approved", f, err)
	}
}

