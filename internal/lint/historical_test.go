package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// writeLiveFeatureDoc creates an in_development feature with a single content
// doc carrying body, so the text rules scan it.
func writeLiveFeatureDoc(t *testing.T, dir, id, body string) {
	t.Helper()
	if err := store.SaveFeature(dir, store.Feature{ID: id, Title: id, State: domain.StateInDevelopment, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docDir := filepath.Join(dir, "content", "features", id)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "spec.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func refToSuperseded(findings []Finding) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == RuleRefToSuperseded {
			out = append(out, f)
		}
	}
	return out
}

func TestLintFlagsHistoricalAlias(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveHistorical(dir, store.Historical{
		ID: "H-0001", Name: "Bills", Aliases: []string{"bills", "boletos"},
		SupersededBy: "Finance", Reason: "absorvido", State: domain.StateSuperseded,
		CreatedAt: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	// Doc mentions the alias "boletos" (not the name), plus the successor.
	writeLiveFeatureDoc(t, dir, "F-0001", "O módulo de boletos roda no Finance.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	refs := refToSuperseded(findings)
	if len(refs) != 1 {
		t.Fatalf("got %d ref-to-superseded findings, want 1: %+v", len(refs), refs)
	}
	if refs[0].File != "content/features/F-0001/spec.md" || refs[0].Entity != "F-0001" {
		t.Fatalf("finding location wrong: %+v", refs[0])
	}
}

// TestLintDuplicateHistoricalsDoubleCount documents the inflation mechanism the
// case 0004 upsert removes: two distinct historical entities sharing the same
// alias each contribute a needle, so a single mention is flagged once per
// duplicate. This is what produced the 89 -> 160 jump before the upsert; the
// fix guarantees only one entity per normalized name ever exists.
func TestLintDuplicateHistoricalsDoubleCount(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"H-0001", "H-0002"} {
		if err := store.SaveHistorical(dir, store.Historical{
			ID: id, Name: "Bills", Aliases: []string{"bills", "boletos"},
			SupersededBy: "Finance", Reason: "absorvido", State: domain.StateSuperseded,
			CreatedAt: time.Unix(0, 0).UTC(),
		}); err != nil {
			t.Fatalf("SaveHistorical %s: %v", id, err)
		}
	}
	writeLiveFeatureDoc(t, dir, "F-0001", "O módulo de boletos roda no Finance.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 2 {
		t.Fatalf("with two duplicate historicals want 2 findings (the inflation), got %d: %+v", len(refs), refs)
	}
}

func TestLintHistoricalWordBoundaryAndSuccessor(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveHistorical(dir, store.Historical{
		ID: "H-0001", Name: "Bills", Aliases: []string{"bills", "boletos"},
		SupersededBy: "Finance", Reason: "absorvido", State: domain.StateSuperseded,
		CreatedAt: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	// "billships" is a superstring (no word boundary); "Finance" is the
	// successor, never a needle. Neither must be flagged.
	writeLiveFeatureDoc(t, dir, "F-0001", "billships sail to Finance every day.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("want no ref-to-superseded findings, got: %+v", refs)
	}
}

// TestLintHistoricalNameNotMatchedAsTitle pins F-0092: a historical entity
// whose Name is a common domain word ("Investments") must NOT turn every prose
// use of that word into a ref-to-superseded. Only the id and specific aliases
// identify the retired entity; the generic Name is a label, not a matcher.
func TestLintHistoricalNameNotMatchedAsTitle(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveHistorical(dir, store.Historical{
		ID: "H-0002", Name: "Investments", Aliases: []string{"invest-app"},
		SupersededBy: "Finance", Reason: "consolidado", State: domain.StateSuperseded,
		CreatedAt: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	// Prose uses the domain word "investments" — not the id, not the alias.
	writeLiveFeatureDoc(t, dir, "F-0001", "Os investments agora vivem no Finance.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("common-word Name must not flag prose, got %d: %+v", len(refs), refs)
	}
}

// TestLintHistoricalFlaggedByID guards criterion 2: dropping the Name needle
// must not regress detection by id — a live doc naming H-0002 is still flagged.
func TestLintHistoricalFlaggedByID(t *testing.T) {
	dir := t.TempDir()
	if err := store.SaveHistorical(dir, store.Historical{
		ID: "H-0002", Name: "Investments", Aliases: []string{"invest-app"},
		SupersededBy: "Finance", Reason: "consolidado", State: domain.StateSuperseded,
		CreatedAt: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	writeLiveFeatureDoc(t, dir, "F-0001", "Migrar o legado H-0002 para o Finance.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 1 {
		t.Fatalf("id mention must stay flagged, got %d: %+v", len(refs), refs)
	}
}

// TestLintHistoricalAliasCurationClears proves the end-to-end effect of the
// forget-alias curation: an entity with a noisy alias flags a doc using that
// word; once the alias is dropped, the finding is gone. (Simulates the post-
// curation store state — ForgetHistoricalAliases' git sync is exercised via the
// CLI path, not here.)
func TestLintHistoricalAliasCurationClears(t *testing.T) {
	dir := t.TempDir()
	save := func(aliases []string) {
		t.Helper()
		if err := store.SaveHistorical(dir, store.Historical{
			ID: "H-0001", Name: "Bills", Aliases: aliases,
			SupersededBy: "Finance", Reason: "absorvido", State: domain.StateSuperseded,
			CreatedAt: time.Unix(0, 0).UTC(),
		}); err != nil {
			t.Fatalf("SaveHistorical: %v", err)
		}
	}
	writeLiveFeatureDoc(t, dir, "F-0001", "O fluxo de boletos roda no Finance.")

	save([]string{"bills", "boletos"})
	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 1 {
		t.Fatalf("pre-curation want 1 ref-to-superseded (alias boletos), got %d: %+v", len(refs), refs)
	}

	save([]string{"bills"}) // forget "boletos"
	findings, err = Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("post-curation want 0 ref-to-superseded, got: %+v", refs)
	}
}
