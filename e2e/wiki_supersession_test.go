package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// writeDoc overwrites a materialized .docs/ document with real content, keeping
// the cerne_id frontmatter so push maps it back to the backend entity.
func writeDoc(t *testing.T, path, id, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCompileResolvesSupersessionThenLintClean reproduces the dogfooding bug F-0069
// fixes: an implemented feature's spec cites a decision later superseded; the
// generated wiki must present the successor, and lint must not flag the superseded
// id — all without editing the immutable spec or the generated page.
func TestCompileResolvesSupersessionThenLintClean(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docs := filepath.Join(proj, ".docs")

	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--module", "finance", "--dir", proj)

	// D-0001 superseded by D-0002.
	mustCerne(t, "decision", "new", "ADR base", "--dir", proj)
	mustCerne(t, "decision", "new", "ADR nova", "--dir", proj)
	mustCerne(t, "decision", "substituir", "D-0001", "--by", "D-0002", "--reason", "redesign", "--dir", proj)

	// An implemented feature whose spec cites the superseded D-0001. Implemented
	// specs are not lint-scanned, so the only place the stale id could surface is
	// the generated wiki — exactly the case F-0069 fixes.
	mustCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--facet", "code", "--module", "finance", "--dir", proj)
	feat := filepath.Join(docs, "current", "features", "F-0001-boletos")
	mustCerne(t, "pull", "--dir", proj)
	writeDoc(t, filepath.Join(feat, "spec.md"), "F-0001",
		"# Boletos\n\nO fluxo segue a decisão base D-0001 do projeto.\n\n## Critérios\n\n- Gera boleto.\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeDoc(t, filepath.Join(feat, "plan.md"), "F-0001",
		"# Plano\n\n## Estratégia\n\nUm MR.\n\n## Sequência de tarefas\n\n1. Implementar com teste e verificar.\n")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "start", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeDoc(t, filepath.Join(feat, "result.md"), "F-0001",
		"# Resultado\n\n## Resultado\n\nEntregue.\n\n## Aceitação\n\n1. ATENDIDO — boleto gerado e testado.\n\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "complete", "F-0001", "--dir", proj)

	// compile --force resolves the superseded reference in the emitted prompt.
	mustCerne(t, "compile", "--force", "--scope", "finance", "--dir", proj)
	prompt := mustRead(t, filepath.Join(docs, "wiki", ".pending", "modules", "finance.prompt.md"))
	if !strings.Contains(prompt, "D-0002 (antes D-0001)") {
		t.Fatalf("compile prompt did not resolve the superseded id to its successor:\n%s", prompt)
	}
	if strings.Contains(strings.ReplaceAll(prompt, "(antes D-0001)", ""), "D-0001") {
		t.Fatalf("compile prompt still presents D-0001 as a current reference:\n%s", prompt)
	}

	// The agent synthesizes the page from the resolved prompt (current decision);
	// no run_id frontmatter, so ingest accepts it by mtime (written after the prompt).
	resp := filepath.Join(docs, "wiki", ".pending", "modules", "finance.md")
	if err := os.WriteFile(resp, []byte("# Finance\n\nO fluxo segue a decisão vigente D-0002.\n"), 0o644); err != nil {
		t.Fatalf("write response: %v", err)
	}
	mustCerne(t, "compile", "--ingest", "--dir", proj)

	// lint is green about supersession: nothing live cites the superseded D-0001.
	out, _ := cerne(t, "lint", "--dir", proj)
	if strings.Contains(out, "D-0001") {
		t.Fatalf("lint still surfaces the superseded D-0001 (wiki unresolved / spec scanned):\n%s", out)
	}
}
