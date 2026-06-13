package importer

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

func TestImportedSources(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A feature's two docs share one source; a decision has its own; a
	// hand-authored reference has no imported_from and must not register.
	write("content/features/F-0001/spec.md", "---\ncerne_id: F-0001\nimported_from: docs/process/cases/0001_x/\n---\n# X")
	write("content/features/F-0001/result.md", "---\ncerne_id: F-0001\nimported_from: docs/process/cases/0001_x/\n---\n# X")
	write("content/decisions/D-0001.md", "---\ncerne_id: D-0001\nimported_from: docs/architecture/adr/adr-001-y.md\n---\n# Y")
	write("content/references/R-0001.md", "# sem frontmatter")

	got, err := ImportedSources(repoDir)
	if err != nil {
		t.Fatalf("ImportedSources: %v", err)
	}
	want := map[string]bool{
		"docs/process/cases/0001_x/":         true,
		"docs/architecture/adr/adr-001-y.md": true,
	}
	if len(got) != len(want) {
		t.Fatalf("ImportedSources = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("ImportedSources missing %q (got %v)", k, got)
		}
	}
}

func TestUnimported(t *testing.T) {
	cands := []Candidate{
		{Kind: "feature", Source: "docs/process/cases/0001_x/"},
		{Kind: "decision", Source: "docs/architecture/adr/adr-001-y.md"},
		{Kind: "reference", Source: "docs/runbooks/new.md"},
	}
	imported := map[string]bool{
		"docs/process/cases/0001_x/":         true,
		"docs/architecture/adr/adr-001-y.md": true,
	}
	kept, skipped := Unimported(cands, imported)
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
	if len(kept) != 1 || kept[0].Source != "docs/runbooks/new.md" {
		t.Errorf("kept = %+v, want only docs/runbooks/new.md", kept)
	}
}

func TestImportedSourcesEmptyBackend(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	got, err := ImportedSources(repoDir)
	if err != nil {
		t.Fatalf("ImportedSources: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ImportedSources on fresh backend = %v, want empty", got)
	}
}
