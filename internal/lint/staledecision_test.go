package lint

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// sdNow is the fixed clock of the stale-decision tests.
var sdNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return sdNow.Add(-time.Duration(n) * 24 * time.Hour) }

func dec(id string, created int, components ...string) store.Decision {
	return store.Decision{ID: id, Title: "Decisão " + id, State: domain.DecisionActive, Components: components, CreatedAt: daysAgo(created), UpdatedAt: daysAgo(created)}
}

func reviewed(d store.Decision, days int) store.Decision {
	at := daysAgo(days)
	d.ReviewedAt = &at
	return d
}

func cites(ids string) string { return "---\ncerne_id: x\n---\n\n# Doc\n\nSegue a " + ids + ".\n" }

// TestStaleDecisionFindings pins F-0385 criteria 1–4 and 10 on fixture
// backends: signal 1 (only closed features cite it, nothing live does, no
// review within N days), signal 2 (every component on a newer active
// decision created after the last review), the live citations that silence
// signal 1, keep valid until a new signal, and the Detail naming the
// citations or the successor and ending with the three exit commands.
func TestStaleDecisionFindings(t *testing.T) {
	closed := store.Feature{ID: "F-0001", Title: "Entregue", State: domain.StateImplemented}
	rejected := store.Feature{ID: "F-0003", Title: "Rejeitada", State: domain.StateRejected}
	live := store.Feature{ID: "F-0002", Title: "Em curso", State: domain.StateInDevelopment}
	retired := dec("D-0003", 10, "k")
	retired.State = domain.DecisionRetired
	for _, c := range []struct {
		name      string
		decisions []store.Decision
		features  []store.Feature
		notes     []store.Note
		runbooks  []store.Runbook
		docs      map[string]string
		want      map[string][]string // flagged entity → substrings of its Detail
		deny      []string            // substrings no Detail may carry
	}{
		{
			name:      "signal 1: only closed features cite it, past N days",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed, rejected},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001"), "content/features/F-0003/plan.md": cites("d-0001")},
			want:      map[string][]string{"D-0001": {"(F-0001 implemented, F-0003 rejected)", "200 days"}},
		},
		{
			name:      "a live feature citing it silences signal 1",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed, live},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001"), "content/features/F-0002/spec.md": cites("D-0001")},
		},
		{
			name:      "an active note's body silences signal 1",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed},
			notes:     []store.Note{{ID: "N-0001", Title: "Nota", State: domain.NoteActive}},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001"), "content/notes/N-0001.md": cites("D-0001")},
		},
		{
			name:      "an active note's --informs silences signal 1",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed},
			notes:     []store.Note{{ID: "N-0001", Title: "Nota", State: domain.NoteActive, Informs: []string{"D-0001"}}},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001")},
		},
		{
			name:      "a superseded note is no live citation",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed},
			notes:     []store.Note{{ID: "N-0001", Title: "Nota", State: domain.NoteSuperseded, Informs: []string{"D-0001"}}},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001"), "content/notes/N-0001.md": cites("D-0001")},
			want:      map[string][]string{"D-0001": {"(F-0001 implemented)"}},
		},
		{
			name:      "an active runbook silences signal 1",
			decisions: []store.Decision{dec("D-0001", 200)},
			features:  []store.Feature{closed},
			runbooks:  []store.Runbook{{ID: "R-0001", Title: "Runbook", State: domain.RunbookActive}},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001"), "content/runbooks/R-0001.md": cites("D-0001")},
		},
		{
			name:      "another active decision silences signal 1; its own doc does not",
			decisions: []store.Decision{dec("D-0001", 200), dec("D-0002", 200)},
			features:  []store.Feature{closed},
			docs: map[string]string{
				"content/features/F-0001/spec.md": cites("D-0001 e D-0002"),
				"content/decisions/D-0002.md":     cites("D-0001 e D-0002"),
			},
			want: map[string][]string{"D-0002": {"(F-0001 implemented)"}},
		},
		{
			name:      "inside the N-day window",
			decisions: []store.Decision{dec("D-0001", 180)},
			features:  []store.Feature{closed},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001")},
		},
		{
			name:      "age alone never flags: nothing cites it",
			decisions: []store.Decision{dec("D-0001", 900)},
			features:  []store.Feature{closed},
		},
		{
			name:      "keep counts N days from the review",
			decisions: []store.Decision{reviewed(dec("D-0001", 400), 10), reviewed(dec("D-0002", 400), 181)},
			features:  []store.Feature{closed},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001 D-0002")},
			want:      map[string][]string{"D-0002": {"181 days"}},
		},
		{
			name:      "signal 2: every component on a newer active decision",
			decisions: []store.Decision{dec("D-0001", 300, "cli", "ui"), dec("D-0002", 100, "ui", "cli", "web")},
			want:      map[string][]string{"D-0001": {"(cli, ui)", "newer active D-0002", "--by D-0002"}},
		},
		{
			name:      "an untagged decision never matches signal 2",
			decisions: []store.Decision{dec("D-0001", 300), dec("D-0002", 10, "x")},
		},
		{
			name:      "the older of two is flagged; a retired one is no successor",
			decisions: []store.Decision{dec("D-0001", 300, "k"), dec("D-0002", 400, "k"), retired},
			want:      map[string][]string{"D-0002": {"newer active D-0001"}},
			deny:      []string{"D-0003"},
		},
		{
			name:      "keep holds signal 2 until a newer overlap appears",
			decisions: []store.Decision{reviewed(dec("D-0001", 300, "k"), 50), dec("D-0002", 100, "k", "j")},
		},
		{
			name:      "an overlap created after the keep flags it again",
			decisions: []store.Decision{reviewed(dec("D-0001", 300, "k"), 50), dec("D-0002", 100, "k", "j"), dec("D-0004", 10, "k")},
			want:      map[string][]string{"D-0001": {"newer active D-0004"}},
			deny:      []string{"active D-0002"},
		},
		{
			name:      "both signals land in one finding",
			decisions: []store.Decision{dec("D-0001", 200, "k"), dec("D-0002", 10, "k")},
			features:  []store.Feature{closed},
			docs:      map[string]string{"content/features/F-0001/spec.md": cites("D-0001")},
			want:      map[string][]string{"D-0001": {"(F-0001 implemented)", "newer active D-0002", "--by D-0002"}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, body := range c.docs {
				writeDoc(t, dir, rel, body)
			}
			got, err := staleDecisionFindings(dir, c.features, c.decisions, c.notes, c.runbooks, 0, sdNow)
			if err != nil {
				t.Fatalf("staleDecisionFindings: %v", err)
			}
			var flagged []string
			for _, f := range got {
				flagged = append(flagged, f.Entity)
				if f.Rule != RuleStaleDecision {
					t.Errorf("rule = %q, want %q", f.Rule, RuleStaleDecision)
				}
				for _, sub := range c.want[f.Entity] {
					if !strings.Contains(f.Detail, sub) {
						t.Errorf("%s detail lacks %q: %s", f.Entity, sub, f.Detail)
					}
				}
				for _, sub := range c.deny {
					if strings.Contains(f.Detail, sub) {
						t.Errorf("%s detail carries %q: %s", f.Entity, sub, f.Detail)
					}
				}
				// Criterion 10: the Detail ends with the three exit commands.
				by := "D-…"
				if i := strings.Index(f.Detail, "--by "); i >= 0 {
					by = strings.Fields(f.Detail[i+5:])[0]
				}
				exits := fmt.Sprintf("confirm it with 'cerne decision keep %[1]s', retire it with 'cerne decision retire %[1]s --reason \"…\"' or supersede it with 'cerne decision substituir %[1]s --by %[2]s --reason \"…\"'", f.Entity, by)
				if !strings.HasSuffix(f.Detail, exits) {
					t.Errorf("%s detail does not end with the exits %q: %s", f.Entity, exits, f.Detail)
				}
			}
			want := slices.Sorted(func(yield func(string) bool) {
				for id := range c.want {
					if !yield(id) {
						return
					}
				}
			})
			slices.Sort(flagged)
			if !slices.Equal(flagged, want) {
				t.Fatalf("flagged %v, want %v: %+v", flagged, want, got)
			}
		})
	}
}

