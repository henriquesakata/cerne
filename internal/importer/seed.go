package importer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// historicalSeedFile is the optional declarative file, at the scan root, that
// seeds historical entities in batch during import.
const historicalSeedFile = "historical.toml"

// HistoricalSeed is one historical-entity entry declared in historical.toml.
// It mirrors the fields of a `cerne retire` invocation so the seed and the
// command create identical entities.
type HistoricalSeed struct {
	Name         string   `toml:"name"`
	Aliases      []string `toml:"aliases"`
	SupersededBy string   `toml:"superseded_by"`
	Reason       string   `toml:"reason"`
}

// LoadHistoricalSeed reads <root>/historical.toml and returns its entries. A
// missing file yields (nil, nil) — the seed is optional; a malformed file is
// an error.
func LoadHistoricalSeed(root string) ([]HistoricalSeed, error) {
	data, err := os.ReadFile(filepath.Join(root, historicalSeedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Historical []HistoricalSeed `toml:"historical"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Historical, nil
}
