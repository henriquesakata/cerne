package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

const importSeedTOML = `
[[historical]]
name          = "Bills"
aliases       = ["bills", "boletos"]
superseded_by = "Finance"
reason        = "absorvido pelo Finance"

[[historical]]
name          = "Investments"
aliases       = ["investments"]
superseded_by = "Finance"
reason        = "absorvido pelo Finance"
`

func writeSeed(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "historical.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
}

func TestImportAppliesHistoricalSeed(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	root := t.TempDir() // only the seed, no importable docs
	writeSeed(t, root, importSeedTOML)

	// Dry-run lists the historicals and persists nothing.
	out, err := execCerne(t, "import", "--root", root, "--dir", projDir)
	if err != nil {
		t.Fatalf("import dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Historicals: 2") {
		t.Fatalf("dry-run missing historicals count:\n%s", out)
	}
	if all, _ := store.LoadAllHistoricals(repoDir); len(all) != 0 {
		t.Fatalf("dry-run persisted historicals: %+v", all)
	}

	// Apply creates H-0001/H-0002 and reports them.
	out, err = execCerne(t, "import", "--yes", "--root", root, "--dir", projDir)
	if err != nil {
		t.Fatalf("import apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "2 historicals") {
		t.Fatalf("apply summary missing historicals:\n%s", out)
	}
	all, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals: %v", err)
	}
	if len(all) != 2 || all[0].Name != "Bills" || all[1].Name != "Investments" {
		t.Fatalf("historicals = %+v", all)
	}
	if all[0].State != domain.StateSuperseded {
		t.Fatalf("H-0001 state = %q, want superseded", all[0].State)
	}
}

func TestImportHistoricalSeedIsIdempotent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	root := t.TempDir()
	writeSeed(t, root, importSeedTOML)

	// First apply creates H-0001/H-0002.
	if _, err := execCerne(t, "import", "--yes", "--root", root, "--dir", projDir); err != nil {
		t.Fatalf("first import: %v", err)
	}
	first, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals after first import: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("after first import got %d historicals, want 2: %+v", len(first), first)
	}

	// Re-importing the SAME seed creates zero new entities (upsert by name).
	if _, err := execCerne(t, "import", "--yes", "--root", root, "--dir", projDir); err != nil {
		t.Fatalf("second import: %v", err)
	}
	second, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals after second import: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("after re-import got %d historicals, want 2 (no new entities): %+v", len(second), second)
	}
	if second[0].ID != "H-0001" || second[1].ID != "H-0002" {
		t.Fatalf("re-import allocated new ids: %+v", second)
	}
}

func TestImportSeedShortAliasAbortsAtomically(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	root := t.TempDir()
	writeSeed(t, root, `
[[historical]]
name          = "Bills"
aliases       = ["bills"]
superseded_by = "Finance"
reason        = "ok"

[[historical]]
name          = "Investments"
aliases       = ["abc"]
superseded_by = "Finance"
reason        = "short alias"
`)
	if _, err := execCerne(t, "import", "--yes", "--root", root, "--dir", projDir); err == nil {
		t.Fatalf("import with short alias succeeded, want error")
	}
	if all, _ := store.LoadAllHistoricals(repoDir); len(all) != 0 {
		t.Fatalf("partial seed persisted despite validation error: %+v", all)
	}
}
