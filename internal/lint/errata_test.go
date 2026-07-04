package lint

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestLintScansErrataOfImplementedFeature(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0002 superseded → a ref-to-superseded needle.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "Velho", State: domain.StateSuperseded, SupersededBy: "F-0003", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0002: %v", err)
	}
	// F-0001 IMPLEMENTED (its spec is NOT scanned) whose errata mentions F-0002.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	if err := store.AppendErrata(dir, "F-0001", "esta feature ainda menciona F-0002", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range refToSuperseded(findings) {
		if f.File == "content/errata/F-0001.md" && contains(f.Detail, "F-0002") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a ref-to-superseded finding on the implemented feature's errata, got: %+v", refToSuperseded(findings))
	}
}

func TestLintCleanWhenErrataMentionsLiveOnly(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0003", Title: "Novo", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0003: %v", err)
	}
	if err := store.AppendErrata(dir, "F-0001", "ver F-0003 para o fluxo atual", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("errata mentioning only live ids must produce no finding, got: %+v", refs)
	}
}

// TestLintErrataToleratesOwnPredecessor pins F-0094: an entity's errata may name
// what that entity superseded (the supersession record) without being flagged —
// errata cannot carry the F-0089 marker, so this self-supersession case is
// tolerated. A stale ref to some OTHER superseded id still surfaces
// (TestLintScansErrataOfImplementedFeature covers that — successor != entity).
func TestLintErrataToleratesOwnPredecessor(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0002 superseded BY F-0001 — the errata's own entity.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "Velho", State: domain.StateSuperseded, SupersededBy: "F-0001", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0002: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Novo", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	// F-0001's errata documents that it superseded F-0002 — legitimate history.
	if err := store.AppendErrata(dir, "F-0001", "esta feature supersedeu F-0002", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("errata naming what its own entity superseded must be tolerated, got: %+v", refs)
	}
}

// TestLintErrataToleratesSelfMentionWhenRetired pins the retire case: an
// entity retired with no successor (superseded, SupersededBy empty) still has
// its own errata, whose frontmatter carries cerne_id: <id>. That self-mention
// is a record, not stale debt — and unlike F-0094 the mention's successor is
// the entity itself with no `by`, so the predecessor tolerance cannot fire.
func TestLintErrataToleratesSelfMentionWhenRetired(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// RB-0003 retired: superseded with NO successor (SupersededBy empty).
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0003", Title: "CI/CD — Pipeline", State: domain.RunbookSuperseded, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveRunbook: %v", err)
	}
	// Its own errata (frontmatter cerne_id: RB-0003 names the entity itself).
	if err := store.AppendErrata(dir, "RB-0003", "reclassificado como N-0117", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range refToSuperseded(findings) {
		if f.File == "content/errata/RB-0003.md" {
			t.Fatalf("a retired entity's own errata self-mention must be tolerated, got: %+v", refToSuperseded(findings))
		}
	}
}

// TestLintErrataToleratesOwnPredecessorByTitle pins F-0094 crit. 3: the
// self-supersession tolerance is per-entity, not per-match-kind — an errata that
// names its predecessor by TITLE (not id) is tolerated just the same.
func TestLintErrataToleratesOwnPredecessorByTitle(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0002 ("Fluxo Legado") superseded BY F-0001 — the errata's own entity.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "Fluxo Legado", State: domain.StateSuperseded, SupersededBy: "F-0001", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0002: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Novo", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	// The errata names the predecessor by its title only — no id in the text.
	if err := store.AppendErrata(dir, "F-0001", "esta feature substituiu o Fluxo Legado", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("errata naming its predecessor by title must be tolerated, got: %+v", refs)
	}
}

// TestLintErrataMixedOwnAndForeignSuperseded pins F-0094 crit. 1+2: the
// tolerance is per-needle. One errata may name both its own predecessor
// (tolerated) and a foreign superseded id whose successor is someone else
// (still a debt) — only the foreign one surfaces.
func TestLintErrataMixedOwnAndForeignSuperseded(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0002 superseded BY F-0001 (the errata owner) — tolerated.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "Velho", State: domain.StateSuperseded, SupersededBy: "F-0001", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0002: %v", err)
	}
	// F-0003 superseded BY F-0004 (a third party) — still a debt.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0003", Title: "Outro", State: domain.StateSuperseded, SupersededBy: "F-0004", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0003: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Novo", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	// One entry names both: its own predecessor F-0002 (ok) and foreign F-0003 (debt).
	if err := store.AppendErrata(dir, "F-0001", "substituiu F-0002; mas ainda aponta para F-0003", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	refs := refToSuperseded(findings)
	if len(refs) != 1 {
		t.Fatalf("want exactly 1 ref-to-superseded (the foreign F-0003), got %d: %+v", len(refs), refs)
	}
	if !contains(refs[0].Detail, "F-0003") {
		t.Fatalf("the surviving finding must be about F-0003, got: %+v", refs[0])
	}
}

// TestLintErrataToleratesOwnPredecessorDecision pins F-0094 crit. 4: the
// tolerance is generic across errata-bearing kinds, not feature-only — a
// decision's errata may name the decision it superseded.
func TestLintErrataToleratesOwnPredecessorDecision(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// D-0002 superseded BY D-0001 — the errata's own decision.
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0002", Title: "Antiga", State: domain.DecisionSuperseded, SupersededBy: "D-0001", CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0002: %v", err)
	}
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0001", Title: "Vigente", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0001: %v", err)
	}
	if err := store.AppendErrata(dir, "D-0001", "esta decisão substituiu D-0002", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("a decision errata naming what it superseded must be tolerated, got: %+v", refs)
	}
}

// TestLintErrataBrokenLinkStillFlagged pins F-0094 crit. 6: the tolerance is
// scoped to ref-to-superseded only — a broken relative link in an errata must
// still surface (errata stays scanned by the other rules, per F-0072).
func TestLintErrataBrokenLinkStillFlagged(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature F-0001: %v", err)
	}
	if err := store.AppendErrata(dir, "F-0001", "veja [detalhes](./inexistente.md)", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("AppendErrata: %v", err)
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range byRule(findings, RuleBrokenLink) {
		if f.File == "content/errata/F-0001.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a broken link in an errata must still be flagged, got: %+v", byRule(findings, RuleBrokenLink))
	}
}
