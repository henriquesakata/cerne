package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestStaleNoteFindings pins the stale-note rule: an active note whose --informs
// targets are ALL terminal (feature implemented/rejected/superseded, decision
// superseded) is flagged; a note with any live target, no targets, or a note that
// is itself already superseded is left alone.
func TestStaleNoteFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	features := []store.Feature{
		{ID: "F-0001", Title: "Concluída", State: domain.StateImplemented},
		{ID: "F-0002", Title: "Em curso", State: domain.StateInDevelopment},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	decisions := []store.Decision{
		{ID: "D-0001", Title: "Vigente", State: domain.DecisionActive},
		{ID: "D-0002", Title: "Antiga", State: domain.DecisionSuperseded},
	}
	for _, d := range decisions {
		d.CreatedAt, d.UpdatedAt = now, now
		if err := store.SaveDecision(dir, d); err != nil {
			t.Fatalf("SaveDecision: %v", err)
		}
	}
	notes := []store.Note{
		{ID: "N-0001", Title: "informa só feature concluída", State: domain.NoteActive, Informs: []string{"F-0001"}},            // stale
		{ID: "N-0002", Title: "informa feature em curso", State: domain.NoteActive, Informs: []string{"F-0002"}},               // live target
		{ID: "N-0003", Title: "informa concluída e em curso", State: domain.NoteActive, Informs: []string{"F-0001", "F-0002"}}, // not ALL terminal
		{ID: "N-0004", Title: "sem informs", State: domain.NoteActive},                                                         // no informs
		{ID: "N-0005", Title: "informa decisão superseded", State: domain.NoteActive, Informs: []string{"D-0002"}},             // stale
		{ID: "N-0006", Title: "informa decisão vigente", State: domain.NoteActive, Informs: []string{"D-0001"}},                // live target
		{ID: "N-0007", Title: "já aposentada", State: domain.NoteSuperseded, Informs: []string{"F-0001"}},                      // not active
		{ID: "N-0008", Title: "durável, ainda que informe feature concluída", State: domain.NoteActive, Informs: []string{"F-0001"}, Durable: true}, // durable → skip
	}
	for _, n := range notes {
		n.CreatedAt, n.UpdatedAt = now, now
		if err := store.SaveNote(dir, n); err != nil {
			t.Fatalf("SaveNote: %v", err)
		}
	}

	got := staleNoteFindings(features, decisions, notes)

	// Exactly N-0001 (feature implemented) and N-0005 (decision superseded).
	want := map[string]bool{"N-0001": true, "N-0005": true}
	if len(got) != len(want) {
		t.Fatalf("got %d stale-note findings, want %d: %+v", len(got), len(want), got)
	}
	for _, f := range got {
		if f.Rule != RuleStaleNote {
			t.Errorf("finding rule = %q, want %q (%+v)", f.Rule, RuleStaleNote, f)
		}
		if !want[f.Entity] {
			t.Errorf("unexpected stale-note for %q (%+v)", f.Entity, f)
		}
		delete(want, f.Entity)
	}
	if len(want) != 0 {
		t.Errorf("missing stale-note findings for: %v", want)
	}

	// The detail names the terminal target(s) with their raw state.
	for _, f := range got {
		if f.Entity == "N-0001" && !strings.Contains(f.Detail, "F-0001 implemented") {
			t.Errorf("N-0001 detail should name 'F-0001 implemented': %q", f.Detail)
		}
		if f.Entity == "N-0005" && !strings.Contains(f.Detail, "D-0002 superseded") {
			t.Errorf("N-0005 detail should name 'D-0002 superseded': %q", f.Detail)
		}
	}
}
