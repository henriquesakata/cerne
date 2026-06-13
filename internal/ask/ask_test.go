package ask

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/store"
)

func seed(t *testing.T) (repoDir, dbPath string) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo", Modules: []string{"finance"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	now := time.Unix(0, 0).UTC()
	mustFeat := func(id string, state domain.FeatureState, by, spec string) {
		f := store.Feature{ID: id, Title: id, Module: "finance", State: state, SupersededBy: by, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", id, err)
		}
		p := filepath.Join(repoDir, "content", "features", id, "spec.md")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(spec), 0o644); err != nil {
			t.Fatalf("write spec: %v", err)
		}
	}
	mustFeat("F-0001", domain.StateApproved, "", "# F-0001\n\nOs boletos vivem no finance.\n")
	mustFeat("F-0002", domain.StateSuperseded, "F-0001", "# F-0002\n\nOs boletos antigos.\n")
	writeWiki := func(rel, body string) {
		p := filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write wiki: %v", err)
		}
	}
	writeWiki("modules/finance.md", "---\ncerne_wiki: finance\n---\n\n# Finance\n\nSíntese de boletos no finance.\n")

	dbPath = filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	return repoDir, dbPath
}

func TestRetrieveCurrentFirstExcludesTerminal(t *testing.T) {
	repoDir, dbPath := seed(t)
	snips, err := Retrieve(repoDir, dbPath, "boletos", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	var sawApproved, sawWiki bool
	for _, s := range snips {
		if s.State == "superseded" {
			t.Fatalf("default retrieval leaked a superseded source: %+v", s)
		}
		if s.Source == "F-0001" {
			sawApproved = true
		}
		if s.Kind == "wiki" {
			sawWiki = true
		}
	}
	if !sawApproved {
		t.Fatalf("F-0001 (approved) missing from snippets: %+v", snips)
	}
	if !sawWiki {
		t.Fatalf("no wiki snippet for the hit module: %+v", snips)
	}
}

func TestRetrieveHistoryIncludesTerminalTagged(t *testing.T) {
	repoDir, dbPath := seed(t)
	snips, err := Retrieve(repoDir, dbPath, "boletos", true, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	var found bool
	for _, s := range snips {
		if s.Source == "F-0002" {
			if s.State != "superseded" {
				t.Fatalf("F-0002 tagged %q, want superseded", s.State)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("--history did not include the superseded F-0002: %+v", snips)
	}
}

// seedWithCrossCutting builds a backend with a feature, an active decision and
// a current reference (all with searchable content), plus compiled wiki pages
// for modules/finance, decisions and references. Returns the repo and db paths.
func seedWithCrossCutting(t *testing.T) (repoDir, dbPath string) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo", Modules: []string{"finance"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	now := time.Unix(0, 0).UTC()

	feat := store.Feature{ID: "F-0001", Title: "Boletos", Module: "finance", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, feat); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	dec := store.Decision{ID: "D-0001", Title: "Gateway de pagamento", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, dec); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	ref := store.Reference{ID: "R-0001", Title: "Runbook de deploy", State: domain.ReferenceCurrent, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveReference(repoDir, ref); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}

	writeFile := func(rel, body string) {
		p := filepath.Join(repoDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	writeFile("content/features/F-0001/spec.md", "# F-0001\n\nFeature de boletos no finance.\n")
	writeFile("content/decisions/D-0001.md", "# D-0001\n\nAdotamos um gateway dedicado.\n")
	writeFile("content/references/R-0001.md", "# R-0001\n\nProcedimento de implantacao.\n")
	// Compiled wiki pages (with provenance frontmatter), as cerne compile writes.
	writeFile("content/wiki/modules/finance.md", "---\ncerne_wiki: finance\n---\n\n# Finance\n\nSíntese do finance.\n")
	writeFile("content/wiki/decisions.md", "---\ncerne_wiki: decisions\n---\n\n# Decisões\n\nSíntese das decisões ativas.\n")
	writeFile("content/wiki/references.md", "---\ncerne_wiki: references\n---\n\n# Referências\n\nSíntese das referências vigentes.\n")

	dbPath = filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	return repoDir, dbPath
}

func hasSnippet(snips []Snippet, source string) bool {
	for _, s := range snips {
		if s.Source == source {
			return true
		}
	}
	return false
}

func TestRetrievePullsDecisionsAndReferencesPages(t *testing.T) {
	repoDir, dbPath := seedWithCrossCutting(t)
	// A query that hits the decision (gateway) and the reference (runbook).
	snips, err := Retrieve(repoDir, dbPath, "gateway runbook", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !hasSnippet(snips, "wiki:decisions") {
		t.Fatalf("decision hit did not pull wiki:decisions: %+v", snips)
	}
	if !hasSnippet(snips, "wiki:references") {
		t.Fatalf("reference hit did not pull wiki:references: %+v", snips)
	}
}

func TestRetrieveFeatureOnlyHitsSkipCrossCuttingPages(t *testing.T) {
	repoDir, dbPath := seedWithCrossCutting(t)
	// A query that hits only the feature: no decision/reference pages pulled.
	snips, err := Retrieve(repoDir, dbPath, "boletos", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !hasSnippet(snips, "wiki:modules/finance") {
		t.Fatalf("feature hit did not pull its module page: %+v", snips)
	}
	if hasSnippet(snips, "wiki:decisions") {
		t.Fatalf("feature-only query wrongly pulled wiki:decisions: %+v", snips)
	}
	if hasSnippet(snips, "wiki:references") {
		t.Fatalf("feature-only query wrongly pulled wiki:references: %+v", snips)
	}
}

func TestExtractKeywords(t *testing.T) {
	got := extractKeywords("Por que abandonamos o módulo de pagamentos?")
	for _, want := range []string{"pagamentos", "abandonamos", "módulo"} {
		if !contains(got, want) {
			t.Fatalf("extractKeywords = %q, missing %q", got, want)
		}
	}
	for _, stop := range []string{"por", "que", " o ", "de"} {
		if contains(got, stop) {
			t.Fatalf("extractKeywords = %q, leaked stopword %q", got, stop)
		}
	}
	if !contains(got, " OR ") {
		t.Fatalf("extractKeywords = %q, want OR-joined terms", got)
	}
}

func TestExtractKeywordsAllStopwords(t *testing.T) {
	q := "por que o que de"
	if got := extractKeywords(q); got != q {
		t.Fatalf("extractKeywords(all-stopwords) = %q, want original %q", got, q)
	}
}

func TestRetrieveNaturalLanguageQuestion(t *testing.T) {
	repoDir, dbPath := seed(t)
	// A natural-language question whose tokens, taken literally as an FTS AND
	// query, would not all co-occur in any document — only keyword extraction
	// (boletos OR ...) recovers the relevant F-0001.
	nl := "Por que será que abandonamos os boletos no sistema?"
	snips, err := Retrieve(repoDir, dbPath, nl, false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	var found bool
	for _, s := range snips {
		if s.Source == "F-0001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("NL question did not recover F-0001 via keyword extraction: %+v", snips)
	}
}

func TestBuildAskPrompt(t *testing.T) {
	prompt := BuildAskPrompt("por que abandonamos boletos?", []Snippet{
		{Source: "F-0002", Kind: "feature", State: "superseded", Text: "boletos antigos"},
	})
	for _, want := range []string{"por que abandonamos boletos?", "F-0002", "superseded", "boletos antigos"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
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
