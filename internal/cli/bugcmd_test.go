package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// T-0006 criterion 1: `bug new` mints B-0001 on its own sequence with the
// three-section report and the pre-filled bar line; a human at the terminal
// gets it already approved.
func TestBugNewMintsBPrefix(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "bug", "new", "Salvar quebra", "--context", "O salvar falha", "--dir", projDir)
	if err != nil {
		t.Fatalf("bug new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "B-0001 created (approved)") || !strings.Contains(out, "cerne feature start B-0001") {
		t.Fatalf("output = %q, want the creation line and the start hint", out)
	}
	f, err := store.LoadFeature(repoDir, "B-0001")
	if err != nil || f.State != domain.StateApproved || f.Origin != "human" {
		t.Fatalf("LoadFeature = %+v, %v; want B-0001 approved human", f, err)
	}
	raw, err := os.ReadFile(app.FeatureDocPath(repoDir, "B-0001", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	spec := string(raw)
	for _, want := range []string{"## Contexto\nO salvar falha", "## Como reproduzir", "## Esperado × obtido", "Barra: não aplicável (correção com teste de regressão)"} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec missing %q:\n%s", want, spec)
		}
	}
}

// An agent session without --on-behalf asked for the bug itself: candidate.
func TestBugNewAgentBornCandidate(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "bug", "new", "Achado pelo agente", "--context", "ctx", "--origin", "agent", "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\noutput: %s", err, out)
	}
	f, _ := store.LoadFeature(repoDir, "B-0001")
	if f.State != domain.StateCandidate || f.Origin != "agent" {
		t.Fatalf("bug = %+v, want candidate agent", f)
	}
	if out, err := execCerne(t, "bug", "new", "Relatado", "--context", "ctx2", "--on-behalf", "salvar quebrou", "--dir", projDir); err != nil {
		t.Fatalf("bug new --on-behalf: %v\noutput: %s", err, out)
	}
	g, _ := store.LoadFeature(repoDir, "B-0002")
	if g.State != domain.StateApproved || g.Origin != "human" {
		t.Fatalf("bug = %+v, want approved human", g)
	}
}

// A --file report without a valid bar line gets the pre-filled one; a report
// that carries its own valid bar is left as written.
func TestBugNewFileBar(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, repoDir := setupProject(t)
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	spec := func(id string) string {
		raw, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	noBar := write("a.md", "## Contexto\nfalha\n\n## Como reproduzir\n1. x\n\n## Esperado × obtido\ny\n")
	if out, err := execCerne(t, "bug", "new", "Sem barra", "--file", noBar, "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	if !strings.Contains(spec("B-0001"), "Barra: não aplicável (correção com teste de regressão)") {
		t.Errorf("bar line not appended:\n%s", spec("B-0001"))
	}
	own := "Barra: não aplicável (typo de texto) — padrão alternativo: revisão visual"
	withBar := write("b.md", "## Contexto\nfalha2\n\n"+own+"\n")
	if out, err := execCerne(t, "bug", "new", "Com barra", "--file", withBar, "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	got := spec("B-0002")
	if !strings.Contains(got, own) || strings.Count(got, "Barra:") != 1 {
		t.Errorf("own bar must stay alone:\n%s", got)
	}
}

// T-0006 criterion 7: accept stays human for a bug of human origin — an agent
// is refused with ErrHumanGate and the state is untouched; a human reaches done.
func TestAcceptBugStaysHuman(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "bug", "new", "Salvar quebra", "--context", "falha", "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "start", "B-0001", "--dir", projDir); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, "B-0001", "result.md", realDoc("B-0001", "Corrigido com teste de regressão."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "complete", "B-0001", "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "accept", "B-0001", "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
		t.Fatalf("accept by an agent err = %v, want ErrHumanGate", err)
	}
	if f, _ := store.LoadFeature(repoDir, "B-0001"); f.State != domain.StateImplemented {
		t.Fatalf("state after refused accept = %s, want implemented", f.State)
	}
	t.Setenv("CERNE_ACTOR", "")
	if out, err := execCerne(t, "feature", "accept", "B-0001", "--dir", projDir); err != nil {
		t.Fatalf("accept by a human: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, "B-0001"); f.State != domain.StateDone {
		t.Fatalf("state = %s, want done", f.State)
	}
}

func TestBugNewRequiresContext(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "bug", "new", "Sem relato", "--dir", projDir); err == nil {
		t.Fatal("want the context gate for bug new")
	}
}

// B-0003: the agent route fills every report section from flags, like the UI
// form; an omitted optional field keeps its template comment; --steps and
// --expected cannot ride along with --file.
func TestBugNewStepsExpectedFlags(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	projDir, _, repoDir := setupProject(t)
	spec := func(id string) string {
		raw, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if out, err := execCerne(t, "bug", "new", "Tudo", "--context", "ctx", "--steps", "1. abrir", "--expected", "salva × falha", "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	for _, want := range []string{"## Contexto\nctx\n", "## Como reproduzir\n1. abrir\n", "## Esperado × obtido\nsalva × falha\n", "Barra: não aplicável (correção com teste de regressão)"} {
		if !strings.Contains(spec("B-0001"), want) {
			t.Errorf("spec missing %q:\n%s", want, spec("B-0001"))
		}
	}
	if out, err := execCerne(t, "bug", "new", "So passos", "--context", "ctx", "--steps", "1. abrir", "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	if s := spec("B-0002"); !strings.Contains(s, "## Como reproduzir\n1. abrir\n") || !strings.Contains(s, "## Esperado × obtido\n<!--") {
		t.Errorf("only --steps:\n%s", s)
	}
	if out, err := execCerne(t, "bug", "new", "So esperado", "--context", "ctx", "--expected", "deveria salvar", "--dir", projDir); err != nil {
		t.Fatalf("bug new: %v\n%s", err, out)
	}
	if s := spec("B-0003"); !strings.Contains(s, "## Esperado × obtido\ndeveria salvar\n") || !strings.Contains(s, "## Como reproduzir\n<!--") {
		t.Errorf("only --expected:\n%s", s)
	}
	f := filepath.Join(t.TempDir(), "r.md")
	if err := os.WriteFile(f, []byte("## Contexto\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "bug", "new", "Conflito", "--file", f, "--steps", "s", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("--file with --steps: err = %v, out = %s; want mutually exclusive", err, out)
	}
	if _, lerr := store.LoadFeature(repoDir, "B-0004"); lerr == nil {
		t.Error("a refused bug new must not write a card")
	}
}
