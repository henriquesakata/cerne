package cli

import (
	"errors"
	"os"
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

// TestApproveCheckIncompleteCard (F-0336 crit. 7): the check treats the card
// impediments as the agent's to fix — "not approvable yet", each with its
// command, no "for a human" label, a non-zero exit — and a clarification
// next to them is still left for the human.
func TestApproveCheckIncompleteCard(t *testing.T) {
	projDir, repoDir, id := incompleteCard(t)
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) {
		t.Fatalf("check err = %v, want errCheckImpediments\noutput: %s", err, out)
	}
	for _, want := range []string{
		"not approvable yet",
		app.ImpedimentUntagged + ": no tag", "cerne feature retag " + id + " --facet <f> --component <c>",
		app.ImpedimentCapabilityMissing + ": no capability", "cerne feature capability " + id + " C-",
		"yours to fix; the CLI approve refuses it without --force",
		"Complete the card with the commands above, then check again.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check output missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"for a human", "Next: ask a human", "'cerne push', and check again"} {
		if strings.Contains(out, gone) {
			t.Fatalf("check output carries %q:\n%s", gone, out)
		}
	}

	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real. [ESCLARECER: pública ou privada?]"))
	commitBackend(t, repoDir)
	out, _ = execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	for _, want := range []string{"not approvable yet", "pública ou privada? (for a human to answer", "Do not answer them yourself"} {
		if !strings.Contains(out, want) {
			t.Fatalf("check with a question missing %q:\n%s", want, out)
		}
	}
}

// TestReadinessLineNamesCardCodes (F-0336 crit. 7): the one-line readiness a
// spec write ends with names the card codes — never "no hard impediment".
func TestReadinessLineNamesCardCodes(t *testing.T) {
	projDir, _, _ := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	specFile := t.TempDir() + "/spec.md"
	if err := os.WriteFile(specFile, []byte(realDoc("F-0001", "Problema real descrito aqui.")), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "feature", "new", "Card sem tag", "--file", specFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new --file: %v\noutput: %s", err, out)
	}
	if want := "Approve readiness: not approvable — untagged, capability-missing; details: 'cerne feature approve F-0001 --check'"; !strings.Contains(out, want) {
		t.Fatalf("readiness line missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "no hard impediment") {
		t.Fatalf("readiness line reads the card impediments as a human's:\n%s", out)
	}
}

// TestClarificationRefusalNamesIncompleteCard (F-0336): the clarification
// refusal runs first, and its --force crosses the card check too — so with an
// incomplete card behind it, it says so instead of "the other approve gates
// still apply".
func TestClarificationRefusalNamesIncompleteCard(t *testing.T) {
	projDir, repoDir, id := incompleteCard(t)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real. [ESCLARECER: qual o limite?]"))
	commitBackend(t, repoDir)
	_, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	for _, want := range []string{"the card is incomplete too (untagged, capability-missing)", "cerne feature approve " + id + " --check", "--force skips this check and the card's"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal lacks %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "the other approve gates still apply") {
		t.Fatalf("refusal says --force leaves the card check standing:\n%v", err)
	}
}
