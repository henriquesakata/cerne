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

// TestRetagOfClosedEntityChangesOnlyTags (F-0482 criterion 4): a done feature, a
// superseded note and a superseded decision keep state, links and lifecycle
// dates; the retag event lands in the log.
func TestRetagOfClosedEntityChangesOnlyTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	del := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	f := store.Feature{ID: "F-0001", Title: "Feita", State: domain.StateDone, Parent: "E-0001", Reason: "r", SupersededBy: "F-0009",
		Fixes: "F-0002", FromIdea: "I-0001", Capabilities: []string{"C-0001"}, CreatedAt: at, UpdatedAt: at, Facets: []string{"auth"}, Components: []string{"old"}}
	n := store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteSuperseded, SupersededBy: "N-0002", Reason: "r", Informs: []string{"F-0001"}, CreatedAt: at, UpdatedAt: at}
	d := store.Decision{ID: "D-0001", Title: "Dec", State: domain.DecisionSuperseded, SupersededBy: "D-0002", Reason: "r", ReviewedAt: &del, CreatedAt: at, UpdatedAt: at}
	for _, err := range []error{store.SaveFeature(repoDir, f), store.SaveNote(repoDir, n), store.SaveDecision(repoDir, d)} {
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	commitBackend(t, repoDir)

	gotF, err := RetagFeature(repoDir, projDir, f.ID, []string{"code"}, []string{"gitback"}, true, true)
	if err != nil {
		t.Fatalf("RetagFeature: %v", err)
	}
	gotN, err := RetagNote(repoDir, projDir, n.ID, []string{"infra"}, nil, true, false)
	if err != nil {
		t.Fatalf("RetagNote: %v", err)
	}
	gotD, err := RetagDecision(repoDir, projDir, d.ID, []string{"data"}, nil, true, false)
	if err != nil {
		t.Fatalf("RetagDecision: %v", err)
	}

	// Only Facets, Components and UpdatedAt may differ.
	gotF.Facets, gotF.Components, gotF.UpdatedAt = f.Facets, f.Components, f.UpdatedAt
	gotN.Facets, gotN.Components, gotN.UpdatedAt = n.Facets, n.Components, n.UpdatedAt
	gotD.Facets, gotD.Components, gotD.UpdatedAt = d.Facets, d.Components, d.UpdatedAt
	if !reflect.DeepEqual(gotF, f) {
		t.Fatalf("feature changed beyond tags:\n got %+v\nwant %+v", gotF, f)
	}
	if !reflect.DeepEqual(gotN, n) {
		t.Fatalf("note changed beyond tags:\n got %+v\nwant %+v", gotN, n)
	}
	if !reflect.DeepEqual(gotD, d) {
		t.Fatalf("decision changed beyond tags:\n got %+v\nwant %+v", gotD, d)
	}
	for id, typ := range map[string]string{f.ID: "feature.retagged", n.ID: "note.retagged", d.ID: "decision.retagged"} {
		if c := retagEventCount(t, repoDir, id, typ); c != 1 {
			t.Fatalf("%s events for %s = %d, want 1", typ, id, c)
		}
	}
}

// TestRetagDoesNotMoveFlowStats (F-0482 criterion 5): the flow metrics computed
// from the event log are identical before and after a retag of a done feature.
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
	before := compute()
	if _, err := RetagFeature(repoDir, projDir, "F-0001", []string{"code"}, nil, true, false); err != nil {
		t.Fatalf("RetagFeature: %v", err)
	}
	if after := compute(); !reflect.DeepEqual(before, after) {
		t.Fatalf("stats moved by a retag:\nbefore %+v\n after %+v", before, after)
	}
}
