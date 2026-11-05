package cli

import (
	"errors"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestApproveCheckListsWithoutTransition (F-0308 crit. 3, 11): an agent — whom
// the human gate refuses the approve itself — rehearses it with --check: every
// impediment is listed (with the three literal bar forms when the bar is the
// problem), the exit code follows the list, and nothing moves: state, events
// and the origin HEAD stay put.
func TestApproveCheckListsWithoutTransition(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature a conferir", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real. [ESCLARECER: qual o limite?]\n")
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")

	head, _ := gitback.HeadSHA(origin)
	events, _ := store.ReadEvents(repoDir)
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errNotApprovable) {
		t.Fatalf("check err = %v, want errNotApprovable\noutput: %s", err, out)
	}
	for _, want := range []string{
		"bar-missing", `no "Barra:" line`,
		"open-clarification", "qual o limite?", "--force",
		"Barra: <referência nomeada", "Barra: não aplicável (", "Barra: dispensada por decisão humana (",
		"cerne push",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check output missing %q:\n%s", want, out)
		}
	}
	assertUntouched := func() {
		t.Helper()
		if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
			t.Fatalf("state = %s after --check, want candidate", f.State)
		}
		if now, _ := gitback.HeadSHA(origin); now != head {
			t.Fatalf("origin HEAD moved %s → %s on --check", head, now)
		}
		if evs, _ := store.ReadEvents(repoDir); len(evs) != len(events) {
			t.Fatalf("events %d → %d on --check", len(events), len(evs))
		}
	}
	assertUntouched()

	// A valid bar and no marker: approvable, exit zero, the bar it would accept
	// is shown — and still nothing moves.
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	head, _ = gitback.HeadSHA(origin)
	events, _ = store.ReadEvents(repoDir)
	out, err = execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil {
		t.Fatalf("check on an approvable spec: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"approvable", "Barra: fixture de teste — referência nomeada", "ask a human"} {
		if !strings.Contains(out, want) {
			t.Fatalf("approvable check output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bar-missing") {
		t.Fatalf("approvable check still lists impediments:\n%s", out)
	}
	assertUntouched()
}

// --check is an approve rehearsal only: the other transitions do not take it.
func TestCheckFlagOnlyOnApprove(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature qualquer", 1)
	if _, err := execCerne(t, "feature", "start", id, "--check", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("start --check err = %v, want unknown flag", err)
	}
}

// TestApproveConfirmationPrintsBar (F-0308 crit. 9): the approve confirmation
// names the bar the human just accepted.
func TestApproveConfirmationPrintsBar(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature com barra", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("approve: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Accepted bar: Barra: fixture de teste — referência nomeada") {
		t.Fatalf("approve confirmation does not name the accepted bar:\n%s", out)
	}
}
