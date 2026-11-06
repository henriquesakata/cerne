package cost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// resultDoc is a result that passes the result checks, ending in its Gauntlet.
func resultDoc(id string) string {
	return "---\ncerne_id: " + id + "\n---\n\n# Título\n\nResultado.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n\n" +
		"## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n"
}

func writeResult(t *testing.T, repo, id, body string) {
	t.Helper()
	dir := filepath.Join(repo, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stampReport(id string, c Counters, messages int) Report {
	return Report{ID: id, Counters: c, ByModel: map[string]Counters{"claude-opus-5-5": c}, Messages: messages, Sessions: 1,
		First: "2026-11-01T10:02:00Z", Last: "2026-11-01T10:03:00Z"}
}

func mustStamp(t *testing.T, doc string, r Report) string {
	t.Helper()
	out, err := Stamp(doc, r)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	return out
}

// TestStampInsertsThenReplacesOneBlock (crit. 8-9): the stamp appends one
// "## Custo" block (table + anchor) after the Gauntlet without breaking the
// result record, replaces it in place on a re-stamp — wherever it sits — and
// is byte-identical for the same report.
func TestStampInsertsThenReplacesOneBlock(t *testing.T) {
	doc := resultDoc("F-0001")
	r := stampReport("F-0001", cc(1, 2, 3, 4), 2)
	once := mustStamp(t, doc, r)
	if !strings.HasPrefix(once, strings.TrimRight(doc, "\n")) || !strings.Contains(once, "| cache_read | 3 |") ||
		!strings.Contains(once, "<!-- cerne:cost v1 {") || strings.Count(once, "## Custo") != 1 {
		t.Fatalf("first stamp:\n%s", once)
	}
	repo := t.TempDir()
	writeResult(t, repo, "F-0001", once)
	if findings := lint.CheckResult(repo, store.Feature{ID: "F-0001", State: domain.StateImplemented}); len(findings) != 0 {
		t.Fatalf("stamped result fails the record checks: %+v", findings)
	}

	twice := mustStamp(t, once, stampReport("F-0001", cc(10, 20, 30, 40), 3))
	if strings.Count(twice, "## Custo") != 1 || strings.Count(twice, "<!-- cerne:cost v1 ") != 1 ||
		!strings.Contains(twice, "| output | 40 |") || strings.Contains(twice, "| output | 4 |") {
		t.Fatalf("re-stamp:\n%s", twice)
	}

	gauntlet := doc[strings.Index(doc, "## Gauntlet"):]
	mid := doc[:strings.Index(doc, "## Gauntlet")] + "## Custo\n\nvelho\n\n" + gauntlet
	got := mustStamp(t, mid, r)
	if strings.Count(got, "## Custo") != 1 || strings.Contains(got, "velho") || !strings.HasSuffix(got, "\n\n"+gauntlet) ||
		strings.Index(got, "## Custo") > strings.Index(got, "## Gauntlet") {
		t.Fatalf("mid-document stamp:\n%s", got)
	}

	if again := mustStamp(t, once, r); again != once {
		t.Fatalf("same report not byte-identical:\n%s\nvs\n%s", again, once)
	}
}

// TestStampNeverLowersAnchor: a re-stamp from fewer messages than the anchor
// recorded (transcripts pruned after the snapshot was dropped) is refused and
// the body is left as is.
func TestStampNeverLowersAnchor(t *testing.T) {
	stamped := mustStamp(t, resultDoc("F-0001"), stampReport("F-0001", cc(9, 9, 9, 9), 100))
	got, err := Stamp(stamped, stampReport("F-0001", cc(1, 1, 1, 1), 80))
	if !errors.Is(err, ErrStampWouldLower) || got != stamped {
		t.Fatalf("err = %v, body changed = %v; want ErrStampWouldLower and the body unchanged", err, got != stamped)
	}
}

// TestRollupSumsAnchorsAndNamesMissing (crit. 10): an epic's report sums its
// children's anchors, names those without one with their state, and totals
// own + children.
func TestRollupSumsAnchorsAndNamesMissing(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "E-0001", domain.StateInDevelopment)
	child := func(id string, state domain.FeatureState) {
		t.Helper()
		if err := store.SaveFeature(in.RepoDir, store.Feature{ID: id, Title: id, Parent: "E-0001", State: state, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	child("F-0002", domain.StateImplemented)
	writeResult(t, in.RepoDir, "F-0002", mustStamp(t, resultDoc("F-0002"), stampReport("F-0002", cc(1, 1, 1, 1), 1)))
	child("F-0003", domain.StateImplemented)
	writeResult(t, in.RepoDir, "F-0003", resultDoc("F-0003"))
	child("F-0004", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s", tmsg{ts: at(2), id: "a", req: "r", c: cc(100, 0, 0, 0)})
	bindAt(t, in.CacheDir, "s", "", "E-0001", at(1))

	r := compute(t, in, "E-0001")
	if r.Counters != cc(100, 0, 0, 0) || r.Children == nil || r.Total == nil {
		t.Fatalf("report = %+v", r)
	}
	if r.Children.Counters != cc(1, 1, 1, 1) || !reflect.DeepEqual(r.Children.Stamped, []string{"F-0002"}) ||
		!reflect.DeepEqual(r.Children.Missing, []string{"F-0003 (implemented)", "F-0004 (in_development)"}) {
		t.Fatalf("children = %+v", *r.Children)
	}
	if *r.Total != cc(101, 1, 1, 1) {
		t.Fatalf("total = %+v, want own + children", *r.Total)
	}
}

// TestRollupRecursesLiveTree (C3): the rollup walks the live parent/child tree
// at every depth, summing each node's own anchor counters — never a stored
// total, which a grandchild stamped after its parent would leave stale — and
// naming every node without an anchor.
func TestRollupRecursesLiveTree(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	node := func(id, parent string, own *Counters, total *Counters) {
		t.Helper()
		if err := store.SaveFeature(in.RepoDir, store.Feature{ID: id, Title: id, Parent: parent, State: domain.StateImplemented, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
		if own == nil {
			writeResult(t, in.RepoDir, id, resultDoc(id))
			return
		}
		r := stampReport(id, *own, 1)
		r.Total = total
		writeResult(t, in.RepoDir, id, mustStamp(t, resultDoc(id), r))
	}
	// F-0002 (an epic) was stamped before its child F-0004: its stored total is its own cost alone.
	node("F-0002", "F-0001", &Counters{Input: 2000}, &Counters{Input: 2000})
	node("F-0003", "F-0001", &Counters{Input: 110}, nil)
	node("F-0004", "F-0002", &Counters{Input: 40000}, nil)
	node("F-0005", "F-0004", nil, nil)

	r := compute(t, in, "F-0001")
	if r.Children == nil || r.Children.Counters != cc(42110, 0, 0, 0) ||
		!reflect.DeepEqual(r.Children.Stamped, []string{"F-0002", "F-0003", "F-0004"}) ||
		!reflect.DeepEqual(r.Children.Missing, []string{"F-0005 (implemented)"}) {
		t.Fatalf("children = %+v", r.Children)
	}
}

// TestAnchorSchemaIsV1 (C14): the anchor has its own v1 schema, decoupled from
// Report — whatever the report carries (reason, gaps, rollup, views), the
// stamped JSON holds exactly the v1 fields.
func TestAnchorSchemaIsV1(t *testing.T) {
	r := stampReport("F-0001", cc(1, 2, 3, 4), 2)
	r.Reason, r.SessionsWithoutTranscript, r.SessionsRecovered = "x", 1, 1
	r.Entities, r.OtherHarness = map[string]Counters{"F-0001": cc(1, 2, 3, 4)}, []string{"s"}
	r.Children, r.Total = &Rollup{Stamped: []string{}, Missing: []string{}}, &Counters{}
	m := anchorRe.FindStringSubmatch(mustStamp(t, resultDoc("F-0001"), r))
	if m == nil {
		t.Fatal("no anchor")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(m[1]), &fields); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"by_model", "counters", "first", "id", "last", "messages", "sessions", "sidechain", "unattributed"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("anchor keys = %v, want %v", keys, want)
	}
}
