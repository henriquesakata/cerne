package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
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

// seedLintFixture seeds a backend with 11 lint findings: 2 ref-to-superseded,
// 1 broken-link, 1 orphan-content, 2 missing-gate-doc, 1 stale-note,
// 2 incomplete-doc, 1 unregistered-source, 1 spec-missing-bar.
func seedLintFixture(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	features := []store.Feature{
		// Live features tagged so untagged-live (features joined that rule in
		// D-0003) stays silent — this fixture counts a fixed set from other rules.
		{ID: "F-0001", Title: "Bills", State: domain.StateSuperseded, SupersededBy: "F-0002"},
		{ID: "F-0002", Title: "Carteira", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0003", Title: "Conta digital", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0004", Title: "Relatórios", State: domain.StateApproved, Facets: []string{"data"}},
		{ID: "F-0005", Title: "Exportação", State: domain.StateImplemented},
		{ID: "F-0007", Title: "One", State: domain.StateSuperseded},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	// Tagged so the untagged-live rule (F-0097) stays silent — this fixture
	// counts a fixed set of findings from the other rules.
	d := store.Decision{ID: "D-0001", Title: "Adotar gateway", State: domain.DecisionActive, Facets: []string{"infra"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	// An active note informing only the implemented F-0005 → stale-note (tagged,
	// so untagged-live stays silent and the count stays fixed).
	note := store.Note{ID: "N-0001", Title: "Migração da exportação", State: domain.NoteActive, Informs: []string{"F-0005"}, Facets: []string{"process"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveNote(repoDir, note); err != nil {
		t.Fatalf("SaveNote: %v", err)
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
	if err.Error() != "11 problem(s) found" {
		t.Fatalf("lint error = %q, want %q", err.Error(), "11 problem(s) found")
	}

	// Groups in fixed rule order, each line "  <loc> — <detail>".
	blocks := []string{
		"REF-TO-SUPERSEDED (2)\n" +
			"  content/decisions/D-0001.md — mentions F-0007 (\"One\"), which is superseded\n" +
			"  content/features/F-0003/spec.md — mentions F-0001 (\"Bills\"), which is superseded — use F-0002\n",
		"BROKEN-LINK (1)\n" +
			"  content/features/F-0003/spec.md:8 — broken link ../missing.md — no such file: fix the path or remove the link, then 'cerne push'\n",
		"ORPHAN-CONTENT (1)\n" +
			"  content/decisions/D-0099.md — content without an entity: content/decisions/D-0099.md\n",
		"MISSING-GATE-DOC (2)\n" +
			"  F-0004 — approved feature without a real spec.md\n" +
			"  F-0005 — implemented feature without a real result.md\n",
		"STALE-NOTE (1)\n" +
			"  N-0001 — only informs concluded work (F-0005 implemented) — retire it or confirm it is still durable\n",
		"INCOMPLETE-DOC (2)\n" +
			"  content/decisions/D-0001.md — content/decisions/D-0001.md: missing template sections: Contexto, Decisão, Consequências, Alternativas consideradas, Implementação e relações\n" +
			"  content/features/F-0003/spec.md — content/features/F-0003/spec.md: missing template sections: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Execução recomendada (agente), Fora de escopo\n",
		"UNREGISTERED-SOURCE (1)\n" +
			"  content/features/F-0003/spec.md — external URL https://example.com cited without a registered reference — register it with 'cerne reference new … --informs F-0003' or remove the URL\n",
		"SPEC-MISSING-BAR (1)\n" +
			"  content/features/F-0003/spec.md — no \"Barra:\" line (Conta digital) — the approve refuses it; 'cerne feature approve F-0003 --check' lists the accepted forms\n",
		"Total: 11 problem(s).\n",
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
	if strings.Contains(out, "Maintenance tasks") {
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

	wantBlock := "## Maintenance tasks (generated by cerne lint)\n" +
		"- [ ] content/decisions/D-0001.md: remove the mention of F-0007 (\"One\") (superseded)\n" +
		"- [ ] content/features/F-0003/spec.md: replace the mention of F-0001 (\"Bills\") with F-0002\n" +
		"- [ ] content/features/F-0003/spec.md:8: fix broken link ../missing.md — no such file: fix the path or remove the link, then 'cerne push'\n" +
		"- [ ] content/decisions/D-0099.md: remove the entity-less content or create the matching entity\n" +
		"- [ ] F-0004: write spec.md (approved feature)\n" +
		"- [ ] F-0005: write result.md (implemented feature)\n" +
		"- [ ] N-0001: retire it (cerne note retire) or mark it durable (cerne note keep)\n"
	if !strings.Contains(out, wantBlock) {
		t.Fatalf("output missing fix-tasks block:\nwant:\n%s\ngot:\n%s", wantBlock, out)
	}
	// The task list comes after the findings table.
	if strings.Index(out, "Total: 10 problem(s).") > strings.Index(out, "## Maintenance tasks") {
		t.Fatalf("fix-tasks block printed before the findings table:\n%s", out)
	}
}

func TestLintClean(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "lint", "--dir", projDir)
	if err != nil {
		t.Fatalf("lint on clean backend: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No problems found.") {
		t.Fatalf("clean output missing message:\n%s", out)
	}
}

func TestLintStagedCatchesLocalOnlyProblem(t *testing.T) {
	projDir, _, repoDir, specPath := pulledProject(t)
	headBefore, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	// Introduce a broken link in the LOCAL spec only — no push.
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nver [detalhe](./inexistente.md)\n")

	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err == nil {
		t.Fatalf("lint --staged should fail on the local-only broken link, got:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "broken-link") && !strings.Contains(out, "link quebrado") {
		t.Fatalf("expected a broken-link finding, got:\n%s", out)
	}
	headAfter, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if headBefore != headAfter {
		t.Fatalf("lint --staged mutated the backend clone: %s -> %s", headBefore, headAfter)
	}
}

func TestLintStagedPartialDocsNoFalsePositive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	// An implemented feature with complete gate docs. pull does NOT materialize
	// implemented features, so .docs/ is partial — yet the staged tree carries the
	// full snapshot, so no orphan-content/missing-gate-doc fires from its absence.
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Exportação", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	for _, name := range []string{"spec.md", "plan.md", "result.md"} {
		writeBackendDoc(t, repoDir, "content/features/F-0001/"+name, realDoc("F-0001", "Conteúdo real de "+name))
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed implemented"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}

	out, err := execCerne(t, "lint", "--staged", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("lint --staged on partial .docs/ should pass, got:\n%s", out)
	}
	if !strings.Contains(out, "No problems found.") {
		t.Fatalf("partial .docs/ produced a false-positive finding:\n%s", out)
	}
}

func TestLintStagedParityWithBackend(t *testing.T) {
	projDir, _, repoDir, specPath := pulledProject(t)
	now := time.Now().UTC()
	// A superseded entity so a mention of it is a ref-to-superseded finding.
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0009", Title: "Antigo", State: domain.StateSuperseded, SupersededBy: "F-0001", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed superseded F-0009"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	// A defective edit citing the superseded F-0009.
	mustWrite(t, specPath, realDoc("F-0001", "ainda referencia F-0009 no fluxo"))

	// Mode A: the same engine over the staged local tree.
	stagedOut, _ := execCerne(t, "lint", "--staged", "--all", "--dir", projDir)
	// Mode B: push the edit, then lint over the backend.
	if out, err := execCerne(t, "push", "--dir", projDir); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	backendOut, _ := execCerne(t, "lint", "--dir", projDir)

	if stagedOut != backendOut {
		t.Fatalf("staged vs backend lint differ (single source of rules broken):\n--staged:\n%s\n--backend:\n%s", stagedOut, backendOut)
	}
	if !strings.Contains(stagedOut, "F-0009") {
		t.Fatalf("expected the ref-to-superseded finding in both, got:\n%s", stagedOut)
	}
}

// TestLintFixTasksErrataAppends pins F-0329 C1: an errata is append-only, so the
// task for a superseded mention in it is the errata command the finding
// carries — never "replace"/"remove the mention" — whether the mentioned entity
// has a successor (superseded) or not (rejected), and the detail's hint never
// leaks into the parsed successor or state.
func TestLintFixTasksErrataAppends(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	for _, d := range []store.Decision{
		{ID: "D-0001", Title: "Pagamentos", State: domain.DecisionActive, Facets: []string{"infra"}},
		{ID: "D-0002", Title: "Fila Kafka", State: domain.DecisionSuperseded, SupersededBy: "D-0003"},
		{ID: "D-0003", Title: "Fila SQS", State: domain.DecisionActive, Facets: []string{"infra"}},
	} {
		d.CreatedAt, d.UpdatedAt = now, now
		if err := store.SaveDecision(repoDir, d); err != nil {
			t.Fatalf("SaveDecision %s: %v", d.ID, err)
		}
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Recurso descartado", State: domain.StateRejected, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.AppendErrata(repoDir, "D-0001", "Os pagamentos passam pela D-0002 e pelo F-0001.", "ator", now); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}

	out, _ := execCerne(t, "lint", "--fix-tasks", "--dir", projDir)
	_, tasks, ok := strings.Cut(out, "## Maintenance tasks")
	if !ok {
		t.Fatalf("output missing fix-tasks block:\n%s", out)
	}
	for _, id := range []string{"D-0002", "F-0001"} {
		want := "- [ ] content/errata/D-0001.md: append an errata entry (the errata is append-only): cerne decision errata D-0001 '<!-- lint:ok-superseded " + id + " --> <why it is history; what is current>'\n"
		if !strings.Contains(tasks, want) {
			t.Errorf("fix-tasks missing the append task for %s:\nwant %q\ngot:\n%s", id, want, tasks)
		}
	}
	for _, line := range strings.Split(tasks, "\n") {
		if strings.Contains(line, "content/errata/") && (strings.Contains(line, "replace the mention") || strings.Contains(line, "remove the mention") || strings.Count(line, "lint:ok-superseded") != 1) {
			t.Errorf("errata task edits the mention or repeats the hint: %q", line)
		}
	}
}
