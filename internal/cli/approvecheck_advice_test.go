package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// adviceSpecDoc is realDoc (a named bar and a judge question) at level.
func adviceSpecDoc(id, level string) string {
	return realDoc(id, "**Nível:** "+level+" — porquê\n\n## Objetivo e critérios de aceitação\n\n1. um")
}

// TestApproveCheckAdvice (F-0390 crit. 1, 2, 6): on a light spec naming a bar
// the check prints an Advice section — the measured cost (not enough here,
// with no number) and the light-with-bar advice — after everything else, and
// still exits 0; an impediment keeps its own exit with the advice beside it.
func TestApproveCheckAdvice(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Leve com barra", 1)
	writeDoc(t, repoDir, id, "spec.md", adviceSpecDoc(id, "leve"))
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil {
		t.Fatalf("advice must not change the exit: %v\n%s", err, out)
	}
	head := "Advice (it never blocks the approve):\n"
	for _, want := range []string{
		head + "  - gauntlet-cost: Gauntlet cost measured in this project (critic + judge): not enough measured features yet (0 of 3 needed)\n",
		`  - light-with-bar: level light with a named bar: when the tests suffice, a light change can use "Barra: não aplicável (<por que os testes bastam>)"`,
		"is approvable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, head), strings.Index(out, "Next: "); i < 0 || j < i {
		t.Errorf("Advice must come before the Next line, after the rest:\n%s", out)
	}

	// An open clarification keeps the check's non-zero exit; the advice is shown too.
	writeDoc(t, repoDir, id, "spec.md", adviceSpecDoc(id, "leve")+"\n[ESCLARECER: qual formato?]\n")
	commitBackend(t, repoDir)
	out, err = execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) || !strings.Contains(out, head) {
		t.Fatalf("err = %v, want errCheckImpediments with the advice shown:\n%s", err, out)
	}
}

// TestApproveCheckAdviceLightMode (F-0390 crit. 6): a project in light mode
// gets no Advice section.
func TestApproveCheckAdviceLightMode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	setGauntlet(t, projDir, "off")
	id := newFeature(t, projDir, "Leve com barra", 1)
	writeDoc(t, repoDir, id, "spec.md", adviceSpecDoc(id, "leve"))
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || strings.Contains(out, "Advice") {
		t.Fatalf("light mode: err = %v, want no Advice section:\n%s", err, out)
	}
}

// TestApproveCheckAdviceFailOpen (F-0390, critic round): when the Gauntlet
// measurement fails — a corrupt feature JSON, an unreadable measured spec —
// the check still exits 0, keeps its impediment lines, skips the cost advice
// and keeps the level advice with no number.
func TestApproveCheckAdviceFailOpen(t *testing.T) {
	for name, breakIt := range map[string]func(t *testing.T, repoDir string){
		"corrupt feature JSON": func(t *testing.T, repoDir string) {
			if err := os.WriteFile(filepath.Join(repoDir, "entities", "features", "F-0099.json"), []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			commitBackend(t, repoDir)
		},
		"unreadable spec": func(t *testing.T, repoDir string) {
			if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0098", Title: "Medida", State: domain.StateImplemented}); err != nil {
				t.Fatal(err)
			}
			total := cost.Counters{Input: 1000}
			doc, err := cost.Stamp("# t\n", cost.Report{ID: "F-0098", Counters: total, Messages: 1,
				ByModel: map[string]cost.Counters{"m": total}, ByRole: map[string]cost.Counters{"critic": {Input: 100}}})
			if err != nil {
				t.Fatal(err)
			}
			writeDoc(t, repoDir, "F-0098", "result.md", doc)
			writeDoc(t, repoDir, "F-0098", "spec.md", "**Nível:** leve\n")
			commitBackend(t, repoDir)
			p := filepath.Join(repoDir, "content", "features", "F-0098", "spec.md")
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
			if _, err := os.ReadFile(p); err == nil {
				t.Skip("the spec stays readable (running as root)")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			id := newFeature(t, projDir, "Leve com barra", 1)
			writeDoc(t, repoDir, id, "spec.md", adviceSpecDoc(id, "leve"))
			commitBackend(t, repoDir)
			t.Setenv("CERNE_ACTOR", "agent")
			before, _ := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)

			breakIt(t, repoDir)
			out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
			if err != nil {
				t.Fatalf("a failed measurement must not fail the check: %v\n%s", err, out)
			}
			head, _, _ := strings.Cut(before, "Advice")
			if !strings.HasPrefix(out, head) {
				t.Errorf("the lines before the advice changed:\n--- before\n%s\n--- after\n%s", before, out)
			}
			if strings.Contains(out, "gauntlet-cost") || !strings.Contains(out, "light-with-bar: ") ||
				!strings.Contains(out, "not enough measured features yet to show the Gauntlet's cost here") {
				t.Errorf("want the cost advice skipped and the level advice with no number:\n%s", out)
			}
		})
	}
}
