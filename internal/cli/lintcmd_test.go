package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeBackendDoc writes a content file at the rel slash path inside the
// cached backend clone.
func writeBackendDoc(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	full := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

// seedLintFixture seeds a backend with exactly 6 lint findings:
// 2 ref-to-superseded, 1 broken-link, 1 orphan-content, 2 missing-gate-doc.
func seedLintFixture(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Bills", State: domain.StateSuperseded, SupersededBy: "F-0002"},
		{ID: "F-0002", Title: "Carteira", State: domain.StateCandidate},
		{ID: "F-0003", Title: "Conta digital", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Relatórios", State: domain.StateApproved},
		{ID: "F-0005", Title: "Exportação", State: domain.StateImplemented},
		{ID: "F-0007", Title: "One", State: domain.StateSuperseded},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	d := store.Decision{ID: "D-0001", Title: "Adotar gateway", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	writeBackendDoc(t, repoDir, "content/features/F-0003/spec.md",
		"---\ncerne_id: F-0003\n---\n\n# Conta digital\n\n"+
			"Substitui o fluxo de Bills (F-0001).\n"+
			"Veja [antigo](../missing.md) e [site](https://example.com).\n")
	writeBackendDoc(t, repoDir, "content/features/F-0004/spec.md",
		"---\ncerne_id: F-0004\n---\n\n# Relatórios\n\n> Spec pendente — escreva o problema, objetivo e critérios de aceite.\n")
	writeBackendDoc(t, repoDir, "content/features/F-0005/spec.md",
		"---\ncerne_id: F-0005\n---\n\n# Exportação\n\nEntrega concluída.\n")
	writeBackendDoc(t, repoDir, "content/features/F-0005/plan.md",
		"---\ncerne_id: F-0005\n---\n\n# Exportação\n\nPassos executados.\n")
	writeBackendDoc(t, repoDir, "content/decisions/D-0001.md",
		"---\ncerne_id: D-0001\n---\n\n# Adotar gateway\n\nSubstitui F-0007 no fluxo.\n")
	writeBackendDoc(t, repoDir, "content/decisions/D-0099.md", "# Órfão\n\nSem entidade.\n")

	if _, err := gitback.CommitAndPush(repoDir, "seed lint fixtures"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestLintFindings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedLintFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--dir", projDir)
	if err == nil {
		t.Fatalf("lint with findings returned nil error, want exit error\noutput: %s", out)
	}
	if err.Error() != "8 problema(s) encontrado(s)" {
		t.Fatalf("lint error = %q, want %q", err.Error(), "8 problema(s) encontrado(s)")
	}

	// Groups in fixed rule order, each line "  <loc> — <detail>".
	blocks := []string{
		"REF-TO-SUPERSEDED (2)\n" +
			"  content/decisions/D-0001.md — menciona F-0007 (\"One\") que está substituída\n" +
			"  content/features/F-0003/spec.md — menciona F-0001 (\"Bills\") que está substituída — use F-0002\n",
		"BROKEN-LINK (1)\n" +
			"  content/features/F-0003/spec.md — link quebrado: ../missing.md\n",
		"ORPHAN-CONTENT (1)\n" +
			"  content/decisions/D-0099.md — conteúdo sem entidade: content/decisions/D-0099.md\n",
		"MISSING-GATE-DOC (2)\n" +
			"  F-0004 — feature approved sem spec.md real\n" +
			"  F-0005 — feature implemented sem result.md real\n",
		"INCOMPLETE-DOC (2)\n" +
			"  content/decisions/D-0001.md — content/decisions/D-0001.md: faltam as seções do template: Contexto, Decisão, Consequências, Alternativas consideradas, Implementação e relações\n" +
			"  content/features/F-0003/spec.md — content/features/F-0003/spec.md: faltam as seções do template: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Fora de escopo\n",
		"Total: 8 problema(s).\n",
	}
	prev := -1
	for _, b := range blocks {
		idx := strings.Index(out, b)
		if idx < 0 {
			t.Fatalf("output missing block %q:\n%s", b, out)
		}
		if idx < prev {
			t.Fatalf("block %q out of order:\n%s", b, out)
		}
		prev = idx
	}
	if strings.Contains(out, "Tarefas de manutenção") {
		t.Fatalf("output has fix-tasks block without --fix-tasks:\n%s", out)
	}
}

func TestLintFixTasks(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedLintFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--fix-tasks", "--dir", projDir)
	if err == nil {
		t.Fatalf("lint with findings returned nil error, want exit error\noutput: %s", out)
	}

	wantBlock := "## Tarefas de manutenção (geradas por cerne lint)\n" +
		"- [ ] content/decisions/D-0001.md: remover menção a F-0007 (\"One\") (substituída)\n" +
		"- [ ] content/features/F-0003/spec.md: substituir menção a F-0001 (\"Bills\") por F-0002\n" +
		"- [ ] content/features/F-0003/spec.md: corrigir link quebrado: ../missing.md\n" +
		"- [ ] content/decisions/D-0099.md: remover conteúdo sem entidade ou criar a entidade correspondente\n" +
		"- [ ] F-0004: escrever spec.md real (feature approved)\n" +
		"- [ ] F-0005: escrever result.md real (feature implemented)\n"
	if !strings.Contains(out, wantBlock) {
		t.Fatalf("output missing fix-tasks block:\nwant:\n%s\ngot:\n%s", wantBlock, out)
	}
	// The task list comes after the findings table.
	if strings.Index(out, "Total: 6 problema(s).") > strings.Index(out, "## Tarefas de manutenção") {
		t.Fatalf("fix-tasks block printed before the findings table:\n%s", out)
	}
}

func TestLintClean(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "lint", "--dir", projDir)
	if err != nil {
		t.Fatalf("lint on clean backend: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nenhum problema encontrado.") {
		t.Fatalf("clean output missing message:\n%s", out)
	}
}
