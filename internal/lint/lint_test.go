package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeDoc writes a content file at the rel slash path inside the backend.
func writeDoc(t *testing.T, backendDir, rel, content string) {
	t.Helper()
	full := filepath.Join(backendDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

// seedBackend builds a backend dir (plain store saves + content writes, no
// git) exercising every lint rule plus the negative cases:
//   - F-0001 "Bills" superseded by F-0002; F-0003 (candidate) mentions both
//     its ID and title → single deduped finding with the ID detail.
//   - F-0007 "One" superseded: title < 4 runes is no needle ("one-shot" in
//     F-0003's spec stays silent); the ID F-0007 in D-0001's doc still hits.
//   - D-0002 superseded decision and R-0001 obsolete reference mentioned by
//     ID; the obsolete reference's TITLE ("API antiga" in D-0001's doc) is
//     not a needle.
//   - broken relative link vs ok http/anchor/mailto/existing-relative links.
//   - orphan content files without entity JSON.
//   - F-0004 approved with stub spec; F-0005 implemented without result.md
//     (its spec mentions "Bills" but implemented docs are not scanned).
func seedBackend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
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
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	decisions := []store.Decision{
		{ID: "D-0001", Title: "Adotar gateway", State: domain.DecisionActive},
		{ID: "D-0002", Title: "Usar boleto legado", State: domain.DecisionSuperseded, SupersededBy: "D-0003"},
	}
	for _, d := range decisions {
		d.CreatedAt, d.UpdatedAt = now, now
		if err := store.SaveDecision(dir, d); err != nil {
			t.Fatalf("SaveDecision %s: %v", d.ID, err)
		}
	}
	references := []store.Reference{
		{ID: "R-0001", Title: "API antiga", State: domain.ReferenceObsolete},
		{ID: "R-0002", Title: "Guia atual", State: domain.ReferenceCurrent},
	}
	for _, r := range references {
		r.CreatedAt, r.UpdatedAt = now, now
		if err := store.SaveReference(dir, r); err != nil {
			t.Fatalf("SaveReference %s: %v", r.ID, err)
		}
	}

	writeDoc(t, dir, "content/features/F-0003/spec.md",
		"---\ncerne_id: F-0003\n---\n\n# Conta digital\n\n"+
			"Substitui o fluxo de Bills (F-0001) e a decisão D-0002; veja R-0001.\n"+
			"Abordagem one-shot para o MVP.\n"+
			"Links: [antigo](../missing.md), [site](https://example.com), "+
			"[este](spec.md), [topo](#secao), [contato](mailto:x@y.z).\n")
	writeDoc(t, dir, "content/features/F-0004/spec.md",
		"---\ncerne_id: F-0004\n---\n\n# Relatórios\n\n> Spec pendente — escreva o problema, objetivo e critérios de aceite.\n")
	writeDoc(t, dir, "content/features/F-0005/spec.md",
		"---\ncerne_id: F-0005\n---\n\n# Exportação\n\nSubstituiu Bills na época.\n")
	writeDoc(t, dir, "content/features/F-0005/plan.md",
		"---\ncerne_id: F-0005\n---\n\n# Exportação\n\nPassos executados.\n")
	writeDoc(t, dir, "content/decisions/D-0001.md",
		"---\ncerne_id: D-0001\n---\n\n# Adotar gateway\n\nSubstitui F-0007 e a API antiga do provedor.\n")
	writeDoc(t, dir, "content/references/R-0002.md",
		"---\ncerne_id: R-0002\n---\n\n# Guia atual\n\nVeja [nada](./nada.md).\n")
	writeDoc(t, dir, "content/features/F-0099/spec.md", "# Órfão\n\nSem entidade.\n")
	writeDoc(t, dir, "content/decisions/D-0099.md", "# Órfão\n\nSem entidade.\n")
	return dir
}

func TestParentFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", Title: "Épico vivo", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Filha sob épico inexistente", Parent: "F-9999", State: domain.StateCandidate},
		{ID: "F-0003", Title: "Épico morto", State: domain.StateSuperseded, SupersededBy: "F-0001"},
		{ID: "F-0004", Title: "Filha órfã", Parent: "F-0003", State: domain.StateApproved},
		{ID: "F-0005", Title: "Épico concluído", State: domain.StateImplemented},
		{ID: "F-0006", Title: "Filha em aberto", Parent: "F-0005", State: domain.StateInDevelopment},
		{ID: "F-0007", Title: "Filha pronta", Parent: "F-0005", State: domain.StateImplemented},
	}
	got := parentFindings(features)

	want := map[string]string{ // entity -> rule
		"F-0002": RuleParentNotFound,
		"F-0004": RuleParentTerminal,
		"F-0005": RuleEpicOpenChildren,
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %d (%+v), want %d", len(got), got, len(want))
	}
	for _, f := range got {
		wantRule, ok := want[f.Entity]
		if !ok || f.Rule != wantRule {
			t.Fatalf("finding %+v unexpected; want rule %q for entity %s", f, wantRule, f.Entity)
		}
	}
}

