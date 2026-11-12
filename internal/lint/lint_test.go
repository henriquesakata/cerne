package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/plantmpl"
	"github.com/henriquesakata/cerne/internal/store"
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
		// Live features are tagged so the untagged-live rule stays silent — this
		// fixture exercises the other rules (D-0003 brought features into that rule).
		{ID: "F-0001", Title: "Bills", State: domain.StateSuperseded, SupersededBy: "F-0002"},
		{ID: "F-0002", Title: "Carteira", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0003", Title: "Conta digital", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0004", Title: "Relatórios", State: domain.StateApproved, Facets: []string{"data"}},
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
		// Tagged so the untagged-live rule stays silent — this fixture exercises
		// the other rules; an untagged live entity is a separate finding (F-0097).
		{ID: "D-0001", Title: "Adotar gateway", State: domain.DecisionActive, Facets: []string{"infra"}},
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
		{ID: "R-0002", Title: "Guia atual", State: domain.ReferenceCurrent, Facets: []string{"code"}},
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

// TestNoteLint pins the note parity rules, reusing the existing rule machinery:
//   - an active note that mentions a superseded entity → ref-to-superseded.
//   - a content/notes/<ID>.md with no note entity → orphan-content.
//   - a superseded note becomes a needle: a live doc mentioning it is flagged.
func TestNoteLint(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Núcleo", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0002", Title: "x", State: domain.DecisionSuperseded, SupersededBy: "D-0003", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	notes := []store.Note{
		{ID: "N-0001", Title: "Nota viva", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now},
		{ID: "N-0007", Title: "Nota substituída longa", State: domain.NoteSuperseded, SupersededBy: "N-0008", CreatedAt: now, UpdatedAt: now},
	}
	for _, n := range notes {
		if err := store.SaveNote(dir, n); err != nil {
			t.Fatalf("SaveNote %s: %v", n.ID, err)
		}
	}

	// Active note mentions superseded D-0002 → ref-to-superseded on the note.
	writeDoc(t, dir, "content/notes/N-0001.md",
		"---\ncerne_id: N-0001\n---\n\n# Nota viva\n\nNão confie na decisão D-0002, foi substituída.\n")
	// Superseded note's content (history) is not scanned, but its ID is a needle.
	writeDoc(t, dir, "content/notes/N-0007.md",
		"---\ncerne_id: N-0007\n---\n\n# Nota substituída longa\n\nConteúdo antigo.\n")
	// A live feature spec that mentions the superseded note N-0007 → flagged.
	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Núcleo\n\nVeja a antiga N-0007 para o histórico.\n")
	// Orphan note content: no N-0099 entity.
	writeDoc(t, dir, "content/notes/N-0099.md", "# Órfã\n\nSem entidade.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawRefOnNote, sawNeedleOnSpec, sawOrphan bool
	for _, f := range findings {
		switch {
		case f.Rule == RuleRefToSuperseded && f.File == "content/notes/N-0001.md":
			sawRefOnNote = true // active note flagged for mentioning superseded D-0002
		case f.Rule == RuleRefToSuperseded && f.File == "content/features/F-0001/spec.md" && f.Detail != "":
			if strings.Contains(f.Detail, "N-0007") {
				sawNeedleOnSpec = true // superseded note is a needle
			}
		case f.Rule == RuleOrphanContent && f.File == "content/notes/N-0099.md":
			sawOrphan = true
		}
	}
	if !sawRefOnNote {
		t.Errorf("active note mentioning superseded D-0002 not flagged:\n%+v", findings)
	}
	if !sawNeedleOnSpec {
		t.Errorf("superseded note N-0007 did not become a needle:\n%+v", findings)
	}
	if !sawOrphan {
		t.Errorf("orphan note content N-0099 not flagged:\n%+v", findings)
	}
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

func TestEpicBehindChildren(t *testing.T) {
	features := []store.Feature{
		// epic candidate with a child already in development -> fires
		{ID: "F-0001", Title: "Épico não iniciado", State: domain.StateCandidate},
		{ID: "F-0002", Title: "Filha em dev", Parent: "F-0001", State: domain.StateInDevelopment},
		// epic approved with a child implemented -> fires
		{ID: "F-0003", Title: "Épico só aprovado", State: domain.StateApproved},
		{ID: "F-0004", Title: "Filha pronta", Parent: "F-0003", State: domain.StateImplemented},
		// epic candidate but children only candidate/approved -> normal decomposition, no fire
		{ID: "F-0005", Title: "Épico em desenho", State: domain.StateCandidate},
		{ID: "F-0006", Title: "Filha candidata", Parent: "F-0005", State: domain.StateCandidate},
		{ID: "F-0007", Title: "Filha aprovada", Parent: "F-0005", State: domain.StateApproved},
		// epic already in development with advanced child -> epic not behind, no fire
		{ID: "F-0008", Title: "Épico em dev", State: domain.StateInDevelopment},
		{ID: "F-0009", Title: "Filha em dev", Parent: "F-0008", State: domain.StateInDevelopment},
	}
	got := parentFindings(features)

	behind := map[string]bool{}
	for _, f := range got {
		if f.Rule == RuleEpicBehindChildren {
			behind[f.Entity] = true
		}
	}
	want := []string{"F-0001", "F-0003"}
	if len(behind) != len(want) {
		t.Fatalf("epic-behind findings = %v (all=%+v), want %v", behind, got, want)
	}
	for _, id := range want {
		if !behind[id] {
			t.Fatalf("missing epic-behind finding for %s; got %v", id, behind)
		}
	}
}

// TestEpicUnplannedFinding (F-0235): a live epic with a live child but a
// still-stub plan.md is flagged, regardless of whether any child has started —
// unlike epic-behind-children, which needs a started child. A filled plan, or
// no live children, stays clean.
func TestEpicUnplannedFinding(t *testing.T) {
	dir := t.TempDir()
	features := []store.Feature{
		// candidate epic, candidate child, plan still stub -> fires
		{ID: "F-0001", Title: "Épico sem plano", State: domain.StateCandidate},
		{ID: "F-0002", Title: "Filha candidata", Parent: "F-0001", State: domain.StateCandidate},
		// approved epic, plan filled -> clean
		{ID: "F-0003", Title: "Épico planejado", State: domain.StateApproved},
		{ID: "F-0004", Title: "Filha candidata", Parent: "F-0003", State: domain.StateCandidate},
		// candidate epic, no live children (only rejected) -> clean
		{ID: "F-0005", Title: "Épico sem filho vivo", State: domain.StateCandidate},
		{ID: "F-0006", Title: "Filha rejeitada", Parent: "F-0005", State: domain.StateRejected},
		// terminal epic with stub plan -> clean (settled history, not linted)
		{ID: "F-0007", Title: "Épico morto", State: domain.StateSuperseded},
		{ID: "F-0008", Title: "Filha", Parent: "F-0007", State: domain.StateCandidate},
	}
	writeDoc(t, dir, "content/features/F-0001/plan.md", plantmpl.Stub("Épico sem plano"))
	writeDoc(t, dir, "content/features/F-0003/plan.md",
		"---\ncerne_id: F-0003\n---\n\n# Épico planejado\n\nPlano real o bastante para o doccheck contar.\n")
	// F-0005, F-0007: no plan.md at all (missing counts as stub, same result).

	got, err := epicUnplannedFindings(dir, features)
	if err != nil {
		t.Fatalf("epicUnplannedFindings: %v", err)
	}
	if len(got) != 1 || got[0].Rule != RuleEpicUnplanned || got[0].Entity != "F-0001" {
		t.Fatalf("findings = %+v, want exactly one epic-unplanned for F-0001", got)
	}
}

// TestEpicUnplannedNotOverlapEpicBehindChildren (F-0235 crit.4): an epic can
// legitimately trip both rules at once (stub plan + a child already in dev) —
// the two conditions are independent, neither rule ever substitutes for the
// other's own fixtures.
func TestEpicUnplannedNotOverlapEpicBehindChildren(t *testing.T) {
	dir := t.TempDir()
	features := []store.Feature{
		{ID: "F-0001", Title: "Épico não iniciado, sem plano", State: domain.StateCandidate},
		{ID: "F-0002", Title: "Filha em dev", Parent: "F-0001", State: domain.StateInDevelopment},
	}
	writeDoc(t, dir, "content/features/F-0001/plan.md", plantmpl.Stub("Épico não iniciado, sem plano"))

	behind := parentFindings(features)
	if len(behind) != 1 || behind[0].Rule != RuleEpicBehindChildren {
		t.Fatalf("parentFindings = %+v, want one epic-behind-children", behind)
	}
	unplanned, err := epicUnplannedFindings(dir, features)
	if err != nil {
		t.Fatalf("epicUnplannedFindings: %v", err)
	}
	if len(unplanned) != 1 || unplanned[0].Rule != RuleEpicUnplanned {
		t.Fatalf("epicUnplannedFindings = %+v, want one epic-unplanned (both rules legitimately fire together)", unplanned)
	}
}

// TestGateFindingsDoneRequiresDocs (F-0131): done is implemented's accepted
// successor, so it carries the same document requirements — a done feature
// missing result.md is flagged exactly like an implemented one would be.
func TestGateFindingsDoneRequiresDocs(t *testing.T) {
	dir := t.TempDir()
	features := []store.Feature{
		{ID: "F-0001", Title: "Aceita sem result", State: domain.StateDone},
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Aceita sem result\n\nProblema real.\n")
	writeDoc(t, dir, "content/features/F-0001/plan.md",
		"---\ncerne_id: F-0001\n---\n\n# Aceita sem result\n\nPlano real.\n")

	got, err := gateFindings(dir, features)
	if err != nil {
		t.Fatalf("gateFindings: %v", err)
	}
	if len(got) != 1 || got[0].Rule != RuleMissingGateDoc || got[0].Entity != "F-0001" || !strings.Contains(got[0].Detail, "result.md") {
		t.Fatalf("gateFindings = %+v, want one missing-gate-doc for F-0001 naming result.md", got)
	}
}

// TestGateFindingsTaskSkipsPlanGate (F-0216): a task (T-) has no plan gate
// by design (F-0203) — lint must not flag it missing, but still requires
// spec/result like any feature-shaped entity.
func TestGateFindingsTaskSkipsPlanGate(t *testing.T) {
	dir := t.TempDir()
	features := []store.Feature{
		{ID: "T-0001", Title: "Task sem plan", State: domain.StateImplemented},
		{ID: "T-0002", Title: "Task sem result", State: domain.StateImplemented},
		{ID: "F-0001", Title: "Feature sem plan", State: domain.StateInDevelopment},
	}
	writeDoc(t, dir, "content/features/T-0001/spec.md",
		"---\ncerne_id: T-0001\n---\n\n# Task sem plan\n\nProblema real.\n")
	writeDoc(t, dir, "content/features/T-0001/result.md",
		"---\ncerne_id: T-0001\n---\n\n# Task sem plan\n\nResultado real.\n")
	writeDoc(t, dir, "content/features/T-0002/spec.md",
		"---\ncerne_id: T-0002\n---\n\n# Task sem result\n\nProblema real.\n")
	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Feature sem plan\n\nProblema real.\n")

	got, err := gateFindings(dir, features)
	if err != nil {
		t.Fatalf("gateFindings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("gateFindings = %+v, want 2 findings (T-0002 result, F-0001 plan)", got)
	}
	byEntity := map[string]Finding{}
	for _, f := range got {
		byEntity[f.Entity] = f
	}
	if f, ok := byEntity["T-0002"]; !ok || !strings.Contains(f.Detail, "result.md") {
		t.Fatalf("want T-0002 missing result.md finding, got %+v", got)
	}
	if f, ok := byEntity["F-0001"]; !ok || !strings.Contains(f.Detail, "plan.md") {
		t.Fatalf("want F-0001 missing plan.md finding (regression), got %+v", got)
	}
}

// TestEpicRulesCountDone (F-0131): done counts the same as implemented in
// both epic rules — it is implemented's human-accepted successor, never less
// concluded.
func TestEpicRulesCountDone(t *testing.T) {
	features := []store.Feature{
		// A done epic with an open child still fires epic-open-children.
		{ID: "F-0001", Title: "Épico aceito", State: domain.StateDone},
		{ID: "F-0002", Title: "Filha em aberto", Parent: "F-0001", State: domain.StateInDevelopment},
		// A done child counts as "started" for epic-behind-children.
		{ID: "F-0003", Title: "Épico não iniciado", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Filha aceita", Parent: "F-0003", State: domain.StateDone},
	}
	got := parentFindings(features)

	want := map[string]string{
		"F-0001": RuleEpicOpenChildren,
		"F-0003": RuleEpicBehindChildren,
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %d (%+v), want %d matching %v", len(got), got, len(want), want)
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
			Detail: `mentions F-0007 ("One"), which is superseded`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `mentions D-0002 ("Usar boleto legado"), which is superseded — use D-0003`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `mentions F-0001 ("Bills"), which is superseded — use F-0002`},
		{Rule: RuleRefToSuperseded, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `mentions R-0001 ("API antiga"), which is obsolete`},
		{Rule: RuleBrokenLink, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Line: 9, Detail: "broken link ../missing.md — no such file: fix the path or remove the link"},
		{Rule: RuleBrokenLink, Entity: "R-0002", File: "content/references/R-0002.md",
			Line: 7, Detail: "broken link ./nada.md — no such file: fix the path or remove the link"},
		{Rule: RuleOrphanContent, Entity: "", File: "content/decisions/D-0099.md",
			Detail: "content without an entity: content/decisions/D-0099.md"},
		{Rule: RuleOrphanContent, Entity: "", File: "content/features/F-0099/spec.md",
			Detail: "content without an entity: content/features/F-0099/spec.md"},
		{Rule: RuleMissingGateDoc, Entity: "F-0004", File: "",
			Detail: "approved feature without a real spec.md"},
		{Rule: RuleMissingGateDoc, Entity: "F-0005", File: "",
			Detail: "implemented feature without a real result.md"},
		{Rule: RuleIncompleteDoc, File: "content/decisions/D-0001.md",
			Detail: "content/decisions/D-0001.md: missing template sections: Contexto, Decisão, Consequências, Alternativas consideradas, Implementação e relações"},
		{Rule: RuleIncompleteDoc, File: "content/features/F-0003/spec.md",
			Detail: "content/features/F-0003/spec.md: missing template sections: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Execução recomendada (agente), Fora de escopo"},
		{Rule: RuleUnregisteredSource, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: "external URL https://example.com cited without a registered reference — register it with 'cerne reference new … --informs F-0003' or remove the URL"},
		{Rule: RuleSpecMissingBar, Entity: "F-0003", File: "content/features/F-0003/spec.md",
			Detail: `no "Barra:" line (Conta digital) — the approve refuses it; 'cerne feature approve F-0003 --check' lists the accepted forms`},
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
		// Live features tagged so untagged-live (now covering features, D-0003)
		// stays silent — this fixture targets the imported_from needle behavior.
		{ID: "F-0090", Title: "Agente autônomo", State: domain.StateSuperseded, SupersededBy: "F-0091"},
		{ID: "F-0091", Title: "Plataforma de agentes", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0092", Title: "Relatório de uso", State: domain.StateCandidate, Facets: []string{"code"}},
		{ID: "F-0093", Title: "Limites de uso", State: domain.StateCandidate, Facets: []string{"code"}},
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
			Detail: `mentions 0090_plugin-autonomous-agent (F-0090 "Agente autônomo"), which is superseded — use F-0091`},
		{Rule: RuleRefToSuperseded, Entity: "F-0091", File: "content/features/F-0091/spec.md",
			Detail: `mentions roadmap-2024 (R-0050 "Roadmap antigo"), which is obsolete`},
		{Rule: RuleRefToSuperseded, Entity: "F-0092", File: "content/features/F-0092/spec.md",
			Detail: `mentions adr-045 (D-0045 "Identity como biblioteca"), which is superseded — use D-0046`},
		{Rule: RuleRefToSuperseded, Entity: "F-0092", File: "content/features/F-0092/spec.md",
			Detail: `mentions case 0090 (F-0090 "Agente autônomo"), which is superseded — use F-0091`},
		{Rule: RuleIncompleteDoc, File: "content/features/F-0091/spec.md",
			Detail: "content/features/F-0091/spec.md: missing template sections: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Execução recomendada (agente), Fora de escopo"},
		{Rule: RuleIncompleteDoc, File: "content/features/F-0092/spec.md",
			Detail: "content/features/F-0092/spec.md: missing template sections: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Execução recomendada (agente), Fora de escopo"},
		{Rule: RuleIncompleteDoc, File: "content/features/F-0093/spec.md",
			Detail: "content/features/F-0093/spec.md: missing template sections: Problema, Objetivo e critérios de aceitação, Decisões do design, Desenho, Verificação, Execução recomendada (agente), Fora de escopo"},
		// The fixture candidates have written specs with no bar (F-0308).
		{Rule: RuleSpecMissingBar, Entity: "F-0091", File: "content/features/F-0091/spec.md",
			Detail: `no "Barra:" line (Plataforma de agentes) — the approve refuses it; 'cerne feature approve F-0091 --check' lists the accepted forms`},
		{Rule: RuleSpecMissingBar, Entity: "F-0092", File: "content/features/F-0092/spec.md",
			Detail: `no "Barra:" line (Relatório de uso) — the approve refuses it; 'cerne feature approve F-0092 --check' lists the accepted forms`},
		{Rule: RuleSpecMissingBar, Entity: "F-0093", File: "content/features/F-0093/spec.md",
			Detail: `no "Barra:" line (Limites de uso) — the approve refuses it; 'cerne feature approve F-0093 --check' lists the accepted forms`},
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

// TestStaleCandidateFindings pins the stale-candidate logic in isolation, with
// now injected so the rule is deterministic. Only a candidate parked past the
// threshold WITHOUT a real spec is noise: a fresh candidate is inside its grace
// window (register-at-start), a stale one that already has a spec is just
// waiting on approve, and a non-candidate is some other rule's concern.
func TestStaleCandidateFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-40 * 24 * time.Hour)
	fresh := now.Add(-5 * 24 * time.Hour)

	features := []store.Feature{
		{ID: "F-0001", Title: "Parada sem spec", State: domain.StateCandidate, CreatedAt: stale},
		{ID: "F-0002", Title: "Recém-criada", State: domain.StateCandidate, CreatedAt: fresh},
		{ID: "F-0003", Title: "Parada com spec", State: domain.StateCandidate, CreatedAt: stale},
		{ID: "F-0004", Title: "Aprovada antiga", State: domain.StateApproved, CreatedAt: stale},
	}
	writeDoc(t, dir, "content/features/F-0003/spec.md",
		"---\ncerne_id: F-0003\n---\n\n# Parada com spec\n\nProblema real, objetivo e critérios de aceitação.\n")

	got, err := staleCandidateFindings(dir, features, now)
	if err != nil {
		t.Fatalf("staleCandidateFindings: %v", err)
	}
	want := []Finding{
		{Rule: RuleStaleCandidate, Entity: "F-0001",
			Detail: "candidate for 40 days without a spec — reject it or advance it with a spec"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// TestRunFlagsStaleCandidate proves the rule is wired into Run: an ancient
// specless candidate surfaces exactly one stale-candidate finding (the day
// count in the detail is relative to wall-clock now, so only Rule/Entity are
// asserted here — the exact-detail contract lives in TestStaleCandidateFindings).
func TestRunFlagsStaleCandidate(t *testing.T) {
	dir := t.TempDir()
	ancient := time.Now().UTC().Add(-365 * 24 * time.Hour)
	f := store.Feature{ID: "F-0001", Title: "Parada", State: domain.StateCandidate, CreatedAt: ancient, UpdatedAt: ancient}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var stale []Finding
	for _, fnd := range got {
		if fnd.Rule == RuleStaleCandidate {
			stale = append(stale, fnd)
		}
	}
	if len(stale) != 1 || stale[0].Entity != "F-0001" {
		t.Fatalf("stale-candidate findings = %+v, want exactly one for F-0001", stale)
	}
}

// TestStaleReadyFindings (F-0131 crit. 6): a fresh implemented feature is
// silent; one older than the window is flagged; a done feature past the same
// age is never flagged (it already crossed the human gate).
func TestStaleReadyFindings(t *testing.T) {
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	stale := now.Add(-20 * 24 * time.Hour)
	fresh := now.Add(-5 * 24 * time.Hour)

	features := []store.Feature{
		{ID: "F-0001", Title: "Ready antiga", State: domain.StateImplemented, UpdatedAt: stale},
		{ID: "F-0002", Title: "Ready recente", State: domain.StateImplemented, UpdatedAt: fresh},
		{ID: "F-0003", Title: "Aceita há muito", State: domain.StateDone, UpdatedAt: stale},
	}

	got := staleReadyFindings(features, 14, now)
	want := []Finding{
		{Rule: RuleStaleReady, Entity: "F-0001",
			Detail: "implemented (Ready) for 20 days without acceptance — review with 'cerne show F-0001 --doc result', then run 'cerne feature accept F-0001' to validate"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

// TestStaleReadyFindingsDefaultWindow: windowDays<=0 falls back to the
// 14-day default, matching project.Config.StaleReadyWindowDays.
func TestStaleReadyFindingsDefaultWindow(t *testing.T) {
	now := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	justOverTwoWeeks := now.Add(-15 * 24 * time.Hour)
	features := []store.Feature{
		{ID: "F-0001", Title: "Ready", State: domain.StateImplemented, UpdatedAt: justOverTwoWeeks},
	}
	got := staleReadyFindings(features, 0, now)
	if len(got) != 1 || got[0].Entity != "F-0001" {
		t.Fatalf("staleReadyFindings with windowDays=0 = %+v, want one finding for F-0001 (14-day default)", got)
	}
}

// TestRunConfigFlagsStaleReady proves the rule is wired into RunConfig: an
// old Ready feature surfaces exactly one stale-ready finding.
func TestRunConfigFlagsStaleReady(t *testing.T) {
	dir := t.TempDir()
	ancient := time.Now().UTC().Add(-30 * 24 * time.Hour)
	f := store.Feature{ID: "F-0001", Title: "Ready", State: domain.StateImplemented, CreatedAt: ancient, UpdatedAt: ancient}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	got, err := RunConfig(dir, 14, 0)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var stale []Finding
	for _, fnd := range got {
		if fnd.Rule == RuleStaleReady {
			stale = append(stale, fnd)
		}
	}
	if len(stale) != 1 || stale[0].Entity != "F-0001" {
		t.Fatalf("stale-ready findings = %+v, want exactly one for F-0001", stale)
	}
}

// TestParentRulesResolveLegacyParent (F-0326): a parent stored in another case
// or padded resolves to its epic for every parent rule in the same run — no
// parent-not-found next to an epic rule that counts the same child, and a
// legacy child of a dead epic is parent-terminal.
func TestParentRulesResolveLegacyParent(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Épico aprovado", State: domain.StateApproved},
		{ID: "F-0002", Title: "Filha legada", Parent: "f-0001", State: domain.StateInDevelopment},
		{ID: "F-0003", Title: "Filha com espaços", Parent: " f-0001 ", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Épico rejeitado", State: domain.StateRejected},
		{ID: "F-0005", Title: "Filha de épico morto", Parent: "f-0004", State: domain.StateCandidate},
	} {
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	rules := map[string]string{}
	for _, f := range got {
		switch f.Rule {
		case RuleParentNotFound, RuleParentTerminal, RuleEpicBehindChildren:
			rules[f.Entity] = f.Rule
		}
	}
	want := map[string]string{"F-0001": RuleEpicBehindChildren, "F-0005": RuleParentTerminal}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("parent rules = %v, want %v", rules, want)
	}
}
