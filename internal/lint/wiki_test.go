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

func byRule(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestLintWikiRules(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	// A superseded feature provides a ref-to-superseded needle by its title.
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0002", Title: "VelhoModulo", State: domain.StateSuperseded, SupersededBy: "F-0003", CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	// A compiled wiki page: provenance lists an unknown source AND the body
	// mentions the superseded entity.
	body := wiki.RenderProvenance(wiki.Provenance{Page: "project", Sources: []string{"F-9999"}, CompiledFrom: "x", InputHash: "y"}) +
		"\n# Projeto\n\nO VelhoModulo foi absorvido por outro.\n"
	p := filepath.Join(dir, "content", "wiki", "project.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write wiki: %v", err)
	}

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	unknown := byRule(findings, RuleWikiSourceUnknown)
	if len(unknown) != 1 || unknown[0].File != "content/wiki/project.md" {
		t.Fatalf("wiki-source-unknown = %+v, want 1 on content/wiki/project.md", unknown)
	}
	if !contains(unknown[0].Detail, "F-9999") {
		t.Fatalf("wiki-source-unknown detail missing F-9999: %q", unknown[0].Detail)
	}

	refs := byRule(findings, RuleRefToSuperseded)
	var onWiki bool
	for _, f := range refs {
		if f.File == "content/wiki/project.md" {
			onWiki = true
		}
	}
	if !onWiki {
		t.Fatalf("ref-to-superseded did not scan the wiki page: %+v", refs)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
