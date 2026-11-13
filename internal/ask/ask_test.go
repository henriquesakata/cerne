package ask

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
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

func TestHistoricalFlag(t *testing.T) {
	cases := []struct {
		kind, state string
		want        bool
	}{
		{"feature", "implemented", true},
		{"feature", "superseded", true},
		{"feature", "approved", false},
		{"feature", "candidate", false},
		{"feature", "in_development", false},
		{"decision", "active", false},
		{"decision", "superseded", true},
		{"decision", "retired", true}, // F-0385
		{"runbook", "active", false},
		{"note", "active", false},
		{"reference", "current", false},
		{"historical", "superseded", true},
		{"wiki", "current", false},
	}
	for _, c := range cases {
		if got := historical(c.kind, c.state); got != c.want {
			t.Errorf("historical(%q, %q) = %v, want %v", c.kind, c.state, got, c.want)
		}
	}
}

func TestRetrieveMarksImplementedHistorical(t *testing.T) {
	repoDir, dbPath := seed(t)
	// F-0003 implemented: retrieved, but flagged as historical record.
	f := store.Feature{ID: "F-0003", Title: "F-0003", Module: "finance", State: domain.StateImplemented, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC()}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	p := filepath.Join(repoDir, "content", "features", "F-0003", "spec.md")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("# F-0003\n\nOs boletos migraram.\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if err := index.Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	snips, err := Retrieve(repoDir, dbPath, "boletos", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	var sawImplemented bool
	for _, s := range snips {
		switch {
		case s.Source == "F-0003":
			sawImplemented = true
			if !s.Historical {
				t.Fatalf("implemented feature not flagged historical: %+v", s)
			}
		case s.Source == "F-0001" || s.Kind == "wiki":
			if s.Historical {
				t.Fatalf("live source wrongly flagged historical: %+v", s)
			}
		}
	}
	if !sawImplemented {
		t.Fatalf("F-0003 (implemented) missing from snippets: %+v", snips)
	}
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

// seedForRanking builds a backend where BM25 alone would rank the implemented
// feature first: its spec repeats the query term, while the active decision and
// the live feature mention it once inside longer bodies.
func seedForRanking(t *testing.T) (repoDir, dbPath string) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo", Modules: []string{"infra"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	now := time.Unix(0, 0).UTC()
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Plataforma", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	feats := []struct {
		id    string
		state domain.FeatureState
		spec  string
	}{
		{"F-0001", domain.StateApproved, "# F-0001\n\nMuda o ambiente da plataforma para o novo desenho agora aprovado.\n"},
		{"F-0002", domain.StateImplemented, "# F-0002\n\nambiente ambiente ambiente ambiente ambiente ambiente.\n"},
		{"F-0003", domain.StateImplemented, "# F-0003\n\nO ambiente antigo, descrito uma vez em corpo mais longo do que os demais.\n"},
	}
	for _, f := range feats {
		if err := store.SaveFeature(repoDir, store.Feature{ID: f.id, Title: f.id, Module: "infra", State: f.state, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.id, err)
		}
		p := filepath.Join(repoDir, "content", "features", f.id, "spec.md")
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(f.spec), 0o644); err != nil {
			t.Fatalf("write spec: %v", err)
		}
	}
	p := filepath.Join(repoDir, "content", "decisions", "D-0001.md")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("# D-0001\n\nDecidimos que o ambiente roda em k8s, entre outras coisas descritas neste corpo mais longo.\n"), 0o644); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	dbPath = filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	return repoDir, dbPath
}

// entityOrder extracts the non-wiki sources in retrieval order.
func entityOrder(snips []Snippet) []string {
	var out []string
	for _, s := range snips {
		if s.Kind != "wiki" {
			out = append(out, s.Source)
		}
	}
	return out
}

func TestRetrieveRanking(t *testing.T) {
	repoDir, dbPath := seedForRanking(t)
	snips, err := Retrieve(repoDir, dbPath, "ambiente", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	got := entityOrder(snips)
	// Precedence tiers: live non-feature (D-0001) > live feature (F-0001) >
	// implemented (F-0002 before F-0003 — BM25 order preserved inside the tier).
	want := []string{"D-0001", "F-0001", "F-0002", "F-0003"}
	if len(got) != len(want) {
		t.Fatalf("entity order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entity order = %v, want %v", got, want)
		}
	}
}

