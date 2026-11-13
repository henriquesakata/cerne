package guide

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

// TestSearchCoverageOrderMatchesRankEntities: the coverage ranking is
// replicated here, not shared (coverageEntities is bound to the backend
// index), so this pins that both order one small fixture the same way. It
// asserts the order of coverage-distinct entries, not bm25 tie-breaks, which
// depend on each corpus's own statistics.
func TestSearchCoverageOrderMatchesRankEntities(t *testing.T) {
	docs := []struct{ id, title, body string }{
		{"F-0001", "Cofre de sessões", "A troca de conta usa o WorkspaceSwitcher. A troca guarda a chave da conta. A criptografia do cofre mudou."},
		{"F-0002", "Modo duplo", "dual dual dual switcher switcher switcher dual switcher"},
		{"F-0003", "Outro assunto", "a chave"},
		{"F-0004", "Sem relação", "nada a ver"},
	}
	repoDir, now := t.TempDir(), time.Now().UTC()
	var secs []Section
	for _, d := range docs {
		if err := store.SaveFeature(repoDir, store.Feature{ID: d.id, Title: d.title, State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(repoDir, "content", "features", d.id, "spec.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(d.body), 0o644); err != nil {
			t.Fatal(err)
		}
		secs = append(secs, Section{Title: d.id + " " + d.title, Source: "faq", Text: d.body})
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatal(err)
	}
	const q = "troca de conta chave criptografia switcher dual"
	ranked, err := index.RankEntities(dbPath, q, 10)
	if err != nil {
		t.Fatal(err)
	}
	var want, got []string
	for _, r := range ranked {
		want = append(want, r.EntityID)
	}
	for _, s := range Search(secs, q, 10) {
		got = append(got, strings.SplitN(s.Title, " ", 2)[0])
	}
	if !reflect.DeepEqual(got, want) || len(want) != 3 {
		t.Fatalf("guide order %v, RankEntities order %v (want the same 3, F-0004 out)", got, want)
	}
}
