package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestRetireThenLintFlagsAlias is the case 0002 acceptance in miniature: a
// historical entity registered via `cerne retire` (no spec/case of its own)
// makes `cerne lint` flag a live document that still mentions one of its
// aliases — the Bills/Investments gap, closed end to end through the CLI.
func TestRetireThenLintFlagsAlias(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docsDir := filepath.Join(proj, ".docs")

	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--dir", proj)
	mustCerne(t, "feature", "new", "Relatórios financeiros", "--context", "ctx de teste", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)

	// Write a real spec that still mentions the legacy module, then push it.
	matches, err := filepath.Glob(filepath.Join(docsDir, "current", "features", "F-0001-*", "spec.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("glob materialized spec: matches=%v err=%v", matches, err)
	}
	realSpec := "---\ncerne_id: F-0001\n---\n\n# Relatórios financeiros\n\n" +
		"O relatório consolida o módulo de boletos com o restante.\n\n" +
		"## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n"
	if err := os.WriteFile(matches[0], []byte(realSpec), 0o644); err != nil {
		t.Fatalf("write real spec: %v", err)
	}
	mustCerne(t, "push", "--dir", proj)

	// Register Bills as historical, absorbed by Finance, with aliases.
	mustCerne(t, "retire", "Bills", "--by", "Finance", "--reason", "absorvido pelo Finance",
		"--alias", "bills", "--alias", "boletos", "--dir", proj)

	// lint exits non-zero and names the offending doc, the alias and the entity.
	out, err := cerne(t, "lint", "--dir", proj)
	if err == nil {
		t.Fatalf("lint found no problems, want a ref-to-superseded finding:\n%s", out)
	}
	for _, want := range []string{
		"REF-TO-SUPERSEDED",
		"content/features/F-0001/spec.md",
		"boletos", "H-0001", "Finance",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("lint output missing %q:\n%s", want, out)
		}
	}
}
