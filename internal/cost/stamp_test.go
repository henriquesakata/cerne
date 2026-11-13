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
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
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

// TestStampIgnoresQuotedExamples (C7): a "## Custo" heading or an anchor quoted
// in a fenced block — or an anchor line outside the real "## Custo" section —
// neither hijacks the stamp nor reads as the anchor; the real block is found
// outside fences and its last anchor is the one read.
func TestStampIgnoresQuotedExamples(t *testing.T) {
	example := `<!-- cerne:cost v1 {"id":"F-0001","counters":{"input":1,"cache_creation":0,"cache_read":0,"output":0},"messages":999} -->`
	doc := strings.Replace(resultDoc("F-0001"), "Resultado.\n",
		"Resultado.\n\n```markdown\n## Custo\n\n"+example+"\n```\n\n"+example+"\n", 1)
	if _, ok := anchor(doc); ok {
		t.Fatal("a quoted example read as the anchor")
	}
	r := stampReport("F-0001", cc(600, 0, 0, 0), 1)
	got := mustStamp(t, doc, r)
	if !strings.HasPrefix(got, strings.TrimRight(doc, "\n")) || !strings.Contains(got, "| input | 600 |") {
		t.Fatalf("stamp did not append the real block after the examples:\n%s", got)
	}
	a, ok := anchor(got)
	if !ok || a.Counters != cc(600, 0, 0, 0) || a.Messages != 1 {
		t.Fatalf("anchor = %+v (%v), want the real one", a, ok)
	}
	if again := mustStamp(t, got, stampReport("F-0001", cc(700, 0, 0, 0), 2)); strings.Count(again, "| input | 700 |") != 1 ||
		strings.Count(again, "\n## Custo\n") != 2 || !strings.Contains(again, "```markdown\n## Custo\n") {
		t.Fatalf("re-stamp touched the quoted example or duplicated the block:\n%s", again)
	}

	// Two anchors inside the real section (a hand edit): the last one counts.
	twice := got + "\n" + strings.Replace(example, `"messages":999`, `"messages":5`, 1) + "\n"
	if a, ok := anchor(twice); !ok || a.Messages != 5 {
		t.Fatalf("anchor = %+v (%v), want the last one in the section", a, ok)
	}
}

// TestComputeHonoursAnchorAfterPrune (C4): once a feature is stamped, its
// transcripts pruned and its snapshot dropped, its own report and the project
// view read its numbers from the anchor, saying so; while the live read holds
// as many messages, the live read stays the source.
func TestComputeHonoursAnchorAfterPrune(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0005", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "a", req: "r", c: cc(12, 120, 1200, 12000)},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s", "", "F-0005", at(1))
	harvest(t, in, "s", "", time.Now())
	prune(t, in, "s")
	live := compute(t, in, "F-0005")
	if live.Source != "transcripts" {
		t.Fatalf("source = %q before the stamp", live.Source)
	}
	saveFeature(t, in.RepoDir, "F-0005", domain.StateImplemented)
	writeResult(t, in.RepoDir, "F-0005", mustStamp(t, resultDoc("F-0005"), live))
	if err := DropSnapshot(in.CacheDir, "F-0005"); err != nil {
		t.Fatal(err)
	}

	r := compute(t, in, "F-0005")
	if r.Source != "stamp" || r.Counters != live.Counters || r.Sidechain != live.Sidechain || r.Messages != 2 ||
		!reflect.DeepEqual(r.ByModel, live.ByModel) || r.First != live.First || r.Last != live.Last {
		t.Fatalf("after prune + stamp:\n%+v\nwant the stamped numbers of\n%+v", r, live)
	}
	p := compute(t, in, "")
	if p.Entities["F-0005"] != live.Counters || !reflect.DeepEqual(p.FromStamp, []string{"F-0005"}) || p.Counters != live.Counters {
		t.Fatalf("project = %+v, want F-0005 from its stamp", p)
	}
}

// TestStampBlockNamesUnattributedAndRecovered (C10b): the "## Custo" prose
// carries the unattributed bucket and the sessions recovered from a snapshot,
// so a complete total does not read as partial.
func TestStampBlockNamesUnattributedAndRecovered(t *testing.T) {
	r := stampReport("F-0001", cc(1, 2, 3, 4), 2)
	r.Sessions, r.SessionsWithoutTranscript, r.SessionsRecovered, r.Unattributed = 2, 1, 1, cc(5, 0, 0, 5)
	got := mustStamp(t, resultDoc("F-0001"), r)
	for _, want := range []string{"Sessões: 2 (1 sem transcript, 1 recuperada(s) do snapshot)",
		"Não atribuído (mensagens dessas sessões do ledger fora de qualquer ligação): input 5 · cache_creation 0 · cache_read 0 · output 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("block lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Atenção") {
		t.Errorf("a fully recovered session reads as a loss:\n%s", got)
	}
}

