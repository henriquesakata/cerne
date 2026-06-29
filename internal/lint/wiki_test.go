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

	// F-0090: ref-to-superseded is an authored-content rule and must NOT scan
	// the generated wiki page (it regenerates from source; the equivalent
	// authored mention is flagged at its own doc).
	refs := byRule(findings, RuleRefToSuperseded)
	for _, f := range refs {
		if f.File == "content/wiki/project.md" {
			t.Fatalf("ref-to-superseded must not scan the generated wiki page: %+v", refs)
		}
	}
}

// The wiki-source-unknown rule is page-name agnostic: it must flag a bogus
// source on the cross-cutting decisions/references pages introduced by case
// 0006 exactly as it does on the project/module pages.
func TestLintWikiRuleCoversDecisionsReferencesPages(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo", Modules: []string{"finance"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("content/wiki/decisions.md",
		wiki.RenderProvenance(wiki.Provenance{Page: "decisions", Sources: []string{"D-9999"}, CompiledFrom: "x", InputHash: "y"})+
			"\n# Decisões\n\nSíntese.\n")
	write("content/wiki/references.md",
		wiki.RenderProvenance(wiki.Provenance{Page: "references", Sources: []string{"R-8888"}, CompiledFrom: "x", InputHash: "y"})+
			"\n# Referências\n\nSíntese.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	unknown := byRule(findings, RuleWikiSourceUnknown)
	var sawDecisions, sawReferences bool
	for _, f := range unknown {
		if f.File == "content/wiki/decisions.md" && contains(f.Detail, "D-9999") {
			sawDecisions = true
		}
		if f.File == "content/wiki/references.md" && contains(f.Detail, "R-8888") {
			sawReferences = true
		}
	}
	if !sawDecisions {
		t.Fatalf("bogus source on decisions.md not flagged: %+v", unknown)
	}
	if !sawReferences {
		t.Fatalf("bogus source on references.md not flagged: %+v", unknown)
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
