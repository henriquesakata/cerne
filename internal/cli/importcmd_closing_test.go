package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/guide"
)

// assertImportClosing checks the shared closing: what came in by kind and by
// state, the example search, the first-feature ask and the guide address.
func assertImportClosing(t *testing.T, out string, wantLines []string, wantSearch string) {
	t.Helper()
	for _, want := range append(wantLines,
		"What came in:",
		"Next steps:",
		"cerne search \""+wantSearch+"\"",
		guide.FirstFeatureAsk,
		guide.FirstWeekURL,
	) {
		if !strings.Contains(out, want) {
			t.Errorf("closing missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, guide.FirstWeekURL) < strings.Index(out, "Next steps:") {
		t.Errorf("guide address comes before the next steps:\n%s", out)
	}
}

func TestImportCaseFolderClosing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())
	out, err := execCerne(t, "import", "--root", treeDir, "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	assertImportClosing(t, out, []string{
		"features 2: 1 candidate, 1 implemented",
		"decisions 1: 1 active",
		"runbooks 1",
	}, "Login")

	// Delivered as done shows in the by-state breakdown.
	projDir, _, _ = setupProject(t)
	out, err = execCerne(t, "import", "--root", treeDir, "--yes", "--delivered-state", "done", "--dir", projDir)
	if err != nil {
		t.Fatalf("import done: %v\n%s", err, out)
	}
	if !strings.Contains(out, "features 2: 1 candidate, 1 done") {
		t.Errorf("by-state breakdown missing done:\n%s", out)
	}
}

func TestImportAgentIngestClosing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeAgentPlan(t, projDir, gabaritoPlan)
	out, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	assertImportClosing(t, out, []string{
		"features 1: 1 implemented",
		"references 1: 1 current",
		"notes 1: 1 active",
	}, "Detecção")
}

func TestImportDryRunHasNoClosing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())
	out, err := execCerne(t, "import", "--root", treeDir, "--dir", projDir)
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if strings.Contains(out, "What came in:") {
		t.Errorf("dry-run printed the closing:\n%s", out)
	}
}
