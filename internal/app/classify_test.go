package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedFeatureWithSpec persists a feature and writes its spec.md content doc
// (with the given imported_from frontmatter and tags line) into the backend.
func seedFeatureWithSpec(t *testing.T, repoDir, id, module, importedFrom, tagsLine string) {
	t.Helper()
	now := time.Now().UTC()
	f := store.Feature{ID: id, Title: id, Module: module, State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
	body := "---\ncerne_id: " + id + "\n"
	if importedFrom != "" {
		body += "imported_from: " + importedFrom + "\n"
	}
	body += "---\n\n# " + id + "\n\n"
	if tagsLine != "" {
		body += tagsLine + "\n\n"
	}
	body += "Corpo.\n"
	path := filepath.Join(repoDir, "content", "features", id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec %s: %v", id, err)
	}
}

func TestClassifyBackfillDryRun(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo", Modules: []string{"finance", "identity"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// F-0001: module-less, folder points to finance -> assignable.
	seedFeatureWithSpec(t, repoDir, "F-0001", "", "docs/process/cases/0021_finance-consolidation/", "")
	// F-0002: module-less, tag points to identity -> assignable.
	seedFeatureWithSpec(t, repoDir, "F-0002", "", "docs/process/cases/0033_data-migration/", "tags: #spec #identity")
	// F-0003: module-less, no signal -> skipped.
	seedFeatureWithSpec(t, repoDir, "F-0003", "", "docs/process/cases/0050_platform-vision/", "tags: #spec #planning")
	// F-0004: already moduled -> never touched, never counted.
	seedFeatureWithSpec(t, repoDir, "F-0004", "finance", "docs/process/cases/0021_finance-consolidation/", "")

	assigned, skipped, err := ClassifyBackfill(repoDir, "" /*projDir unused in dry-run*/, false)
	if err != nil {
		t.Fatalf("ClassifyBackfill dry-run: %v", err)
	}

	want := []Assignment{{ID: "F-0001", Module: "finance"}, {ID: "F-0002", Module: "identity"}}
	if len(assigned) != len(want) {
		t.Fatalf("assigned = %+v, want %+v", assigned, want)
	}
	for i, w := range want {
		if assigned[i] != w {
			t.Fatalf("assigned[%d] = %+v, want %+v", i, assigned[i], w)
		}
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (F-0003 only; F-0004 already moduled is not counted)", skipped)
	}

	// Dry-run writes nothing: modules unchanged, no events.
	for _, id := range []string{"F-0001", "F-0002", "F-0003"} {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			t.Fatalf("LoadFeature %s: %v", id, err)
		}
		if f.Module != "" {
			t.Fatalf("%s.Module = %q after dry-run, want empty", id, f.Module)
		}
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("dry-run appended %d events, want 0", len(evs))
	}
}

func TestClassifyBackfillReadsImportedFromSpecFrontmatter(t *testing.T) {
	// Features do not persist imported_from on the entity; it must be read from
	// the spec.md frontmatter the importer injected.
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo", Modules: []string{"chronos"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	seedFeatureWithSpec(t, repoDir, "F-0001", "", "docs/process/cases/0101_chronos-subsystem-extraction/", "")

	assigned, skipped, err := ClassifyBackfill(repoDir, "", false)
	if err != nil {
		t.Fatalf("ClassifyBackfill: %v", err)
	}
	if len(assigned) != 1 || assigned[0] != (Assignment{ID: "F-0001", Module: "chronos"}) || skipped != 0 {
		t.Fatalf("assigned = %+v, skipped = %d, want F-0001->chronos and 0 skipped", assigned, skipped)
	}
}
