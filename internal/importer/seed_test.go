package importer

import (
	"os"
	"path/filepath"
	"testing"
)

const seedTOML = `
[[historical]]
name          = "Bills"
aliases       = ["bills", "boletos"]
superseded_by = "Finance"
reason        = "Módulo de boletos absorvido pelo Finance."

[[historical]]
name          = "Investments"
aliases       = ["investments", "investimentos"]
superseded_by = "Finance"
reason        = "Módulo de investimentos absorvido pelo Finance."
`

func TestLoadHistoricalSeed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "historical.toml"), []byte(seedTOML), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	seeds, err := LoadHistoricalSeed(root)
	if err != nil {
		t.Fatalf("LoadHistoricalSeed: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("got %d seeds, want 2", len(seeds))
	}
	if seeds[0].Name != "Bills" || seeds[0].SupersededBy != "Finance" ||
		len(seeds[0].Aliases) != 2 || seeds[0].Aliases[1] != "boletos" {
		t.Fatalf("seed[0] = %+v", seeds[0])
	}
	if seeds[1].Name != "Investments" {
		t.Fatalf("seed[1] = %+v", seeds[1])
	}
}

func TestLoadHistoricalSeedMissing(t *testing.T) {
	seeds, err := LoadHistoricalSeed(t.TempDir())
	if err != nil {
		t.Fatalf("missing seed should not error: %v", err)
	}
	if seeds != nil {
		t.Fatalf("missing seed = %+v, want nil", seeds)
	}
}

func TestLoadHistoricalSeedInvalid(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "historical.toml"), []byte("this is = not [ valid toml"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadHistoricalSeed(root); err == nil {
		t.Fatalf("invalid toml should error")
	}
}
