package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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
	if !strings.Contains(out, "Históricas: 2") {
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
	if !strings.Contains(out, "2 históricas") {
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
