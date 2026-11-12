package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestApproveCheckWarnsCLILiteralPT (F-0353 criteria 3, 6): on a spec whose
// criterion quotes a pt-BR CLI message, `approve --check` lists the finding as
// a warning and exits 0 when nothing else blocks; with the key unset it says
// nothing about it.
func TestApproveCheckWarnsCLILiteralPT(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Listagem", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "## Objetivo e critérios de aceitação\n\n1. O sistema imprime `nenhuma feature encontrada`."))
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || strings.Contains(out, "spec-cli-literal-pt") {
		t.Fatalf("key unset: err = %v, output must not mention the rule (criterion 3):\n%s", err, out)
	}

	if err := project.SetSetting(projDir, "lint.cli_output_language", project.ScopeProject, "en"); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil {
		t.Fatalf("a warning must not block the check: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Warnings (they do not block the approve):",
		`spec-cli-literal-pt: line 9: CLI output literal reads pt-BR (pt-BR word "nenhuma"): nenhuma feature encontrada`,
		"is approvable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
}
