package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// cliLiteralSpec is a candidate spec whose criterion quotes a pt-BR CLI
// message; its criteria heading sits on line 7.
const cliLiteralSpec = "---\ncerne_id: F-0001\n---\n\n# T\n\n## Objetivo e critérios de aceitação\n\n" +
	"1. QUANDO nada existe, O SISTEMA DEVE imprimir `nenhuma feature encontrada`.\n"

// cliLiteralFixture is a project with candidate F-0001 carrying cliLiteralSpec
// in the backend clone; the lint key is left at its default.
func cliLiteralFixture(t *testing.T) (projDir string) {
	t.Helper()
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Listagem", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	spec := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	if err := os.MkdirAll(filepath.Dir(spec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, []byte(cliLiteralSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return projDir
}

// TestLintSpecCLILiteralPT (F-0353 criteria 1, 3): with lint.cli_output_language
// = "en" the lint prints the finding, as a warning, with the file line; with
// the key unset it prints nothing about it. That it never fails the lint alone
// is pinned by lint.Advisory in TestSpecCLILiteralFindings.
func TestLintSpecCLILiteralPT(t *testing.T) {
	projDir := cliLiteralFixture(t)

	out, _ := execCerne(t, "lint", "--dir", projDir) // the bare fixture has other blocking findings; only this rule is asserted
	if strings.Contains(out, "SPEC-CLI-LITERAL-PT") {
		t.Fatalf("key unset: the rule must not run (criterion 3):\n%s", out)
	}

	if err := project.SetSetting(projDir, "lint.cli_output_language", project.ScopeProject, "en"); err != nil {
		t.Fatal(err)
	}
	out, _ = execCerne(t, "lint", "--dir", projDir)
	for _, want := range []string{
		"SPEC-CLI-LITERAL-PT (1) (warning)",
		`line 9: CLI output literal reads pt-BR (pt-BR word "nenhuma"): nenhuma feature encontrada`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
