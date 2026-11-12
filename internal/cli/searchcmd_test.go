package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// indexPathFor derives the index.db path from the cached clone dir returned
// by setupProject (<cache>/<project>/repo → <cache>/<project>/index.db).
func indexPathFor(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), "index.db")
}

// resultLines returns the output lines that look like search hits (start
// with an entity ID prefix).
func resultLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		for _, prefix := range []string{"F-", "D-", "R-"} {
			if strings.HasPrefix(line, prefix) {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func TestSearchAutoBuildsIndexAndFindsSpec(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas mensais sem juros."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "search", "parcelas", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Building index...") {
		t.Fatalf("missing index auto-build message:\n%s", out)
	}
	if !strings.Contains(out, "F-0001 [candidate] Parcelamento — spec") {
		t.Fatalf("missing result line:\n%s", out)
	}
	if !strings.Contains(out, "«parcelas»") {
		t.Fatalf("missing highlighted snippet:\n%s", out)
	}
	if _, err := os.Stat(indexPathFor(repoDir)); err != nil {
		t.Fatalf("index.db not created: %v", err)
	}

	// Second run: the index already exists, no rebuild message.
	out, err = execCerne(t, "search", "parcelas", "--dir", projDir)
	if err != nil {
		t.Fatalf("second search: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "Building index...") {
		t.Fatalf("second search rebuilt the index:\n%s", out)
	}
}

func TestSearchSupersededArrowAndKinds(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	// Seed history straight into the cached clone: search is offline and
	// reads it as-is.
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Bills (boletos)", State: domain.StateSuperseded,
			SupersededBy: "F-0002", CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Pagamentos", State: domain.StateCandidate,
			CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Emitir boleto com vencimento mensal."))
	if err := store.SaveDecision(repoDir, store.Decision{
		ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	out, err := execCerne(t, "search", "boleto", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 [superseded → F-0002] Bills (boletos) — spec") {
		t.Fatalf("missing superseded arrow line:\n%s", out)
	}

	out, err = execCerne(t, "search", "postgres", "--dir", projDir)
	if err != nil {
		t.Fatalf("search decision: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "D-0001 [active] Usar Postgres\n") {
		t.Fatalf("missing decision line (no docs list outside features):\n%s", out)
	}
}

func TestSearchLimitFlag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeature(t, projDir, "Relatório mensal", 1)
	newFeature(t, projDir, "Fechamento mensal", 2)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Consolidar gastos do período."))
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "Travar lançamentos do período."))

	out, err := execCerne(t, "search", "período", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\noutput: %s", err, out)
	}
	if got := resultLines(out); len(got) != 2 {
		t.Fatalf("result lines = %d, want 2:\n%s", len(got), out)
	}

	out, err = execCerne(t, "search", "período", "--limit", "1", "--dir", projDir)
	if err != nil {
		t.Fatalf("search --limit: %v\noutput: %s", err, out)
	}
	if got := resultLines(out); len(got) != 1 {
		t.Fatalf("result lines with --limit 1 = %d, want 1:\n%s", len(got), out)
	}
}

func TestSearchNoResults(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "search", "zzzz", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, `Nothing found for "zzzz".`) {
		t.Fatalf("missing no-results message:\n%s", out)
	}
}

// TestSearchJSON: --json emits hits as a parseable object (the same shape the
// HTTP API returns) and suppresses the human "Construindo índice..." chatter so
// stdout is pure JSON even when the index is built on the first run.
func TestSearchJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas mensais sem juros."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "search", "parcelas", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("search --json: %v\noutput: %s", err, out)
	}
	var data struct {
		Results []struct {
			EntityID string `json:"entity_id"`
			Kind     string `json:"kind"`
			Doc      string `json:"doc"`
			State    string `json:"state"`
			Title    string `json:"title"`
			Snippet  string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("search --json is not valid JSON: %v\n%s", err, out)
	}
	if len(data.Results) != 1 {
		t.Fatalf("search --json results = %d, want 1:\n%s", len(data.Results), out)
	}
	r := data.Results[0]
	if r.EntityID != "F-0001" || r.Kind != "feature" || r.Doc != "spec" || r.State != "candidate" || r.Title != "Parcelamento" {
		t.Fatalf("search --json hit = %+v, want F-0001 feature/spec candidate Parcelamento", r)
	}
	if !strings.Contains(r.Snippet, "parcelas") {
		t.Fatalf("search --json snippet missing match: %q", r.Snippet)
	}
}

func TestSearchOutsideProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	if _, err := execCerne(t, "search", "x", "--dir", dir); err == nil {
		t.Fatalf("search outside a project succeeded, want error")
	}
}