// TestStaleDecisionIsAdvisoryAndWired pins F-0385 criteria 7 and 9: the rule
// is advisory (never fails the lint), sits right after stale-note in the
// report order, runs from RunConfig and leaves every decision as it was.
func TestStaleDecisionIsAdvisoryAndWired(t *testing.T) {
	if !Advisory(RuleStaleDecision) {
		t.Fatal("stale-decision must be advisory")
	}
	i := slices.Index(RuleOrder, RuleStaleNote)
	if i < 0 || i+1 >= len(RuleOrder) || RuleOrder[i+1] != RuleStaleDecision {
		t.Fatalf("stale-decision must follow stale-note in RuleOrder: %v", RuleOrder)
	}
	for rank, rule := range RuleOrder {
		if got, ok := ruleRank[rule]; !ok || got != rank {
			t.Errorf("ruleRank[%s] = %d (present %v), want %d", rule, got, ok, rank)
		}
	}

	dir := t.TempDir()
	old := time.Now().UTC().Add(-400 * 24 * time.Hour)
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Entregue", State: domain.StateImplemented, CreatedAt: old, UpdatedAt: old}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	d := store.Decision{ID: "D-0001", Title: "Antiga", State: domain.DecisionActive, Facets: []string{"code"}, CreatedAt: old, UpdatedAt: old}
	if err := store.SaveDecision(dir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", cites("D-0001"))
	got, err := RunConfig(dir, 14, 8)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var n int
	for _, f := range got {
		if f.Rule == RuleStaleDecision && f.Entity == "D-0001" {
			n++
			if Blocking(f) {
				t.Errorf("stale-decision finding blocks the lint: %+v", f)
			}
		}
	}
	if n != 1 {
		t.Fatalf("stale-decision findings for D-0001 = %d, want 1: %+v", n, got)
	}
	after, err := store.LoadDecision(dir, "D-0001")
	if err != nil || after.State != domain.DecisionActive || after.ReviewedAt != nil || !after.UpdatedAt.Equal(old) {
		t.Fatalf("decision after lint = %+v (%v), want untouched", after, err)
	}
}
