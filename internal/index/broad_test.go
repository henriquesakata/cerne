package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedBroad indexes one candidate feature per spec body (F-0001, F-0002, ...).
func seedBroad(t *testing.T, specs ...string) string {
	t.Helper()
	repoDir := t.TempDir()
	now := time.Now().UTC()
	for i, spec := range specs {
		id := "F-000" + string(rune('1'+i))
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: id, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		p := filepath.Join(repoDir, "content", "features", id, "spec.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(spec), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	return dbPath
}

func ids(rs []Result) map[string]bool {
	m := map[string]bool{}
	for _, r := range rs {
		m[r.EntityID] = true
	}
	return m
}

const (
	docA = "# A\n\ntroca de conta e chave\n"
	docB = "# B\n\ncriptografia do switcher dual\n"
)

// Criterion 1 (and 8's index half): all-terms empty -> OR over the keywords.
func TestSearchBroadBroadensWhenAllTermsEmpty(t *testing.T) {
	db := seedBroad(t, docA, docB)
	q := "troca conta chave criptografia switcher dual"
	if rs, _ := Search(db, q, 10); len(rs) != 0 {
		t.Fatalf("precondition: Search with all terms = %d hits, want 0", len(rs))
	}
	rs, broadened, err := SearchBroad(db, q, 10)
	if err != nil {
		t.Fatalf("SearchBroad: %v", err)
	}
	if !broadened {
		t.Fatal("broadened = false, want true")
	}
	if got := ids(rs); !got["F-0001"] || !got["F-0002"] || len(got) != 2 {
		t.Fatalf("hits = %v, want F-0001 and F-0002", got)
	}
}

// Criterion 3: a hit with all terms is returned as-is, no broadening.
func TestSearchBroadKeepsExactResult(t *testing.T) {
	db := seedBroad(t, docA, docB)
	rs, broadened, err := SearchBroad(db, "troca conta chave", 10)
	if err != nil {
		t.Fatal(err)
	}
	if broadened || len(rs) != 1 || rs[0].EntityID != "F-0001" {
		t.Fatalf("got broadened=%v hits=%+v, want exact F-0001 only", broadened, rs)
	}
}

// Criterion 4: FTS5 syntax is never broadened; criterion 1: a single
// keyword is not either (OR of one term is the same query).
func TestSearchBroadSkipsSyntaxAndSingleTerm(t *testing.T) {
	db := seedBroad(t, docA, docB)
	for _, q := range []string{
		`"troca criptografia"`, "troca AND criptografia", "zzzz OR yy troca", "troca NOT conta",
		"troca NEAR criptografia", "troca* criptografia", "^troca criptografia", "troca + criptografia", "(troca criptografia)", "title:troca criptografia",
	} {
		_, broadened, err := SearchBroad(db, q, 10)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		if broadened {
			t.Errorf("%q: broadened = true, want false (FTS5 syntax)", q)
		}
	}
	if _, broadened, _ := SearchBroad(db, "zzzz de", 10); broadened {
		t.Error("single keyword: broadened = true, want false")
	}
}

// Criterion 1 guard: the AND is empty ("de" is in A, "criptografia" only in B),
// but the one keyword alone would hit B; with fewer than 2 keywords it must not broaden.
func TestSearchBroadSingleKeywordDoesNotBroaden(t *testing.T) {
	db := seedBroad(t, docA, docB)
	rs, broadened, err := SearchBroad(db, "criptografia de", 10)
	if err != nil {
		t.Fatal(err)
	}
	if broadened || len(rs) != 0 {
		t.Fatalf("got broadened=%v hits=%+v, want none", broadened, rs)
	}
}

// Criterion 5: nothing even after broadening -> empty, not broadened.
func TestSearchBroadNothingFound(t *testing.T) {
	db := seedBroad(t, docA, docB)
	rs, broadened, err := SearchBroad(db, "xxxx yyyy", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 0 || broadened {
		t.Fatalf("got broadened=%v hits=%+v, want empty and not broadened", broadened, rs)
	}
}
