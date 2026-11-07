package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestApproveCheckListsWithoutTransition (F-0308 crit. 3, 11): an agent — whom
// the human gate refuses the approve itself — rehearses it with --check: every
// impediment is listed (with the three literal bar forms when the bar is the
// problem), the exit code follows the list, and nothing moves: state, events,
// the origin HEAD, the docs.lock bytes and the .docs/ tree stay put.
func TestApproveCheckListsWithoutTransition(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature a conferir", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real. [ESCLARECER: qual o limite?]\n")
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")
	// A materialized .docs/ to watch, besides the backend.
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}

	head, _ := gitback.HeadSHA(origin)
	events, _ := store.ReadEvents(repoDir)
	lock, docs := projectFiles(t, projDir)
	if len(docs) == 0 {
		t.Fatal("pull materialized no .docs/ file to watch")
	}
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) {
		t.Fatalf("check err = %v, want errCheckImpediments\noutput: %s", err, out)
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
		nowLock, nowDocs := projectFiles(t, projDir)
		if nowLock != lock {
			t.Fatalf("docs.lock changed on --check:\n%s\n→\n%s", lock, nowLock)
		}
		if !reflect.DeepEqual(nowDocs, docs) {
			t.Fatalf(".docs/ changed on --check: %d files → %d", len(docs), len(nowDocs))
		}
	}
	assertUntouched()

	// A valid bar and no marker: approvable, exit zero, the bar it would accept
	// is shown — and still nothing moves.
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	head, _ = gitback.HeadSHA(origin)
	events, _ = store.ReadEvents(repoDir)
	lock, docs = projectFiles(t, projDir)
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

// projectFiles returns the docs.lock bytes and every .docs/ file (path →
// content) of the user's project.
func projectFiles(t *testing.T, projDir string) (string, map[string]string) {
	t.Helper()
	lock, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	root := filepath.Join(projDir, ".docs")
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		docs[strings.TrimPrefix(p, root)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(lock), docs
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

// TestApproveCheckSoftOnly (F-0308, F-0175): open clarifications are questions
// for the human, never for the agent to answer — with only those left, the
// check says there is no hard impediment, tells to leave the questions for
// the human with the approve request, and keeps the non-zero exit (the exit
// follows the list, crit. 3); the edit-and-push footer is for hard ones only.
func TestApproveCheckSoftOnly(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature com pergunta", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real. [ESCLARECER: pública ou privada?]"))
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) {
		t.Fatalf("check err = %v, want errCheckImpediments\noutput: %s", err, out)
	}
	for _, want := range []string{
		"no hard impediment",
		"1 open clarification(s) for a human to answer",
		"the CLI approve refuses them without --force; the UI does not",
		"pública ou privada?",
		"Barra: fixture de teste — referência nomeada",
		"leave the questions for the human, with the approve request: 'cerne clarify list " + id + "'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("soft-only check output missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"not approvable", "'cerne push', and check again"} {
		if strings.Contains(out, gone) {
			t.Fatalf("soft-only check output carries %q:\n%s", gone, out)
		}
	}
}

// TestClarificationRefusalNamesHardImpediments (F-0308): the CLI refuses open
// clarifications before the core checks the spec, so a spec with no bar AND a
// question used to be refused on the question alone — answered, the approve
// then failed on the bar. The refusal now names the hard impediments waiting
// behind it; with none, it stays as it was.
func TestClarificationRefusalNamesHardImpediments(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature sem barra e com pergunta", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real. [ESCLARECER: qual o limite?]\n")
	commitBackend(t, repoDir)

	_, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	for _, want := range []string{"bar-missing", "cerne feature approve " + id + " --check", "answering them (or --force) is not enough"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("clarification refusal does not name the hard impediment (%q):\n%v", want, err)
		}
	}
	// F-0330 crit. 4: the refusal says --force does not cross the hard ones — true.
	if out, err := execCerne(t, "feature", "approve", id, "--force", "--dir", projDir); err == nil {
		t.Fatalf("approve --force crossed a hard impediment:\n%s", out)
	}
	if f, err := store.LoadFeature(repoDir, id); err != nil || f.State != domain.StateCandidate {
		t.Fatalf("after a refused --force: %+v, %v; want candidate", f, err)
	}

	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real. [ESCLARECER: qual o limite?]"))
	commitBackend(t, repoDir)
	_, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
	if !errors.Is(err, app.ErrOpenClarifications) || strings.Contains(err.Error(), "--check") {
		t.Fatalf("with no hard impediment the refusal must stay the clarification one: %v", err)
	}
}