func TestTrimAfterRanking(t *testing.T) {
	repoDir, dbPath := seedForRanking(t)
	snips, err := Retrieve(repoDir, dbPath, "ambiente", false, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	var wikiCount int
	for _, s := range snips {
		if s.Kind == "wiki" {
			wikiCount++
		}
	}
	// Keep the wiki pages plus two entity slots: the live sources must survive
	// the cut; the historical (implemented) prose is what gets dropped.
	trimmed := Trim(snips, wikiCount+2, 0)
	got := entityOrder(trimmed)
	want := []string{"D-0001", "F-0001"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("post-trim entity order = %v, want %v", got, want)
	}
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
	}, true)
	for _, want := range []string{"por que abandonamos boletos?", "F-0002", "superseded", "boletos antigos"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestBuildAskPromptAnnotatesExternalURL: a snippet carrying a URL (external
// reference) surfaces that URL in the prompt so the answer can cite the source.
func TestBuildAskPromptAnnotatesExternalURL(t *testing.T) {
	prompt := BuildAskPrompt("como configurar PKCE?", []Snippet{
		{Source: "R-0042", Kind: "reference", State: "current", URL: "https://example.com/pkce", Text: "use S256"},
	}, true)
	for _, want := range []string{"R-0042", "referência externa", "https://example.com/pkce"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestBuildAskPromptNoPreamble: with preamble=false the instruction scaffold is
// dropped, but the question and the sources stay.
func TestBuildAskPromptNoPreamble(t *testing.T) {
	prompt := BuildAskPrompt("por que abandonamos boletos?", []Snippet{
		{Source: "F-0002", Kind: "feature", State: "superseded", Text: "boletos antigos"},
	}, false)
	if contains(prompt, "Responda em pt-BR") || contains(prompt, "Respeite o estado") {
		t.Fatalf("preamble=false must omit the instruction:\n%s", prompt)
	}
	for _, want := range []string{"por que abandonamos boletos?", "F-0002", "boletos antigos"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestTrim: caps the number of sources (order preserved) and truncates each
// snippet body to the rune budget, appending an ellipsis when it cuts.
func TestBuildAskPromptHistoricalLabel(t *testing.T) {
	snips := []Snippet{
		{Source: "F-0344", Kind: "feature", State: "implemented", Historical: true, Text: "era compose"},
		{Source: "D-0047", Kind: "decision", State: "active", Text: "plataforma k8s"},
	}
	got := BuildAskPrompt("ambiente?", snips, false)
	if !strings.Contains(got, "## F-0344 (feature, implemented — registro histórico)") {
		t.Fatalf("historical header missing label:\n%s", got)
	}
	if strings.Contains(got, "## D-0047 (decision, active — registro histórico)") {
		t.Fatalf("live source wrongly labeled historical:\n%s", got)
	}
}

func TestBuildAskPromptPreamblePrecedence(t *testing.T) {
	snips := []Snippet{{Source: "D-0001", Kind: "decision", State: "active", Text: "x"}}
	withPre := BuildAskPrompt("q", snips, true)
	if !strings.Contains(withPre, "registro histórico") || !strings.Contains(withPre, "precedência") {
		t.Fatalf("preamble missing precedence instruction:\n%s", withPre)
	}
	noPre := BuildAskPrompt("q", snips, false)
	if strings.Contains(noPre, "precedência") {
		t.Fatalf("precedence instruction leaked without preamble:\n%s", noPre)
	}
}

func TestTrim(t *testing.T) {
	in := []Snippet{
		{Source: "wiki:project", Kind: "wiki", State: "current", Text: "abcdefghij"},
		{Source: "F-0001", Kind: "feature", State: "candidate", Text: "short"},
		{Source: "F-0002", Kind: "feature", State: "candidate", Text: "dropped"},
	}

	// Budget 5: "abcdefghij" (10) is cut to 5 + ellipsis; "short" (exactly 5) is
	// left intact — truncation only fires when length strictly exceeds the budget.
	got := Trim(in, 2, 5)
	if len(got) != 2 {
		t.Fatalf("Trim kept %d sources, want 2 (cap): %+v", len(got), got)
	}
	if got[0].Source != "wiki:project" || got[1].Source != "F-0001" {
		t.Fatalf("Trim must preserve order, got %s,%s", got[0].Source, got[1].Source)
	}
	if got[0].Text != "abcde…" {
		t.Fatalf("Trim truncate = %q, want \"abcde…\"", got[0].Text)
	}
	if got[1].Text != "short" {
		t.Fatalf("Trim must leave text at the budget intact, got %q", got[1].Text)
	}
	// Zero limits are no-ops; the input is not mutated.
	if all := Trim(in, 0, 0); len(all) != 3 || in[0].Text != "abcdefghij" {
		t.Fatalf("Trim(0,0) must keep all and not mutate input: len=%d in0=%q", len(all), in[0].Text)
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

// F-0367 criterion 7: ask retrieves entities, not documents — a feature whose
// spec, plan and result all match is one source — and keeps the vigency order
// over them. A question with FTS5 punctuation still retrieves by its keywords.
func TestRetrieveOneSourcePerEntity(t *testing.T) {
	repoDir, _ := seedForRanking(t)
	for _, doc := range []string{"plan.md", "result.md"} {
		p := filepath.Join(repoDir, "content", "features", "F-0002", doc)
		if err := os.WriteFile(p, []byte("# F-0002\n\nO ambiente segue igual.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("index.Rebuild: %v", err)
	}
	for _, q := range []string{"ambiente", "plataforma: qual o ambiente (k8s)?"} {
		snips, err := Retrieve(repoDir, dbPath, q, false, 10)
		if err != nil {
			t.Fatalf("Retrieve(%q): %v", q, err)
		}
		got := entityOrder(snips)
		want := []string{"D-0001", "F-0001", "F-0002", "F-0003"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Retrieve(%q) entity order = %v, want %v", q, got, want)
		}
	}
}
