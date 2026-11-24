package cli

import (
	"os"
	"path/filepath"
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

// An entity that was not imported, or was imported by the case-folder
// convention (imported_from, no imported_anchor), shows exactly what it did
// before: no provenance lines.
func TestShowEntityWithoutAnchorShowsNoProvenance(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "note", "new", "A note", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\n%s", err, out)
	}
	check := func(label string) {
		t.Helper()
		out, err := execCerne(t, "show", "N-0001", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\nimported from:") || strings.Contains(out, "\nanchor:") {
			t.Fatalf("%s: shows provenance:\n%s", label, out)
		}
		out, err = execCerne(t, "show", "N-0001", "--json", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, `"imported_from"`) || strings.Contains(out, `"imported_anchor"`) {
			t.Fatalf("%s: --json carries provenance:\n%s", label, out)
		}
	}
	check("native")

	doc := filepath.Join(repoDir, "content", "notes", "N-0001.md")
	body := "---\ncerne_id: N-0001\nimported_from: docs/old.md\n---\n\n# Old note\n\nSome real body text.\n"
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	check("imported by convention")
}
