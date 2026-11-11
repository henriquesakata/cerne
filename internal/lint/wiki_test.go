package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/wiki"
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

// TestLintWikiSourceRecognizesRunbooksAndNotes pins F-0091: runbook and note ids
// are valid wiki provenance sources, so only a truly dangling id is flagged.
func TestLintWikiSourceRecognizesRunbooksAndNotes(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0024", Title: "Plugin Governance", State: domain.RunbookActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveRunbook: %v", err)
	}
	if err := store.SaveNote(dir, store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteActive, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	// Wiki page whose provenance lists a real runbook, a real note, and one
	// dangling id — only the dangling id is unknown.
	writeWikiPage(t, dir, "content/wiki/references.md", []string{"RB-0024", "N-0001", "RB-9999"}, "# Referências\n\nSíntese.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	unknown := byRule(findings, RuleWikiSourceUnknown)
	if len(unknown) != 1 || !contains(unknown[0].Detail, "RB-9999") {
		t.Fatalf("want exactly 1 wiki-source-unknown on RB-9999, got: %+v", unknown)
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

// TestDowngradeWikiDebt pins F-0316 criteria 2, 3 and 5 at the engine: with
// the wiki off its debt is stamped advisory and stops blocking; with it on
// nothing changes — the stamp is derived per run, never stored.
func TestDowngradeWikiDebt(t *testing.T) {
	fresh := func() []Finding {
		return []Finding{{Rule: RuleWikiSourceUnknown, File: "content/wiki/project.md"}, {Rule: RuleBrokenLink, Entity: "F-0001"}}
	}

	off := fresh()
	DowngradeWikiDebt(off, false)
	if !off[0].Downgraded || Blocking(off[0]) {
		t.Errorf("wiki debt with wiki off = %+v, want downgraded and not blocking", off[0])
	}
	if off[1].Downgraded || !Blocking(off[1]) {
		t.Errorf("broken-link with wiki off = %+v, want untouched and blocking", off[1])
	}

	on := fresh()
	DowngradeWikiDebt(on, true)
	for _, f := range on {
		if f.Downgraded || !Blocking(f) {
			t.Errorf("%s with wiki on = %+v, want untouched and blocking", f.Rule, f)
		}
	}

	if Blocking(Finding{Rule: RuleStalePending}) {
		t.Error("stale-pending blocks; want advisory (F-0322)")
	}
}
