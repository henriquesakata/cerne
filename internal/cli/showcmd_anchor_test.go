package cli

import (
	"strings"
	"testing"
)

// F-0500 crit. 8: `cerne show` prints where an agent-imported entity's
// evidence sits, in text and in --json.
func TestShowImportedEntityShowsAnchor(t *testing.T) {
	projDir, _, _ := setupProject(t)
	writeTree(t, projDir, frigateTree())
	writeAgentPlan(t, projDir, gabaritoPlan)
	if out, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir); err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}

	out, err := execCerne(t, "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	for _, want := range []string{"\nimported from: CLAUDE.md\n", "\nanchor: CLAUDE.md:3-3\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("show missing %q:\n%s", want, out)
		}
	}

	out, err = execCerne(t, "show", "N-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"imported_anchor": "CLAUDE.md:3-3"`) {
		t.Errorf("show --json missing imported_anchor:\n%s", out)
	}
}

// An entity that was not imported shows no provenance lines.
func TestShowNativeEntityHasNoAnchor(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "note", "new", "A note", "--dir", projDir); err != nil {
		t.Skipf("cannot create a native note here: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "imported from:") || strings.Contains(out, "anchor:") {
		t.Fatalf("native entity shows provenance:\n%s", out)
	}
}
