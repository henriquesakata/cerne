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

// TestWikiExcludedFromAuthoredRules pins crit. 1 & 2: the generated wiki page is
// not scanned by the authored-content rules — a superseded citation and a broken
// markdown link on a wiki page produce neither ref-to-superseded nor broken-link.
func TestWikiExcludedFromAuthoredRules(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	// Wiki page sourced from F-0008; cites the superseded D-0003 and links a
	// nonexistent file — both authored-rule triggers, both must be silent here.
	writeWikiPage(t, dir, "content/wiki/modules/finance.md", []string{"F-0008"}, "# Finance\n\nBaseado em D-0003. Veja [detalhe](./missing.md).\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range findings {
		if (f.Rule == RuleRefToSuperseded || f.Rule == RuleBrokenLink) && f.File == "content/wiki/modules/finance.md" {
			t.Fatalf("authored rule %q must not scan the generated wiki page, got: %+v", f.Rule, f)
		}
	}
}

// TestAuthoredBrokenLinkStillFlagged pins crit. 4 for broken-link: an authored
// doc with a dangling relative link is still flagged.
func TestAuthoredBrokenLinkStillFlagged(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	writeLiveFeatureDoc(t, dir, "F-0001", "Veja [doc](./missing.md) para detalhes.")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range byRule(findings, RuleBrokenLink) {
		if f.File == "content/features/F-0001/spec.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("authored broken link must be flagged, got: %+v", byRule(findings, RuleBrokenLink))
	}
}

// TestAuthoredEntityRefUnchanged pins crit. 4: an authored entity doc citing a
// superseded id is still flagged (attributed to the feature), even with a
// history word present — only the generated wiki is excluded (F-0090).
func TestAuthoredEntityRefUnchanged(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	writeLiveFeatureDoc(t, dir, "F-0001", "menciona D-0003 que foi superseded por outra")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var found bool
	for _, f := range refToSuperseded(findings) {
		if f.Entity == "F-0001" && contains(f.File, "content/features/F-0001/spec.md") && contains(f.Detail, "D-0003") {
			found = true
		}
	}
	if !found {
		t.Fatalf("authored entity ref-to-superseded must still be flagged (tolerance is wiki-only), got: %+v", refToSuperseded(findings))
	}
}