// TestAnchorCountersReadsStampedResult pins the process panel's one anchor
// reader (F-0321): the own counters of a stamped result; nothing from a quoted
// example inside a code fence, nor from an empty body.
func TestAnchorCountersReadsStampedResult(t *testing.T) {
	got, err := Stamp("", Report{ID: "F-0001", Counters: Counters{1, 2, 3, 4}, Unattributed: Counters{5, 6, 7, 8}, Messages: 1})
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if c, u, ok := AnchorCounters(got); !ok || c != (Counters{1, 2, 3, 4}) || u != (Counters{5, 6, 7, 8}) {
		t.Fatalf("AnchorCounters = %+v, %+v, %v, want {1 2 3 4}, {5 6 7 8}, true", c, u, ok)
	}
	fenced := "# F-0001\n\n```markdown\n" + got + "```\n"
	if _, _, ok := AnchorCounters(fenced); ok {
		t.Fatalf("AnchorCounters read a fenced example as the anchor")
	}
	if _, _, ok := AnchorCounters(""); ok {
		t.Fatalf("AnchorCounters read an anchor from an empty body")
	}
}

// TestStampBreaksDownByRole (F-0384 crit. 5): with a role other than main the
// "## Custo" block has one line per role and the v1 anchor carries by_role, an
// additive field read back by the next stamp; a main-only cost prints no role
// line; an anchor from before by_role still reads.
func TestStampBreaksDownByRole(t *testing.T) {
	r := stampReport("F-0001", cc(7, 0, 0, 7), 3)
	r.ByRole = map[string]Counters{"main": cc(1, 0, 0, 1), "builder": cc(2, 0, 0, 2), "critic": cc(4, 0, 0, 4)}
	got := mustStamp(t, resultDoc("F-0001"), r)
	const line = "Por papel: builder (input 2 · cache_creation 0 · cache_read 0 · output 2); " +
		"critic (input 4 · cache_creation 0 · cache_read 0 · output 4); main (input 1 · cache_creation 0 · cache_read 0 · output 1).\n"
	if !strings.Contains(got, line) {
		t.Errorf("block lacks the role line %q:\n%s", line, got)
	}
	if a, ok := anchor(got); !ok || !reflect.DeepEqual(a.ByRole, r.ByRole) {
		t.Errorf("anchor by_role = %+v (ok %v), want %+v", a.ByRole, ok, r.ByRole)
	}

	r.ByRole = map[string]Counters{"main": cc(7, 0, 0, 7)}
	if got := mustStamp(t, resultDoc("F-0001"), r); strings.Contains(got, "Por papel") {
		t.Errorf("a main-only cost prints a role line:\n%s", got)
	}

	old := resultDoc("F-0001") + "\n## Custo\n\n<!-- cerne:cost v1 " +
		`{"id":"F-0001","counters":{"input":1,"cache_creation":0,"cache_read":0,"output":0},"sidechain":{"input":0,"cache_creation":0,"cache_read":0,"output":0},"by_model":{},"messages":1,"sessions":1,"first":"","last":"","unattributed":{"input":0,"cache_creation":0,"cache_read":0,"output":0}}` +
		" -->\n"
	if a, ok := anchor(old); !ok || a.Messages != 1 || a.ByRole != nil {
		t.Errorf("pre-F-0384 anchor = %+v (ok %v), want it read, without by_role", a, ok)
	}
}

// TestByRoleNamesTheUnknownGap (F-0384 critic I1): when part of the cost comes
// from snapshot buckets with no role (harvested before F-0384), the gap is the
// explicit "unknown" role — in ByRole, the --json and the stamp line — so the
// breakdown never reads as complete when it is not.
func TestByRoleNamesTheUnknownGap(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	a, x := cc(1, 2, 3, 4), cc(10, 20, 30, 40)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "a", req: "r", c: a},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", c: x})
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	harvest(t, in, "s", "", time.Now())
	s := readSnap(t, in.CacheDir, "F-0002")
	for rel, f := range s.Sessions["s"].Files {
		if strings.Contains(rel, "/subagents/") {
			for i := range f.Buckets {
				f.Buckets[i].Role = "" // as a pre-F-0384 harvest wrote it
			}
		}
	}
	if err := s.save(in.CacheDir); err != nil {
		t.Fatal(err)
	}
	prune(t, in, "s")

	r := compute(t, in, "F-0002")
	if want := map[string]Counters{"main": a, "unknown": x}; !reflect.DeepEqual(r.ByRole, want) || r.Counters != a.add(x) {
		t.Fatalf("ByRole = %+v (counters %+v), want %+v", r.ByRole, r.Counters, want)
	}
	if raw, _ := json.Marshal(r); !strings.Contains(string(raw), `"unknown":{"input":10,`) {
		t.Errorf("--json lacks the unknown role: %s", raw)
	}
	if got := mustStamp(t, resultDoc("F-0002"), r); !strings.Contains(got, "Por papel: main (input 1 · cache_creation 2 · cache_read 3 · output 4); unknown (input 10 · cache_creation 20 · cache_read 30 · output 40).") {
		t.Errorf("stamp lacks the unknown role:\n%s", got)
	}
}
