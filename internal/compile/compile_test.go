package compile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

func seedBackend(t *testing.T, modules ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo", Modules: modules}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	return dir
}

func addFeature(t *testing.T, dir, id, module string, state domain.FeatureState, spec string) {
	t.Helper()
	if err := store.SaveFeature(dir, store.Feature{ID: id, Title: id, Module: module, State: state, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
	if spec != "" {
		p := filepath.Join(dir, "content", "features", id, "spec.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(spec), 0o644); err != nil {
			t.Fatalf("write spec: %v", err)
		}
	}
}

func pageByName(pages []Page, name string) (Page, bool) {
	for _, p := range pages {
		if p.Name == name {
			return p, true
		}
	}
	return Page{}, false
}

func sourceIDs(p Page) []string {
	var ids []string
	for _, s := range p.Sources {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestPagesMultiModule(t *testing.T) {
	dir := seedBackend(t, "finance", "core")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nspec.\n")
	addFeature(t, dir, "F-0002", "core", domain.StateCandidate, "# F-0002\n\nspec.\n")
	addFeature(t, dir, "F-0003", "finance", domain.StateSuperseded, "# F-0003\n\nold.\n") // not live
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0001", Title: "ADR um", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := store.SaveReference(dir, store.Reference{ID: "R-0001", Title: "Ref um", State: domain.ReferenceCurrent, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	fin, ok := pageByName(pages, "finance")
	if !ok || len(fin.Sources) != 1 || fin.Sources[0].ID != "F-0001" {
		t.Fatalf("finance page = %+v", fin)
	}
	core, ok := pageByName(pages, "core")
	if !ok || len(core.Sources) != 1 || core.Sources[0].ID != "F-0002" {
		t.Fatalf("core page = %+v", core)
	}
	proj, ok := pageByName(pages, "project")
	if !ok {
		t.Fatalf("missing project page")
	}
	got := sourceIDs(proj)
	if len(got) != 2 || got[0] != "D-0001" || got[1] != "R-0001" {
		t.Fatalf("project sources = %v, want [D-0001 R-0001]", got)
	}
}

func TestPagesNoModules(t *testing.T) {
	dir := seedBackend(t)
	addFeature(t, dir, "F-0001", "", domain.StateCandidate, "# F-0001\n\nspec.\n")
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0001", Title: "ADR", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if len(pages) != 1 || pages[0].Name != "project" {
		t.Fatalf("pages = %+v, want single project page", pages)
	}
	if ids := sourceIDs(pages[0]); len(ids) != 2 || ids[0] != "D-0001" || ids[1] != "F-0001" {
		t.Fatalf("project sources = %v, want [D-0001 F-0001]", ids)
	}
}

// writeCompiled simulates a previously compiled page whose provenance matches
// the page's current input hash.
func writeCompiled(t *testing.T, dir string, p Page) {
	t.Helper()
	prov := wiki.Provenance{Page: p.Name, Sources: sourceIDs(p), CompiledFrom: "sha", InputHash: p.InputHash()}
	full := filepath.Join(dir, "content", "wiki", filepath.FromSlash(p.Rel)+".md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(wiki.RenderProvenance(prov)+"\n# "+p.Name+"\n\nsynthesis.\n"), 0o644); err != nil {
		t.Fatalf("write compiled: %v", err)
	}
}

func TestDirtyTracking(t *testing.T) {
	dir := seedBackend(t, "finance", "core")
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nspec.\n")
	addFeature(t, dir, "F-0002", "core", domain.StateCandidate, "# F-0002\n\nspec.\n")

	pages, err := Pages(dir)
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	// Nothing compiled yet -> every page dirty.
	d, err := Dirty(dir, pages, false)
	if err != nil {
		t.Fatalf("Dirty: %v", err)
	}
	if len(d) != len(pages) {
		t.Fatalf("initial dirty = %d, want all %d", len(d), len(pages))
	}

	// Compile finance -> only finance becomes clean.
	fin, _ := pageByName(pages, "finance")
	writeCompiled(t, dir, fin)
	d, _ = Dirty(dir, pages, false)
	if _, dirty := pageByName(d, "finance"); dirty {
		t.Fatalf("finance still dirty after compile: %+v", d)
	}

	// Editing the finance feature makes finance dirty again.
	addFeature(t, dir, "F-0001", "finance", domain.StateApproved, "# F-0001\n\nEDITED.\n")
	pages, _ = Pages(dir)
	d, _ = Dirty(dir, pages, false)
	if _, dirty := pageByName(d, "finance"); !dirty {
		t.Fatalf("finance not dirty after content edit")
	}

	// Force marks everything dirty.
	d, _ = Dirty(dir, pages, true)
	if len(d) != len(pages) {
		t.Fatalf("force dirty = %d, want %d", len(d), len(pages))
	}
}

func TestBuildPagePrompt(t *testing.T) {
	p := Page{Name: "finance", Rel: "modules/finance", Sources: []EntitySource{
		{ID: "F-0001", Kind: "feature", State: "approved", Title: "Boletos", Content: "# Boletos\n\nGera boletos.\n"},
	}}
	prompt := BuildPagePrompt(p)
	for _, want := range []string{"finance", "F-0001", "approved", "Boletos", "Gera boletos"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