func TestRunFindings(t *testing.T) {
	dir := seedBackend(t)

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []Finding{
		{Rule: RuleRefToSuperseded, Entity: "D-0001", File: "content/decisions/D-0001.md",
			Detail: `menciona F-0007 ("One") que está substituída`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `menciona D-0002 ("Usar boleto legado") que está substituída — use D-0003`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `menciona F-0001 ("Bills") que está substituída — use F-0002`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `menciona R-0001 ("API antiga") que está obsoleta`},
		{Rule: RuleBrokenLink, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: "link quebrado: ../missing.md"},
		{Rule: RuleBrokenLink, Entity: "R-0002", File: "content/references/R-0002.md",
			Detail: "link quebrado: ./nada.md"},
		{Rule: RuleOrphanContent, Entity: "", File: "content/decisions/D-0099.md",
			Detail: "conteúdo sem entidade: content/decisions/D-0099.md"},
		{Rule: RuleOrphanContent, Entity: "", File: "content/features/F-0099/spec.md",
			Detail: "conteúdo sem entidade: content/features/F-0099/spec.md"},
		{Rule: RuleMissingGateDoc, Entity: "F-0004", File: "",
			Detail: "feature approved sem spec.md real"},
		{Rule: RuleMissingGateDoc, Entity: "F-0005", File: "",
			Detail: "feature implemented sem result.md real"},
	}
	if len(got) != len(want) {
		t.Fatalf("Run returned %d findings, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("finding[%d] =\n  %+v\nwant\n  %+v", i, got[i], want[i])
		}
	}
}

