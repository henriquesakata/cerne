package index

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedEntities indexes features given as id -> title -> doc name -> body.
type fixtureFeature struct {
	id, title string
	docs      map[string]string // "spec" -> body
}

func seedEntities(t *testing.T, fs ...fixtureFeature) string {
	t.Helper()
	repoDir := t.TempDir()
	now := time.Now().UTC()
	for _, f := range fs {
		if err := store.SaveFeature(repoDir, store.Feature{ID: f.id, Title: f.title, State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		for doc, body := range f.docs {
			p := filepath.Join(repoDir, "content", "features", f.id, doc+".md")
			if doc == "errata" {
				p = filepath.Join(repoDir, "content", "errata", f.id+".md")
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	return dbPath
}

func entityIDs(rs []EntityResult) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.EntityID)
	}
	return out
}

// Criterion 1: the three docs of one feature are one line listing them, and
// the limit counts entities.
func TestSearchEntitiesOneLinePerEntity(t *testing.T) {
	three := map[string]string{"spec": "parcelas mensais", "plan": "parcelas no checkout", "result": "parcelas no extrato"}
	db := seedEntities(t,
		fixtureFeature{"F-0001", "Parcelamento", three},
		fixtureFeature{"F-0002", "Cartão", three},
		fixtureFeature{"F-0003", "Fatura", three},
	)
	rs, broadened, err := SearchEntities(db, "parcelas", 2)
	if err != nil || broadened {
		t.Fatalf("SearchEntities: %v broadened=%v", err, broadened)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d entities, want 2 (limit counts entities): %v", len(rs), entityIDs(rs))
	}
	if !reflect.DeepEqual(rs[0].Docs, []string{"spec", "plan", "result"}) {
		t.Fatalf("Docs = %v, want [spec plan result]", rs[0].Docs)
	}
	if rs[0].Doc == "" || rs[0].Snippet == "" {
		t.Fatalf("entity line lost its best document: %+v", rs[0])
	}
}

// Criterion 3: on the OR path, the entity covering 5 of 6 terms across its
// documents beats the one repeating 2 rare terms; the errata counts.
func TestSearchEntitiesRanksByCoverage(t *testing.T) {
	db := seedEntities(t,
		fixtureFeature{"F-0001", "Cofre de sessões", map[string]string{
			"spec":   "A troca de conta usa o WorkspaceSwitcher.",
			"result": "A troca guarda a chave da conta.",
			"errata": "A criptografia do cofre mudou.",
		}},
		fixtureFeature{"F-0002", "Modo duplo", map[string]string{
			"spec": "dual dual dual switcher switcher switcher dual switcher",
		}},
	)
	rs, broadened, err := SearchEntities(db, "troca de conta chave criptografia switcher dual", 10)
	if err != nil || !broadened {
		t.Fatalf("SearchEntities: %v broadened=%v", err, broadened)
	}
	if got := entityIDs(rs); !reflect.DeepEqual(got, []string{"F-0001", "F-0002"}) {
		t.Fatalf("order = %v, want [F-0001 F-0002]", got)
	}
	if rs[0].Coverage != 5 || rs[1].Coverage != 2 {
		t.Fatalf("coverage = %d, %d; want 5, 2", rs[0].Coverage, rs[1].Coverage)
	}
}

// Criterion 3: equal coverage is broken by bm25 with the title weighing more.
func TestSearchEntitiesTitleBreaksTies(t *testing.T) {
	db := seedEntities(t,
		// Unweighted, F-0001 wins: rateio twice in a short body. The title
		// weight puts F-0002, whose title is the term, first.
		fixtureFeature{"F-0001", "Relatório", map[string]string{"spec": "rateio rateio extrato"}},
		fixtureFeature{"F-0002", "Rateio", map[string]string{"spec": "o extrato do mês"}},
		// Filler, so bm25's idf of the terms is positive (FTS5 floors it when
		// a term is in half the rows or more).
		fixtureFeature{"F-0003", "Agenda", map[string]string{"spec": "consultas da semana"}},
		fixtureFeature{"F-0004", "Estoque", map[string]string{"spec": "itens do depósito"}},
		fixtureFeature{"F-0005", "Folha", map[string]string{"spec": "salários e férias"}},
	)
	rs, _, err := SearchEntities(db, "rateio extrato tributo", 10)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if got := entityIDs(rs); !reflect.DeepEqual(got, []string{"F-0002", "F-0001"}) {
		t.Fatalf("order = %v, want the title match F-0002 first", got)
	}
}

// F-0343 behavior kept: explicit FTS5 syntax is not broadened.
func TestSearchEntitiesKeepsExplicitSyntax(t *testing.T) {
	db := seedEntities(t, fixtureFeature{"F-0001", "A", map[string]string{"spec": "troca de conta"}})
	rs, broadened, err := SearchEntities(db, `"troca criptografia"`, 10)
	if err != nil || broadened || len(rs) != 0 {
		t.Fatalf("syntax query = %v broadened=%v err=%v; want nothing, not broadened", entityIDs(rs), broadened, err)
	}
}
