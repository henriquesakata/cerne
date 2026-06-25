package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// writeWikiPage writes a compiled wiki page with the given provenance sources
// and body (the agent's synthesis).
func writeWikiPage(t *testing.T, dir, rel string, sources []string, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := wiki.RenderProvenance(wiki.Provenance{Page: "p", Sources: sources, CompiledFrom: "x", InputHash: "y"}) + "\n" + body
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write wiki page: %v", err)
	}
}

// supersededDecisionPair seeds D-0003 (superseded by the active D-0002).
func supersededDecisionPair(t *testing.T, dir string) {
	t.Helper()
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0003", Title: "ADR velho", State: domain.DecisionSuperseded, SupersededBy: "D-0002", CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0003: %v", err)
	}
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0002", Title: "ADR novo", State: domain.DecisionActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveDecision D-0002: %v", err)
	}
}

func TestWikiDocCarriesSources(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	writeWikiPage(t, dir, "content/wiki/modules/finance.md", []string{"F-0008"}, "# Finance\n")
	docs, err := wikiDocs(dir)
	if err != nil {
		t.Fatalf("wikiDocs: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("want 1 wiki doc, got %d", len(docs))
	}
	if docs[0].entity != "" {
		t.Errorf("wiki doc entity = %q, want empty", docs[0].entity)
	}
	if len(docs[0].sources) != 1 || docs[0].sources[0] != "F-0008" {
		t.Errorf("wiki doc sources = %v, want [F-0008]", docs[0].sources)
	}
}

func TestWikiRefAttributedToSource(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	// Wiki page sourced from F-0008; body cites the superseded D-0003 unmarked.
	writeWikiPage(t, dir, "content/wiki/modules/finance.md", []string{"F-0008"}, "# Finance\n\nBaseado em D-0003.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range refToSuperseded(findings) {
		if f.Entity == "F-0008" && f.File == "content/wiki/modules/finance.md" && contains(f.Detail, "D-0003") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a ref-to-superseded finding attributed to F-0008 citing D-0003, got: %+v", refToSuperseded(findings))
	}
}
