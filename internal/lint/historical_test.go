package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
