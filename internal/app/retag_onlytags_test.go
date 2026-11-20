package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

// retagEventCount counts the events of one type for one entity.
func retagEventCount(t *testing.T, repoDir, id, typ string) int {
	t.Helper()
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	n := 0
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == typ {
			n++
		}
	}
	return n
}

// TestRetagOfClosedEntityChangesOnlyTags (F-0482 criterion 4): a closed entity of
// each kind keeps state, links, lifecycle dates and errata; only its tags change
// and the retag event lands in the log. UpdatedAt is the one other field that
// moves, on purpose: a retag stamps it exactly as a retag of a live entity
// always did, and it is not a lifecycle date.
func TestRetagOfClosedEntityChangesOnlyTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	del := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	type kase struct {
		id, event string
		before    any
		retag     func() (any, error)
		// strip returns a copy with tags and UpdatedAt zeroed, and the UpdatedAt.
		strip func(v any) (any, time.Time)
	}
	f := store.Feature{ID: "F-0001", Title: "Feita", State: domain.StateDone, Parent: "E-0001", Reason: "r", SupersededBy: "F-0009",
		Fixes: "F-0002", FromIdea: "I-0001", Capabilities: []string{"C-0001"}, CreatedAt: at, UpdatedAt: at, Facets: []string{"auth"}, Components: []string{"old"}}
	rej := store.Feature{ID: "F-0003", Title: "Rejeitada", State: domain.StateRejected, Reason: "no", CreatedAt: at, UpdatedAt: at}
	n := store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteSuperseded, SupersededBy: "N-0002", Reason: "r", Informs: []string{"F-0001"}, CreatedAt: at, UpdatedAt: at}
	d := store.Decision{ID: "D-0001", Title: "Dec", State: domain.DecisionSuperseded, SupersededBy: "D-0002", Reason: "r", ReviewedAt: &del, CreatedAt: at, UpdatedAt: at}
	r := store.Reference{ID: "R-0001", Title: "Ref", State: domain.ReferenceObsolete, URL: "https://example.com/x", External: true, Informs: []string{"F-0001"}, CreatedAt: at, UpdatedAt: at}
	rb := store.Runbook{ID: "RB-0001", Title: "Rb", State: domain.RunbookSuperseded, SupersededBy: "RB-0002", Reason: "r", CreatedAt: at, UpdatedAt: at}
	for _, err := range []error{store.SaveFeature(repoDir, f), store.SaveFeature(repoDir, rej), store.SaveNote(repoDir, n),
		store.SaveDecision(repoDir, d), store.SaveReference(repoDir, r), store.SaveRunbook(repoDir, rb)} {
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	commitBackend(t, repoDir)
	// An erratum on the done feature: its body is immutable, so it must survive.
	if _, err := AddErrata(repoDir, projDir, f.ID, "uma correcao", "tester"); err != nil {
		t.Fatalf("AddErrata: %v", err)
	}
	errataBefore, err := store.LoadErrata(repoDir, f.ID)
	if err != nil || errataBefore == "" {
		t.Fatalf("errata not stored: %q, %v", errataBefore, err)
	}

	stripF := func(v any) (any, time.Time) {
		x := v.(store.Feature)
		u := x.UpdatedAt
		x.Facets, x.Components, x.UpdatedAt = nil, nil, time.Time{}
		return x, u
	}
	cases := []kase{
		{f.ID, "feature.retagged", f, func() (any, error) {
			return RetagFeature(repoDir, projDir, f.ID, []string{"code"}, []string{"gitback"}, true, true)
		}, stripF},
		{rej.ID, "feature.retagged", rej, func() (any, error) { return RetagFeature(repoDir, projDir, rej.ID, []string{"code"}, nil, true, false) }, stripF},
		{n.ID, "note.retagged", n, func() (any, error) { return RetagNote(repoDir, projDir, n.ID, []string{"infra"}, nil, true, false) },
			func(v any) (any, time.Time) {
				x := v.(store.Note)
				u := x.UpdatedAt
				x.Facets, x.Components, x.UpdatedAt = nil, nil, time.Time{}
				return x, u
			}},
		{d.ID, "decision.retagged", d, func() (any, error) { return RetagDecision(repoDir, projDir, d.ID, []string{"data"}, nil, true, false) },
			func(v any) (any, time.Time) {
				x := v.(store.Decision)
				u := x.UpdatedAt
				x.Facets, x.Components, x.UpdatedAt = nil, nil, time.Time{}
				return x, u
			}},
		{r.ID, "reference.retagged", r, func() (any, error) { return RetagReference(repoDir, projDir, r.ID, []string{"code"}, nil, true, false) },
			func(v any) (any, time.Time) {
				x := v.(store.Reference)
				u := x.UpdatedAt
				x.Facets, x.Components, x.UpdatedAt = nil, nil, time.Time{}
				return x, u
			}},
		{rb.ID, "runbook.retagged", rb, func() (any, error) {
			return RetagRunbook(repoDir, projDir, rb.ID, []string{"process"}, nil, true, false)
		},
			func(v any) (any, time.Time) {
				x := v.(store.Runbook)
				u := x.UpdatedAt
				x.Facets, x.Components, x.UpdatedAt = nil, nil, time.Time{}
				return x, u
			}},
	}
	for _, c := range cases {
		got, err := c.retag()
		if err != nil {
			t.Fatalf("retag %s: %v", c.id, err)
		}
		wantStripped, _ := c.strip(c.before)
		gotStripped, gotUpdated := c.strip(got)
		// Everything but tags and UpdatedAt is identical: state, links, reason,
		// CreatedAt, ReviewedAt (the lifecycle dates).
		if !reflect.DeepEqual(gotStripped, wantStripped) {
			t.Fatalf("%s changed beyond tags:\n got %+v\nwant %+v", c.id, gotStripped, wantStripped)
		}
		if !gotUpdated.After(at) {
			t.Fatalf("%s UpdatedAt = %v, want it stamped by the retag", c.id, gotUpdated)
		}
		if k := retagEventCount(t, repoDir, c.id, c.event); k != 1 {
			t.Fatalf("%s events for %s = %d, want 1", c.event, c.id, k)
		}
	}
	if got, _ := store.LoadErrata(repoDir, f.ID); got != errataBefore {
		t.Fatalf("errata changed by a retag:\n got %q\nwant %q", got, errataBefore)
	}
}

