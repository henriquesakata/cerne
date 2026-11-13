package cli

import (
	"errors"
	"strings"
	"testing"
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
