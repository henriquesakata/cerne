package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApproveWarnings (F-0353 criteria 1, 3, 6): the pt-BR CLI literal of a
// criterion is an approve warning, never an impediment, and only for a
// project whose lint.cli_output_language is "en".
func TestApproveWarnings(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	repoDir, _, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001",
		"---\ncerne_id: F-0001\n---\n\n# t\n\n## Objetivo e critérios de aceitação\n\n1. O sistema imprime `nenhuma feature encontrada`.\n\nBarra: referência nomeada — acesso\n")

	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ApproveWarnings(repoDir, projDir, "F-0001")
	if err != nil || len(got) != 0 {
		t.Fatalf("key unset: warnings = %v, err = %v; want none (criterion 3)", got, err)
	}

	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n[lint]\ncli_output_language = \"en\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = ApproveWarnings(repoDir, projDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	want := `spec-cli-literal-pt: line 9: CLI output literal reads pt-BR (pt-BR word "nenhuma"): nenhuma feature encontrada`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("warnings = %q, want [%q]", got, want)
	}

	// Never an impediment (criterion 6): ApproveReadiness does not carry it.
	imps, err := ApproveReadiness(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	for _, im := range imps {
		if strings.Contains(im.Message, "pt-BR") || im.Code == "spec-cli-literal-pt" {
			t.Errorf("the literal leaked into the impediments: %+v", im)
		}
	}
}
