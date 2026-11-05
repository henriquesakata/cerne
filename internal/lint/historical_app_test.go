package lint_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// External test package: internal/app imports internal/lint (F-0303), so a test
// in package lint that calls app would be an import cycle.

func writeLiveFeatureDoc(t *testing.T, dir string) {
	t.Helper()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "F-0001", State: domain.StateInDevelopment, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docDir := filepath.Join(dir, "content", "features", "F-0001")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "spec.md"), []byte("O módulo de boletos roda no Finance."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func countRefToSuperseded(findings []lint.Finding) int {
	n := 0
	for _, f := range findings {
		if f.Rule == lint.RuleRefToSuperseded {
			n++
		}
	}
	return n
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
	writeLiveFeatureDoc(t, dir)

	// Register Bills once via the real app path.
	if _, created, err := app.SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, "", "cli"); err != nil || !created {
		t.Fatalf("first SaveHistoricalEntity: (created=%v, err=%v)", created, err)
	}
	before, err := lint.Run(dir)
	if err != nil {
		t.Fatalf("Run before re-registration: %v", err)
	}
	countBefore := countRefToSuperseded(before)
	if countBefore != 1 {
		t.Fatalf("baseline ref-to-superseded = %d, want 1: %+v", countBefore, countRefToSuperseded(before))
	}

	// Register Bills AGAIN (same name) — pre-fix this created H-0002 and the
	// lint double-counted the "boletos" mention.
	if _, created, err := app.SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, "", "cli"); err != nil || created {
		t.Fatalf("second SaveHistoricalEntity: (created=%v, err=%v), want created=false", created, err)
	}
	if all, _ := store.LoadAllHistoricals(dir); len(all) != 1 {
		t.Fatalf("re-registration leaked entities: %+v", all)
	}
	after, err := lint.Run(dir)
	if err != nil {
		t.Fatalf("Run after re-registration: %v", err)
	}
	countAfter := countRefToSuperseded(after)
	if countAfter != countBefore {
		t.Fatalf("ref-to-superseded count changed across re-registration: %d -> %d (want stable)", countBefore, countAfter)
	}
}
