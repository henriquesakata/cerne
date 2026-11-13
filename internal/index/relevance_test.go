package index

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// relevanceCase is one line of testdata/relevance/questions.tsv.
type relevanceCase struct {
	question, want string
	topBefore      bool // the entity was in the top 5 before F-0367
}

// seedRelevance builds a backend from testdata/relevance/corpus: one feature
// per directory, its title in title.txt, its documents as <doc>.md (errata.md
// goes to content/errata/<id>.md).
func seedRelevance(t *testing.T) string {
	t.Helper()
	root := filepath.Join("testdata", "relevance", "corpus")
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := t.TempDir()
	now := time.Now().UTC()
	for _, d := range dirs {
		id := d.Name()
		title, err := os.ReadFile(filepath.Join(root, id, "title.txt"))
		if err != nil {
			t.Fatal(err)
		}
		f := store.Feature{ID: id, Title: strings.TrimSpace(string(title)), State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
		docs, _ := filepath.Glob(filepath.Join(root, id, "*.md"))
		for _, src := range docs {
			body, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(repoDir, "content", "features", id, filepath.Base(src))
			if filepath.Base(src) == "errata.md" {
				dst = filepath.Join(repoDir, "content", "errata", id+".md")
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	return dbPath
}

func relevanceCases(t *testing.T) []relevanceCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "relevance", "questions.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cases []relevanceCase
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		cases = append(cases, relevanceCase{cols[0], cols[1], cols[2] == "yes"})
	}
	return cases
}

// F-0367 criterion 8: the expected entity is in the top 5 for at least 18 of
// the 20 questions, and no question that was in the top 5 before leaves it.
func TestRelevance(t *testing.T) {
	db := seedRelevance(t)
	cases := relevanceCases(t)
	if len(cases) != 20 {
		t.Fatalf("fixture has %d questions, want 20", len(cases))
	}
	hits := 0
	for _, c := range cases {
		rs, _, err := SearchEntities(db, c.question, 5)
		if err != nil {
			t.Fatalf("SearchEntities(%q): %v", c.question, err)
		}
		got := entityIDs(rs)
		in := false
		for _, id := range got {
			in = in || id == c.want
		}
		if in {
			hits++
		} else if c.topBefore {
			t.Errorf("regression: %q left the top 5 (want %s, got %v)", c.question, c.want, got)
		}
		t.Logf("%-5v %s %q -> %v", in, c.want, c.question, got)
	}
	t.Logf("top 5: %d/20", hits)
	if hits < 18 {
		t.Errorf("top 5 hits = %d/20, want >= 18", hits)
	}
}
