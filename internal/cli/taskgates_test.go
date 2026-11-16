package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// T-0007: the CLI side of the task gates — the cascade lines, the level
// advice and the task new hint.

const exemptTaskSpec = "---\ncerne_id: %s\n---\n\n# Passo\n\n**Nível:** padrão\n\nCorpo real do documento.\n\n## Verificação\n\nBarra: não aplicável (regra de gate) — padrão alternativo: testes\n"

// seedCard persists f with spec as its spec.md and commits the backend.
func seedCard(t *testing.T, repoDir string, f store.Feature, spec string) {
	t.Helper()
	if f.Title == "" {
		f.Title = "Card " + f.ID
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, f.ID, "spec.md", spec)
	commitBackend(t, repoDir)
}

// TestFeatureAcceptPrintsCascade (T-0007 crit. 3): the parent's accept lists
// each task it accepted along; --all-implemented does not trip over a task
// the cascade already accepted.
func TestFeatureAcceptPrintsCascade(t *testing.T) {
	for _, args := range [][]string{{"E-0001"}, {"--all-implemented", "--reason", "migração"}} {
		t.Run(args[0], func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			seedCard(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateImplemented}, realDoc("E-0001", "Épico."))
			seedCard(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented}, strings.ReplaceAll(exemptTaskSpec, "%s", "T-0002"))
			out, err := execCerne(t, append([]string{"feature", "accept"}, append(args, "--dir", projDir)...)...)
			if err != nil {
				t.Fatalf("accept: %v\n%s", err, out)
			}
			if !strings.Contains(out, "  ↳ T-0002: implemented → done along with E-0001\n") {
				t.Fatalf("accept output lacks the cascaded task:\n%s", out)
			}
			if f, _ := store.LoadFeature(repoDir, "T-0002"); f.State != domain.StateDone {
				t.Fatalf("T-0002 state = %s, want done", f.State)
			}
			if args[0] == "--all-implemented" && !strings.Contains(out, "2 feature(s) accepted.") {
				t.Fatalf("the bulk summary counts the cascaded task too:\n%s", out)
			}
		})
	}
}

// specFile writes body to a throwaway spec file and returns its path.
func specFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte(completeSpec(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTaskNewWarnsStandardWithoutBar (T-0007 crit. 6): a standard or advanced
// task declaring "não aplicável" gets the approve check's advice at birth,
// never a refusal; a light one does not.
func TestTaskNewWarnsStandardWithoutBar(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedCard(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateApproved}, realDoc("E-0001", "Épico."))
	out, err := execCerne(t, "task", "new", "Passo padrão", "--parent", "E-0001", "--file", specFile(t, strings.ReplaceAll(exemptTaskSpec, "%s", "T-0001")), "--dir", projDir)
	if err != nil {
		t.Fatalf("task new: %v\n%s", err, out)
	}
	if !strings.Contains(out, `standard-without-bar: level standard with "Barra: não aplicável"`) || !strings.Contains(out, "it never blocks") {
		t.Fatalf("task new output lacks the level advice:\n%s", out)
	}
	light := strings.Replace(strings.ReplaceAll(exemptTaskSpec, "%s", "T-0002"), "padrão", "leve", 1)
	out, err = execCerne(t, "task", "new", "Passo leve", "--parent", "E-0001", "--file", specFile(t, light), "--dir", projDir)
	if err != nil {
		t.Fatalf("task new: %v\n%s", err, out)
	}
	if strings.Contains(out, "standard-without-bar") {
		t.Fatalf("a light task with no bar gets no advice:\n%s", out)
	}
}

// TestStartTaskWarnsStandardWithoutBar (T-0007 crit. 6): the same advice at
// the task's start, which still proceeds.
func TestStartTaskWarnsStandardWithoutBar(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedCard(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateApproved}, realDoc("E-0001", "Épico."))
	seedCard(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateApproved}, strings.ReplaceAll(exemptTaskSpec, "%s", "T-0002"))
	out, err := execCerne(t, "feature", "start", "T-0002", "--dir", projDir)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if !strings.Contains(out, "standard-without-bar:") {
		t.Fatalf("start output lacks the level advice:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "T-0002"); f.State != domain.StateInDevelopment {
		t.Fatalf("T-0002 state = %s, want in_development: the advice never blocks", f.State)
	}
}

// TestFeatureNewParentCommittedHintsTask (T-0007 crit. 8): a feature born under
// a parent already approved (or beyond) points at 'cerne task new', the path
// born approved — a hint only, the feature is still a candidate F-; under a
// candidate parent there is no hint.
func TestFeatureNewParentCommittedHintsTask(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedCard(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateInDevelopment}, realDoc("E-0001", "Épico."))
	seedCard(t, repoDir, store.Feature{ID: "E-0002", State: domain.StateCandidate}, realDoc("E-0002", "Outro épico."))
	out, err := execCerne(t, "feature", "new", "Filha comprometida", "--parent", "E-0001", "--context", "ctx", "--component", "test", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if !strings.Contains(out, "E-0001 is already in_development: 'cerne task new \"<title>\" --parent E-0001' creates a task born approved") {
		t.Fatalf("feature new output lacks the task new hint:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("F-0001 state = %s, want candidate: the hint changes nothing", f.State)
	}
	out, err = execCerne(t, "feature", "new", "Filha livre", "--parent", "E-0002", "--context", "ctx", "--component", "test", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if strings.Contains(out, "cerne task new") {
		t.Fatalf("a candidate parent gets no hint:\n%s", out)
	}
}

// TestCompleteTaskNextStepNamesParentAccept (T-0007 crit. 3): a completed
// task under a parent is accepted along with it, so the Next step sends the
// human to the parent's accept, not to one accept per task.
func TestCompleteTaskNextStepNamesParentAccept(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedCard(t, repoDir, store.Feature{ID: "E-0001", State: domain.StateInDevelopment}, realDoc("E-0001", "Épico."))
	seedCard(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateInDevelopment}, strings.ReplaceAll(exemptTaskSpec, "%s", "T-0002"))
	writeDoc(t, repoDir, "T-0002", "result.md", "# R\n\n## Resultado\n\nEntregue.\n")
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "complete", "T-0002", "--no-stamp", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "  - human accept: T-0002 is accepted along with its parent: cerne feature accept E-0001\n") {
		t.Fatalf("complete output lacks the parent's accept:\n%s", out)
	}
}

// TestFeatureApprovePrintsCascade (T-0007 crit. 1): the parent's approve
// lists the child it approved along and the one it left, with why.
func TestFeatureApprovePrintsCascade(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	tags := func(f store.Feature) store.Feature { f.Components = []string{"test"}; return f }
	seedCard(t, repoDir, tags(store.Feature{ID: "E-0001", State: domain.StateCandidate}), realDoc("E-0001", "Épico."))
	seedCard(t, repoDir, tags(store.Feature{ID: "F-0002", Parent: "E-0001", State: domain.StateCandidate}), realDoc("F-0002", "Filha pronta."))
	seedCard(t, repoDir, store.Feature{ID: "F-0003", Parent: "E-0001", State: domain.StateCandidate}, realDoc("F-0003", "Filha sem tag."))
	out, err := execCerne(t, "feature", "approve", "E-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	for _, want := range []string{"  ↳ F-0002: candidate → approved along with E-0001\n", "  ↳ F-0003 stays in candidate: untagged"} {
		if !strings.Contains(out, want) {
			t.Errorf("approve output lacks %q:\n%s", want, out)
		}
	}
}
