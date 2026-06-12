package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedBackend builds a backend tree covering every indexing shape: a
// superseded feature with spec+plan docs, a candidate feature with a spec,
// a title-only feature, an active and a superseded decision, and a
// title-only reference.
func seedBackend(t *testing.T) (repoDir string) {
	t.Helper()
	repoDir = t.TempDir()
	now := time.Now().UTC()

	features := []store.Feature{
		{ID: "F-0001", Title: "Bills (boletos)", State: domain.StateSuperseded,
			SupersededBy: "F-0002", CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Pagamentos via Pix", State: domain.StateCandidate,
			CreatedAt: now, UpdatedAt: now},
		{ID: "F-0003", Title: "Telemetria de uso", State: domain.StateCandidate,
			CreatedAt: now, UpdatedAt: now},
	}
	for _, f := range features {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	decisions := []store.Decision{
		{ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive,
			CreatedAt: now, UpdatedAt: now},
		{ID: "D-0002", Title: "Usar MySQL", State: domain.DecisionSuperseded,
			SupersededBy: "D-0001", CreatedAt: now, UpdatedAt: now},
	}
	for _, d := range decisions {
		if err := store.SaveDecision(repoDir, d); err != nil {
			t.Fatalf("SaveDecision %s: %v", d.ID, err)
		}
	}
	if err := store.SaveReference(repoDir, store.Reference{
		ID: "R-0001", Title: "Mapa de arquitetura", State: domain.ReferenceCurrent,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}

	writeContent(t, repoDir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Bills\n\nEmitir boleto bancário com vencimento mensal, "+
			"juros e multa configuráveis, registro automático na rede e fallback para pix "+
			"quando o pagador preferir liquidação instantânea em vez do fluxo comum de compensação.\n")
	writeContent(t, repoDir, "content/features/F-0001/plan.md",
		"---\ncerne_id: F-0001\n---\n\n# Plano\n\nCronograma em duas fases com rollout comum gradual.\n")
	writeContent(t, repoDir, "content/features/F-0002/spec.md",
		"---\ncerne_id: F-0002\n---\n\n# Pagamentos\n\nIntegração com provedor de pagamento "+
			"instantâneo no fluxo comum de checkout.\n")
	writeContent(t, repoDir, "content/decisions/D-0001.md",
		"---\ncerne_id: D-0001\n---\n\n# Usar Postgres\n\nBanco relacional escolhido pela "+
			"maturidade e pela robustez da decisão.\n")
	writeContent(t, repoDir, "content/decisions/D-0002.md",
		"---\ncerne_id: D-0002\n---\n\n# Usar MySQL\n\nDescartado pela limitação de janelas transacionais.\n")
	return repoDir
}

func writeContent(t *testing.T, repoDir, rel, body string) {
	t.Helper()
	path := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

// rebuild seeds a backend, builds the index and returns the db path.
func rebuild(t *testing.T) (repoDir, dbPath string) {
	t.Helper()
	repoDir = seedBackend(t)
	dbPath = filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	return repoDir, dbPath
}

func TestSearchFindsDocuments(t *testing.T) {
	_, dbPath := rebuild(t)
	tests := []struct {
		name, query                          string
		wantID, wantKind, wantDoc, wantState string
	}{
		{"body word in feature spec", "boleto", "F-0001", "feature", "spec", "superseded"},
		{"body word in feature plan", "cronograma", "F-0001", "feature", "plan", "superseded"},
		{"body word in decision doc", "maturidade", "D-0001", "decision", "doc", "active"},
		{"title word of title-only feature", "telemetria", "F-0003", "feature", "", "candidate"},
		{"title word of title-only reference", "arquitetura", "R-0001", "reference", "", "current"},
		{"accented query matches accented body", "decisão", "D-0001", "decision", "doc", "active"},
		{"unaccented query matches accented body", "decisao", "D-0001", "decision", "doc", "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Search(dbPath, tt.query, 10)
			if err != nil {
				t.Fatalf("Search(%q): %v", tt.query, err)
			}
			if len(results) != 1 {
				t.Fatalf("Search(%q) = %d results, want 1: %+v", tt.query, len(results), results)
			}
			r := results[0]
			if r.EntityID != tt.wantID || r.Kind != tt.wantKind || r.Doc != tt.wantDoc || r.State != tt.wantState {
				t.Fatalf("result = %+v, want id %s kind %s doc %q state %s",
					r, tt.wantID, tt.wantKind, tt.wantDoc, tt.wantState)
			}
		})
	}
}

func TestSearchSnippetMarkers(t *testing.T) {
	_, dbPath := rebuild(t)
	results, err := Search(dbPath, "boleto", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1: %+v", len(results), results)
	}
	snippet := results[0].Snippet
	if want := "«boleto»"; !strings.Contains(snippet, want) {
		t.Fatalf("snippet = %q, want highlighted %q", snippet, want)
	}
	if !strings.Contains(snippet, "…") {
		t.Fatalf("snippet = %q, want ellipsis for a body longer than the snippet window", snippet)
	}
	if results[0].Title != "Bills (boletos)" {
		t.Fatalf("Title = %q, want %q", results[0].Title, "Bills (boletos)")
	}
}

func TestSearchSupersededBy(t *testing.T) {
	_, dbPath := rebuild(t)
	tests := []struct {
		name, query, wantID, wantSupersededBy string
	}{
		{"superseded feature carries pointer", "boleto", "F-0001", "F-0002"},
		{"superseded decision carries pointer", "transacionais", "D-0002", "D-0001"},
		{"active decision has none", "maturidade", "D-0001", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Search(dbPath, tt.query, 10)
			if err != nil {
				t.Fatalf("Search(%q): %v", tt.query, err)
			}
			if len(results) != 1 || results[0].EntityID != tt.wantID {
				t.Fatalf("Search(%q) = %+v, want single result for %s", tt.query, results, tt.wantID)
			}
			if results[0].SupersededBy != tt.wantSupersededBy {
				t.Fatalf("SupersededBy = %q, want %q", results[0].SupersededBy, tt.wantSupersededBy)
			}
		})
	}
}

func TestSearchLimit(t *testing.T) {
	_, dbPath := rebuild(t)
	// "comum" appears in three documents (F-0001 spec, F-0001 plan, F-0002 spec).
	all, err := Search(dbPath, "comum", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unlimited results = %d, want 3: %+v", len(all), all)
	}
	limited, err := Search(dbPath, "comum", 2)
	if err != nil {
		t.Fatalf("Search with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited results = %d, want 2: %+v", len(limited), limited)
	}
}

func TestSearchRankTitleHit(t *testing.T) {
	_, dbPath := rebuild(t)
	// "pix" is in F-0002's title and buried in F-0001's spec body.
	results, err := Search(dbPath, "pix", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("results = %d, want at least the title hit and the body hit: %+v", len(results), results)
	}
	found := false
	for _, r := range results {
		if r.EntityID == "F-0002" {
			found = true
		}
	}
	if !found {
		t.Fatalf("title hit F-0002 missing from results: %+v", results)
	}
}

func TestSearchSpecialCharsFallsBackToPhrase(t *testing.T) {
	_, dbPath := rebuild(t)
	for _, query := range []string{`c++ "thing`, `bole*to(`, `"unbalanced`, `F-0001`} {
		if _, err := Search(dbPath, query, 10); err != nil {
			t.Fatalf("Search(%q) errored, want phrase fallback to absorb fts5 syntax errors: %v", query, err)
		}
	}
}

func TestRebuildTwiceNoDupes(t *testing.T) {
	repoDir, dbPath := rebuild(t)
	if err := Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	results, err := Search(dbPath, "boleto", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results after two rebuilds = %d, want 1 (drop+recreate): %+v", len(results), results)
	}
}
