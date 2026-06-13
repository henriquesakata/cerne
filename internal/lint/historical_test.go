package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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

// TestLintRefCountStableAcrossReRegistration is the regression for the exact
// case 0004 bug: re-running `retire` for an already-registered historical
// entity used to create a duplicate H- entity, and the lint counted the SAME
// mention once per duplicate needle (on the hub corpus ref-to-superseded
// jumped 89 -> 160 from a single duplicate retire). With the upsert in place a
// second registration of the same name updates the one entity, so the lint
// count is unchanged.
func TestLintRefCountStableAcrossReRegistration(t *testing.T) {
	dir := t.TempDir()
	// A live feature doc mentioning the alias "boletos" once (the alias, not
	// the entity name nor the successor "Finance").
	writeLiveFeatureDoc(t, dir, "F-0001", "O módulo de boletos roda no Finance.")

	// Register Bills once via the real app path.
	if _, created, err := app.SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, "", "cli"); err != nil || !created {
		t.Fatalf("first SaveHistoricalEntity: (created=%v, err=%v)", created, err)
	}
	before, err := Run(dir)
	if err != nil {
		t.Fatalf("Run before re-registration: %v", err)
	}
	countBefore := len(refToSuperseded(before))
	if countBefore != 1 {
		t.Fatalf("baseline ref-to-superseded = %d, want 1: %+v", countBefore, refToSuperseded(before))
	}

	// Register Bills AGAIN (same name) — pre-fix this created H-0002 and the
	// lint double-counted the "boletos" mention.
	if _, created, err := app.SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, "", "cli"); err != nil || created {
		t.Fatalf("second SaveHistoricalEntity: (created=%v, err=%v), want created=false", created, err)
	}
	if all, _ := store.LoadAllHistoricals(dir); len(all) != 1 {
		t.Fatalf("re-registration leaked entities: %+v", all)
	}
	after, err := Run(dir)
	if err != nil {
		t.Fatalf("Run after re-registration: %v", err)
	}
	countAfter := len(refToSuperseded(after))
	if countAfter != countBefore {
		t.Fatalf("ref-to-superseded count changed across re-registration: %d -> %d (want stable)", countBefore, countAfter)
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
