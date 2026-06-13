package materialize

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestMaterializeExcludesHistorical pins the contract that historical
// entities are history: they never reach .docs/ nor CONTEXT.md. Expected to
// pass without production changes — materialize only iterates features,
// decisions and references — so it guards against a future regression.
func TestMaterializeExcludesHistorical(t *testing.T) {
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Painel", "", domain.StateInDevelopment, map[string]string{
		"spec.md": "---\ncerne_id: F-0001\n---\n\n# Painel\n\nConteúdo real do painel.\n",
	})
	if err := store.SaveHistorical(backend, store.Historical{
		ID: "H-0001", Name: "Bills", Aliases: []string{"bills", "boletos"},
		SupersededBy: "Finance", Reason: "absorvido", State: domain.StateSuperseded,
		CreatedAt: seedTime,
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, ""); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	ctx := readContext(t, docsDir)
	for _, banned := range []string{"Bills", "H-0001", "boletos", "Finance"} {
		if strings.Contains(ctx, banned) {
			t.Fatalf("CONTEXT.md leaks historical %q:\n%s", banned, ctx)
		}
	}

	err := filepath.WalkDir(docsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(p, "H-0001") || strings.Contains(strings.ToLower(p), "bills") {
			t.Fatalf("materialized path references historical: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}