// seedImportedBackend builds a backend whose terminal entities carry content
// docs with imported_from frontmatter, so ref-to-superseded must also match
// the ORIGINAL identifiers, not only the cerne ID/title:
//   - F-0090 imported from a case dir → needles "0090_plugin-autonomous-agent"
//     and "case 0090". F-0091's spec mentions BOTH forms (single deduped
//     finding, folder-name detail); F-0092's spec mentions only "case 0090".
//   - D-0045 imported from an ADR file → needles
//     "adr-045-identity-as-library-baseline" and "adr-045"; F-0092's spec
//     hits the short form.
//   - R-0050 obsolete, imported from another shape (docs/planning/
//     roadmap-2024.md) → needle "roadmap-2024"; F-0091's spec hits it.
//   - R-0051 imported from docs/notes/ab.md → needle "ab" dropped by the
//     min-length guard; F-0093's spec mentions "ab" and stays silent.
//   - F-0094 terminal with a content doc WITHOUT frontmatter → no imported
//     needles, unchanged behavior.
//   - F-0093's spec mentions "case 00901" → word boundary, no match.
func seedImportedBackend(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()

	features := []store.Feature{
		{ID: "F-0090", Title: "Agente autônomo", State: domain.StateSuperseded, SupersededBy: "F-0091"},
		{ID: "F-0091", Title: "Plataforma de agentes", State: domain.StateCandidate},
		{ID: "F-0092", Title: "Relatório de uso", State: domain.StateCandidate},
		{ID: "F-0093", Title: "Limites de uso", State: domain.StateCandidate},
		{ID: "F-0094", Title: "Painel antigo", State: domain.StateSuperseded},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	d := store.Decision{ID: "D-0045", Title: "Identity como biblioteca", State: domain.DecisionSuperseded, SupersededBy: "D-0046", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(dir, d); err != nil {
		t.Fatalf("SaveDecision %s: %v", d.ID, err)
	}
	references := []store.Reference{
		{ID: "R-0050", Title: "Roadmap antigo", State: domain.ReferenceObsolete},
		{ID: "R-0051", Title: "Notas curtas", State: domain.ReferenceObsolete},
	}
	for _, r := range references {
		r.CreatedAt, r.UpdatedAt = now, now
		if err := store.SaveReference(dir, r); err != nil {
			t.Fatalf("SaveReference %s: %v", r.ID, err)
		}
	}

	writeDoc(t, dir, "content/features/F-0090/spec.md",
		"---\ncerne_id: F-0090\nimported_from: docs/process/cases/0090_plugin-autonomous-agent/\n---\n\n# Agente autônomo\n\nHistórico.\n")
	writeDoc(t, dir, "content/decisions/D-0045.md",
		"---\ncerne_id: D-0045\nimported_from: docs/architecture/adr/adr-045-identity-as-library-baseline.md\n---\n\n# Identity como biblioteca\n\nHistórico.\n")
	writeDoc(t, dir, "content/references/R-0050.md",
		"---\ncerne_id: R-0050\nimported_from: docs/planning/roadmap-2024.md\n---\n\n# Roadmap antigo\n\nHistórico.\n")
	writeDoc(t, dir, "content/references/R-0051.md",
		"---\ncerne_id: R-0051\nimported_from: docs/notes/ab.md\n---\n\n# Notas curtas\n\nHistórico.\n")
	writeDoc(t, dir, "content/features/F-0094/spec.md",
		"# Painel antigo\n\nSem frontmatter.\n")

	writeDoc(t, dir, "content/features/F-0091/spec.md",
		"---\ncerne_id: F-0091\n---\n\n# Plataforma de agentes\n\n"+
			"Evolui o 0090_plugin-autonomous-agent (case 0090) sem depender do roadmap-2024.\n")
	writeDoc(t, dir, "content/features/F-0092/spec.md",
		"---\ncerne_id: F-0092\n---\n\n# Relatório de uso\n\n"+
			"Decisão herdada do adr-045; escopo original no case 0090.\n")
	writeDoc(t, dir, "content/features/F-0093/spec.md",
		"---\ncerne_id: F-0093\n---\n\n# Limites de uso\n\n"+
			"Ver case 00901 e o plano ab para detalhes.\n")
	return dir
}

func TestRunImportedFromNeedles(t *testing.T) {
	dir := seedImportedBackend(t)

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []Finding{
		{Rule: RuleRefToSuperseded, Entity: "F-0091", File: "content/features/F-0091/spec.md",
			Detail: `menciona 0090_plugin-autonomous-agent (F-0090 "Agente autônomo") que está substituída — use F-0091`},
		{Rule: RuleRefToSuperseded, Entity: "F-0091", File: "content/features/F-0091/spec.md",
			Detail: `menciona roadmap-2024 (R-0050 "Roadmap antigo") que está obsoleta`},
		{Rule: RuleRefToSuperseded, Entity: "F-0092", File: "content/features/F-0092/spec.md",
			Detail: `menciona adr-045 (D-0045 "Identity como biblioteca") que está substituída — use D-0046`},
		{Rule: RuleRefToSuperseded, Entity: "F-0092", File: "content/features/F-0092/spec.md",
			Detail: `menciona case 0090 (F-0090 "Agente autônomo") que está substituída — use F-0091`},
	}
	if len(got) != len(want) {
		t.Fatalf("Run returned %d findings, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("finding[%d] =\n  %+v\nwant\n  %+v", i, got[i], want[i])
		}
	}
}

func TestRunCleanBackend(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The .gitkeep placeholders InitLayout writes must not count as orphans.
	if len(got) != 0 {
		t.Fatalf("Run on clean backend = %+v, want no findings", got)
	}
}
