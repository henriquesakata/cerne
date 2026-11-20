package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// contextFixture is a backend under <tmp>/repo with its index at
// <tmp>/index.db, the layout of the cache: task T-0002 under epic E-0001 and
// a neighbour of every ring (E-0005 verification 4).
func contextFixture(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	now := time.Now().UTC()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	feat := func(id, title, parent string, st domain.FeatureState, comps ...string) {
		t.Helper()
		if err := store.SaveFeature(repo, store.Feature{ID: id, Title: title, Parent: parent, State: st, Components: comps, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	note := func(id string, st domain.NoteState, gist string, informs, when []string, body string, comps ...string) {
		t.Helper()
		if err := store.SaveNote(repo, store.Note{ID: id, Title: "Note " + id, State: st, Gist: gist, Informs: informs, When: when, Components: comps, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		write("content/notes/"+id+".md", "---\ncerne_id: "+id+"\n---\n\n# Note "+id+"\n\n"+body+"\n")
	}
	ref := func(id string, st domain.ReferenceState, informs []string, comps ...string) {
		t.Helper()
		if err := store.SaveReference(repo, store.Reference{ID: id, Title: "Source " + id, State: st, Informs: informs, Components: comps, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		write("content/references/"+id+".md", "# Source "+id+"\n\nreference body\n")
	}
	dec := func(id string, st domain.DecisionState, informs []string, comps ...string) {
		t.Helper()
		if err := store.SaveDecision(repo, store.Decision{ID: id, Title: "Decision " + id, State: st, Informs: informs, Components: comps, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		write("content/decisions/"+id+".md", "---\ncerne_id: "+id+"\n---\n\n# Decision "+id+"\n\nBody of "+id+".\n")
	}
	rb := func(id string, st domain.RunbookState, comps ...string) {
		t.Helper()
		if err := store.SaveRunbook(repo, store.Runbook{ID: id, Title: "Runbook " + id, State: st, Components: comps, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		write("content/runbooks/"+id+".md", "# Runbook "+id+"\n\nrunbook body\n")
	}

	feat("E-0001", "Epic of the package", "", domain.StateInDevelopment, "ui")
	write("content/features/E-0001/spec.md", "# Epic\n\n## Problem\n\nThe epic cites [[D-0001]] and [[N-0004]].\n")
	write("content/features/E-0001/plan.md", "# Epic plan\n\nTask 2 has no plan of its own.\n")
	feat("T-0003", "A task planned in its epic", "E-0001", domain.StateInDevelopment)
	write("content/features/T-0003/spec.md", "# Task 3\n\n## Problem\n\nThe how is the epic's plan.\n")
	feat("T-0002", "Assemble the package of a task", "E-0001", domain.StateInDevelopment, "cli", "hooks")
	write("content/features/T-0002/spec.md", "# Task\n\n## Problem\n\nThe package gathers what informs a task from the search index.\n\n"+
		"It follows [[N-0001]], not the done [[F-0009]], next to the live [[F-0010]], the source [[R-0001]] and the runbook [[RB-0001]]; "+
		"the superseded [[N-0013]], the resolved [[P-0001]], the open [[P-0002]], the promoted [[I-0001]], the untitled [[I-0002]] and the done [[W-0001]].\n")
	write("content/features/T-0002/plan.md", "# Plan\n\nTouch `internal/cli/contextcmd.go` and `internal/hooks/rules.go:12`, never `https://example.com/a/b`; branch from `origin/main`, the folder `internal/lint` aside.\n")
	feat("F-0009", "Done feature", "", domain.StateDone, "cli")
	feat("F-0010", "Live feature", "", domain.StateApproved)
	feat("F-0011", "Rejected proposal", "", domain.StateRejected, "hooks")
	feat("F-0012", "Superseded proposal", "", domain.StateSuperseded, "cli")
	feat("F-0020", "Zzz qwerty", "", domain.StateCandidate)
	write("content/features/F-0020/spec.md", "# Lonely\n\n## Problem\n\nXyzzy plugh.\n")
	write("content/features/F-0009/spec.md", "# Done\n\ndone body\n")

	note("N-0001", domain.NoteActive, "gist one", nil, nil, "Body of N-0001.\n\nIts second paragraph.")
	note("N-0002", domain.NoteActive, "", []string{"T-0002"}, nil, "Body of N-0002.")
	note("N-0003", domain.NoteActive, "", nil, []string{"edit:internal/hooks/**"}, "Plain words.")
	note("N-0004", domain.NoteActive, "", nil, nil, "<!-- Record the durable knowledge here. Replace this line. -->") // a stub: no body
	note("N-0005", domain.NoteActive, "", nil, nil, "Mind internal/hooks/rules.go and internal/cli/contextcmd.go before editing.")
	note("N-0006", domain.NoteActive, "", nil, nil, "Plain words.", "hooks")
	note("N-0007", domain.NoteActive, "", nil, nil, "Plain words.", "cli")
	note("N-0008", domain.NoteActive, "", nil, nil, "Plain words.", "cli", "hooks")
	note("N-0009", domain.NoteActive, "", nil, nil, "How the package gathers from the search index.")
	note("N-0011", domain.NoteActive, "", nil, nil, "Plain words.", "cli")
	note("N-0012", domain.NoteActive, "", nil, nil, "Rebase on origin/main, never internal/lint/x.go.")
	note("N-0013", domain.NoteSuperseded, "", nil, nil, "Old words.")
	note("N-0014", domain.NoteSuperseded, "", []string{"T-0002"}, nil, "Old words.")
	for id, st := range map[string]domain.PendingState{"P-0001": domain.PendingResolved, "P-0002": domain.PendingOpen} {
		if err := store.SavePending(repo, store.Pending{ID: id, Title: "Pending " + id, State: st, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range []store.Idea{
		{ID: "I-0001", Title: "Promoted idea", State: domain.IdeaPromoted, CreatedAt: now},
		{ID: "I-0002", Body: "First line of the idea\nmore of it", State: domain.IdeaActive, CreatedAt: now},
	} {
		if err := store.SaveIdea(repo, i); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveWork(repo, store.Work{ID: "W-0001", Title: "Finished work", State: domain.WorkDone}); err != nil {
		t.Fatal(err)
	}
	note("N-0010", domain.NoteSuperseded, "", nil, nil, "The package of a task from the search index.", "cli")
	ref("R-0001", domain.ReferenceCurrent, nil)
	ref("R-0002", domain.ReferenceCurrent, []string{"N-0001"})
	ref("R-0003", domain.ReferenceCurrent, []string{"T-0002"}, "hooks") // in ring (a) and on demand
	ref("R-0004", domain.ReferenceCurrent, nil, "hooks")
	dec("D-0001", domain.DecisionActive, nil, "cli") // in ring (a) and on demand
	dec("D-0003", domain.DecisionActive, nil, "cli")
	dec("D-0004", domain.DecisionSuperseded, nil, "cli")
	dec("D-0005", domain.DecisionActive, []string{"T-0002"}) // informs the task (T-0018)
	dec("D-0006", domain.DecisionSuperseded, []string{"T-0002"})
	rb("RB-0001", domain.RunbookActive)
	rb("RB-0002", domain.RunbookActive, "cli")
	if err := index.Rebuild(repo, filepath.Join(filepath.Dir(repo), "index.db"), time.Time{}); err != nil {
		t.Fatal(err)
	}
	return repo
}

type ringID struct{ Ring, ID string }

func ringIDs(items []ContextItem) []ringID {
	out := []ringID{}
	for _, it := range items {
		out = append(out, ringID{it.Ring, it.ID})
	}
	return out
}

// E-0005 verification 4 and 13 (criteria 6 and 19): the rings in order — the
// task's own links before the epic's, a done feature never — the stage
// document, the epic, a note's sources and the on-demand counts.
func TestTaskContextRings(t *testing.T) {
	repo := contextFixture(t)
	p, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	want := []ringID{
		{"a", "N-0001"}, {"a", "F-0010"}, {"a", "R-0001"}, {"a", "RB-0001"}, // the task's spec cites them, in order
		{"a", "P-0002"}, {"a", "I-0002"}, // in force; the superseded note, the resolved pending, the promoted idea and the done work are not
		{"a", "N-0002"}, {"a", "D-0005"}, {"a", "R-0003"}, // they inform the task (the superseded N-0014 and D-0006 do not count)
		{"a", "D-0001"}, {"a", "N-0004"}, // the epic's spec cites them
		{"b", "N-0003"}, {"b", "N-0005"}, // a trigger, then a citation, of a path the plan cites
		{"c", "N-0006"}, {"c", "N-0008"}, {"c", "N-0007"}, {"c", "N-0011"}, // hooks (2 active notes) before cli (3); N-0008 enters by hooks
		{"d", "N-0009"}, // the index, by the title and the problem
	}
	if got := ringIDs(p.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("rings:\n got %v\nwant %v", got, want)
	}
	if p.Doc.Name != "plan" || !strings.Contains(p.Doc.Body, "internal/cli/contextcmd.go") {
		t.Fatalf("stage document = %q, want the whole plan:\n%s", p.Doc.Name, p.Doc.Body)
	}
	if p.Epic == nil || p.Epic.ID != "E-0001" || p.Epic.Title != "Epic of the package" {
		t.Fatalf("epic = %+v", p.Epic)
	}
	byID := map[string]ContextItem{}
	for _, it := range p.Items {
		byID[it.ID] = it
	}
	if n := byID["N-0001"]; n.Gist != "gist one" || !reflect.DeepEqual(n.Sources, []string{"R-0002"}) || n.Kind != "note" {
		t.Fatalf("N-0001 = %+v, want its gist and the reference that sustains it", n)
	}
	// T-0018 (criteria 6 and 16): the notes and decisions in force the author
	// linked come with their body — no frontmatter, no title heading, a stub
	// none — and every other item stays an index line.
	wantBody := map[string]string{
		"N-0001": "Body of N-0001.\n\nIts second paragraph.",
		"N-0002": "Body of N-0002.",
		"D-0001": "Body of D-0001.",
		"D-0005": "Body of D-0005.",
	}
	for _, it := range p.Items {
		if it.Body != wantBody[it.ID] {
			t.Errorf("%s [%s] body = %q, want %q", it.ID, it.Ring, it.Body, wantBody[it.ID])
		}
	}
	if text := p.Text(); !strings.Contains(text, "[a] N-0001  note  Note N-0001 — gist one  · sources R-0002\n  Body of N-0001.\n\n  Its second paragraph.\n[a] F-0010") ||
		!strings.Contains(text, "[a] D-0005  decision  Decision D-0005\n  Body of D-0005.\n[a] R-0003") {
		t.Errorf("a body is not right under its item line:\n%s", text)
	}
	if i := byID["I-0002"]; i.Title != "First line of the idea" || i.Kind != "idea" {
		t.Fatalf("an untitled idea = %+v, want its first line as the title", i)
	}
	if byID["N-0003"].Why != "edit:internal/hooks/**" || byID["N-0005"].Why != "internal/cli/contextcmd.go" || byID["N-0006"].Why != "hooks" {
		t.Fatalf("why: %+v %+v %+v", byID["N-0003"], byID["N-0005"], byID["N-0006"])
	}
	// Each count is what its command lists (critic M3), saying how many of
	// them ring (a) already listed; with no list command, the ids themselves.
	wantOnDemand := []OnDemand{
		{What: "decisions", Count: 2, Listed: 1, Command: "cerne decision list --component cli,hooks"},
		{What: "references", Count: 2, Listed: 1, Command: "cerne reference list --component cli,hooks"},
		{What: "runbooks", Count: 1, Command: "cerne show RB-0002"},
		{What: "rejected or superseded proposals", Count: 2, Command: "cerne show <id>: F-0011, F-0012"},
	}
	if !reflect.DeepEqual(p.OnDemand, wantOnDemand) {
		t.Fatalf("on demand:\n got %+v\nwant %+v", p.OnDemand, wantOnDemand)
	}
	text := p.Text()
	if !strings.Contains(text, "decisions: 2 (1 of them listed above) — cerne decision list --component cli,hooks\n") {
		t.Fatalf("the close does not say what ring (a) already listed:\n%s", text)
	}
	for _, gone := range []string{"F-0009", "] F-0011", "] F-0012", "N-0010", "D-0004", "D-0006", "done body", "reference body", "runbook body", "Plain words", "Body of D-0003", "Old words"} { // the proposals only as ids of the close
		if strings.Contains(text, gone) {
			t.Errorf("the package shows %q:\n%s", gone, text)
		}
	}
}

// Verification 13 without matches: a task sharing no component counts zero,
// and an entity with no neighbour at all has an empty neighbourhood.
func TestTaskContextEmpty(t *testing.T) {
	repo := contextFixture(t)
	p, err := TaskContext(repo, "F-0020", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 0 || p.Epic != nil || p.Doc.Name != "spec" {
		t.Fatalf("lonely feature: %+v", p)
	}
	for _, o := range p.OnDemand {
		if o.Count != 0 {
			t.Fatalf("on demand with no component: %+v", p.OnDemand)
		}
	}
	if _, err := TaskContext(repo, "N-0001", DefaultContextBudget); !errors.Is(err, ErrNotATask) {
		t.Fatalf("a note: %v, want ErrNotATask", err)
	}
}

// Verification 5 (criterion 7): past the budget the rings are cut from (d)
// to (a), what is left is id and title, the omitted count is per ring, and
// the task's document is never cut.
func TestTaskContextBudget(t *testing.T) {
	repo := contextFixture(t)
	full, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	tiny, err := TaskContext(repo, "T-0002", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiny.Items) != 0 || tiny.Doc != full.Doc || !reflect.DeepEqual(tiny.Omitted, map[string]int{"a": 11, "b": 2, "c": 4, "d": 1}) {
		t.Fatalf("budget 1: items %v, omitted %v, doc kept %v", ringIDs(tiny.Items), tiny.Omitted, tiny.Doc == full.Doc)
	}
	if last := lastLine(tiny.Text()); !strings.Contains(last, "left out every neighbourhood item: a 11 · b 2 · c 4 · d 1") || !strings.Contains(last, "cerne show <id>") || strings.Contains(last, "cut the list") {
		t.Fatalf("last line %q does not say what was omitted and how to read it", last)
	}
	// A budget that holds the fixed part and a few short lines.
	fixed := (len(tiny.Text()) + 3) / 4
	mid, err := TaskContext(repo, "T-0002", fixed+30)
	if err != nil {
		t.Fatal(err)
	}
	if !mid.Compact || len(mid.Items) == 0 || len(mid.Items) == len(full.Items) {
		t.Fatalf("mid budget: compact=%v kept %d of %d", mid.Compact, len(mid.Items), len(full.Items))
	}
	if !reflect.DeepEqual(ringIDs(mid.Items), ringIDs(full.Items)[:len(mid.Items)]) {
		t.Fatalf("the cut is not from the end (d to a): %v", ringIDs(mid.Items))
	}
	omitted := 0
	for _, n := range mid.Omitted {
		omitted += n
	}
	if omitted != len(full.Items)-len(mid.Items) || mid.Omitted["d"] != 1 {
		t.Fatalf("omitted %v for %d dropped", mid.Omitted, len(full.Items)-len(mid.Items))
	}
	if est := (len(mid.Text()) + 3) / 4; est > fixed+30 {
		t.Fatalf("text of ~%d tokens past the budget %d", est, fixed+30)
	}
	for _, it := range mid.Items {
		if strings.Contains(mid.Text(), it.ID+"  note  ") || strings.Contains(mid.Text(), "gist one") {
			t.Fatalf("compact lines keep only id and title:\n%s", mid.Text())
		}
	}
}

// Verification 11, second half (criterion 16): the package comes out the same
// with every other entity's body unreadable — it reads the bodies of the task,
// its epic and the notes and decisions in force of ring (a) only (T-0018); the
// rest is metadata and the index.
func TestTaskContextReadsOnlyItsOwnBodies(t *testing.T) {
	repo := contextFixture(t)
	before, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	beforeIdx, err := TaskIndex(repo, "T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if before.Items[0].ID != "N-0001" || before.Items[0].Body == "" {
		t.Fatalf("the probe needs a ring (a) body: %+v", before.Items[0])
	}
	// The superseded N-0013, N-0014 and D-0006 are linked too, and locked.
	readable := []string{"T-0002", "E-0001", "N-0001", "N-0002", "N-0004", "D-0001", "D-0005"}
	var locked []string
	err = filepath.WalkDir(filepath.Join(repo, "content"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || slices.ContainsFunc(readable, func(id string) bool { return strings.Contains(p, id) }) {
			return err
		}
		locked = append(locked, p)
		return os.Chmod(p, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, p := range locked {
			_ = os.Chmod(p, 0o644)
		}
	})
	if _, err := os.ReadFile(locked[0]); err == nil {
		t.Skip("running as a user that reads mode 000 files (root): the probe proves nothing here")
	}
	after, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("the package changed with the other bodies unreadable:\n%s\n---\n%s", before.Text(), after.Text())
	}
	afterIdx, err := TaskIndex(repo, "T-0002")
	if err != nil || !reflect.DeepEqual(afterIdx, beforeIdx) {
		t.Fatalf("the index changed: %v", err)
	}
}

// Criterion 9/15: the binding index is the notes of rings (a) to (c) only —
// no ring (d), no other kind — and never loads every feature (an unreadable
// feature file elsewhere in the backend does not stop it).
func TestTaskIndexNotesOfRingsAToC(t *testing.T) {
	repo := contextFixture(t)
	if err := os.Chmod(filepath.Join(repo, "entities", "features", "F-0011.json"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(repo, "entities", "features", "F-0011.json"), 0o644) })
	items, err := TaskIndex(repo, "T-0002")
	if err != nil {
		t.Fatal(err)
	}
	want := []ringID{{"a", "N-0001"}, {"a", "N-0002"}, {"a", "N-0004"}, {"b", "N-0003"}, {"b", "N-0005"}, {"c", "N-0006"}, {"c", "N-0008"}, {"c", "N-0007"}, {"c", "N-0011"}}
	if got := ringIDs(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("index:\n got %v\nwant %v", got, want)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// Critic L3: a task whose plan is its epic's says where the plan is; a task
// with its own plan does not.
func TestTaskContextPointsAtTheEpicPlan(t *testing.T) {
	repo := contextFixture(t)
	p, err := TaskContext(repo, "T-0003", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	if p.EpicPlan != "cerne show E-0001 --doc plan" || !strings.Contains(p.Text(), "the plan is the epic's: cerne show E-0001 --doc plan\n") {
		t.Fatalf("epic plan = %q:\n%s", p.EpicPlan, p.Text())
	}
	own, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil || own.EpicPlan != "" {
		t.Fatalf("a task with its own plan: %q %v", own.EpicPlan, err)
	}
}

// Critic L9: a note citing two paths of the same tier says the same one every
// time (the first in order), never the one a map iteration happened on.
func TestNotesCitingPathsIsStable(t *testing.T) {
	repo := contextFixture(t)
	db := filepath.Join(filepath.Dir(repo), "index.db")
	for range 30 {
		got := notesCitingPaths(db, []string{"internal/hooks/rules.go", "internal/cli/contextcmd.go"})
		if c := got["N-0005"]; c.why != "internal/cli/contextcmd.go" || c.score != 3 {
			t.Fatalf("N-0005 = %+v, want the first whole path in order", c)
		}
	}
}
