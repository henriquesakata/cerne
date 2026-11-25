package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0500 crit. 7: whatever is wrong with the entity behind an imported doc,
// the importer never aborts: the whole source counts as imported.
func TestImportedItemsFallsBackToSourceOnly(t *testing.T) {
	for name, tt := range map[string]struct {
		frontmatter string
		entityJSON  string // written to the entity file when not empty
	}{
		"no cerne_id":        {"imported_from: CLAUDE.md\n", ""},
		"malformed cerne_id": {"cerne_id: N-../../x\nimported_from: CLAUDE.md\n", ""},
		"corrupt entity":     {"cerne_id: N-0001\nimported_from: CLAUDE.md\n", "{not json"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
				t.Fatal(err)
			}
			doc := filepath.Join(repo, "content", "notes", "N-0001.md")
			if err := os.MkdirAll(filepath.Dir(doc), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(doc, []byte("---\n"+tt.frontmatter+"---\n\nbody\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.entityJSON != "" {
				ent := filepath.Join(repo, "entities", "notes", "N-0001.json")
				if err := os.MkdirAll(filepath.Dir(ent), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(ent, []byte(tt.entityJSON), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			kept, skipped, err := FilterPlanItems(repo, []PlanItem{{Kind: "note", Title: "Any", State: "active", Body: "b", Source: "CLAUDE.md"}})
			if err != nil {
				t.Fatalf("FilterPlanItems aborted: %v", err)
			}
			if skipped != 1 || len(kept) != 0 {
				t.Fatalf("kept=%+v skipped=%d, want the item skipped", kept, skipped)
			}
		})
	}
}