// TestRetagDoesNotMoveFlowStats (F-0482 criterion 5): the flow metrics computed
// from the event log are identical before and after a retag of a done feature, an idea and a pending.
func TestRetagDoesNotMoveFlowStats(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Feita", State: domain.StateDone}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	for i, typ := range []string{"feature.created", "feature.started", "feature.completed"} {
		ev := domain.Event{ID: "e" + typ, EntityID: "F-0001", Type: typ, At: day(1 + 2*i)}
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	commitBackend(t, repoDir)
	compute := func() stats.Metrics {
		evs, err := store.ReadEvents(repoDir)
		if err != nil {
			t.Fatalf("ReadEvents: %v", err)
		}
		m, err := stats.Compute(evs, time.Time{})
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		return m
	}
	if err := store.SaveIdea(repoDir, store.Idea{ID: "I-0001", Title: "i", State: domain.IdeaActive}); err != nil {
		t.Fatalf("SaveIdea: %v", err)
	}
	if err := store.SavePending(repoDir, store.Pending{ID: "P-0001", Title: "p", From: "F-0001", State: domain.PendingOpen}); err != nil {
		t.Fatalf("SavePending: %v", err)
	}
	commitBackend(t, repoDir)
	before := compute()
	if _, err := RetagFeature(repoDir, projDir, "F-0001", []string{"code"}, nil, true, false); err != nil {
		t.Fatalf("RetagFeature: %v", err)
	}
	if _, err := RetagIdea(repoDir, projDir, "I-0001", []string{"code"}, nil, true, false); err != nil {
		t.Fatalf("RetagIdea: %v", err)
	}
	if _, err := RetagPending(repoDir, projDir, "P-0001", []string{"code"}, nil, true, false); err != nil {
		t.Fatalf("RetagPending: %v", err)
	}
	if after := compute(); !reflect.DeepEqual(before, after) {
		t.Fatalf("stats moved by a retag:\nbefore %+v\n after %+v", before, after)
	}
}
