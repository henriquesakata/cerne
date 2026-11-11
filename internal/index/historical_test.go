package index

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestSearchFindsHistoricalByAlias(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.SaveHistorical(repoDir, store.Historical{
		ID: "H-0001", Name: "Bills", Aliases: []string{"bills", "boletos"},
		SupersededBy: "Finance", Reason: "absorvido pelo Finance", State: domain.StateSuperseded,
		CreatedAt: time.Unix(0, 0).UTC(),
	}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	res, err := Search(dbPath, "boletos", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(res), res)
	}
	if res[0].EntityID != "H-0001" || res[0].Kind != "historical" ||
		res[0].State != "superseded" || res[0].SupersededBy != "Finance" {
		t.Fatalf("result = %+v", res[0])
	}
}
