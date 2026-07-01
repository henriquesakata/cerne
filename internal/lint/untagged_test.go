package lint

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestUntaggedLiveFindings pins the untagged-live rule: a live entity (active
// note/decision, current reference, actively-worked feature) carrying no tag on
// EITHER axis (facets and components both empty) is flagged; a tag on either axis
// silences it, and a superseded/obsolete entity is history and is never flagged.
// Features gained the axes in D-0003: candidate/approved/in_development are
// flagged, but implemented (done backlog) is deliberately spared.
func TestUntaggedLiveFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", Title: "candidate sem tag", State: domain.StateCandidate},                             // flagged
		{ID: "F-0002", Title: "approved com facet", State: domain.StateApproved, Facets: []string{"infra"}}, // tagged → skip
		{ID: "F-0003", Title: "in_dev sem tag", State: domain.StateInDevelopment},                            // flagged
		{ID: "F-0004", Title: "implemented sem tag", State: domain.StateImplemented},                         // done backlog → skip
		{ID: "F-0005", Title: "superseded sem tag", State: domain.StateSuperseded},                           // history → skip
		{ID: "F-0006", Title: "rejected sem tag", State: domain.StateRejected},                               // history → skip
	}
	notes := []store.Note{
		{ID: "N-0001", Title: "sem tag", State: domain.NoteActive},                                        // flagged
		{ID: "N-0002", Title: "só facet", State: domain.NoteActive, Facets: []string{"infra"}},            // tagged → skip
		{ID: "N-0003", Title: "só componente", State: domain.NoteActive, Components: []string{"gitea"}},   // tagged → skip
		{ID: "N-0004", Title: "superseded sem tag", State: domain.NoteSuperseded},                         // history → skip
	}
	references := []store.Reference{
		{ID: "R-0001", Title: "current sem tag", State: domain.ReferenceCurrent},                              // flagged
		{ID: "R-0002", Title: "current com facet", State: domain.ReferenceCurrent, Facets: []string{"code"}}, // tagged → skip
		{ID: "R-0003", Title: "obsolete sem tag", State: domain.ReferenceObsolete},                            // history → skip
	}
	decisions := []store.Decision{
		{ID: "D-0001", Title: "active sem tag", State: domain.DecisionActive}, // flagged
		{ID: "D-0002", Title: "active com ambos", State: domain.DecisionActive,
			Facets: []string{"auth"}, Components: []string{"keycloak"}}, // tagged → skip
		{ID: "D-0003", Title: "superseded sem tag", State: domain.DecisionSuperseded}, // history → skip
	}

	got := untaggedLiveFindings(features, decisions, references, notes)

	want := map[string]bool{"F-0001": true, "F-0003": true, "N-0001": true, "R-0001": true, "D-0001": true}
	if len(got) != len(want) {
		t.Fatalf("got %d untagged-live findings, want %d: %+v", len(got), len(want), got)
	}
	for _, f := range got {
		if f.Rule != RuleUntaggedLive {
			t.Errorf("finding rule = %q, want %q (%+v)", f.Rule, RuleUntaggedLive, f)
		}
		if !want[f.Entity] {
			t.Errorf("unexpected untagged-live for %q (%+v)", f.Entity, f)
		}
		delete(want, f.Entity)
		if !strings.Contains(f.Detail, "tag") {
			t.Errorf("detail should mention tags: %q", f.Detail)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing untagged-live findings for: %v", want)
	}
}

// TestRunFlagsUntaggedLive proves the rule is wired into Run: a single active,
// untagged note surfaces exactly one untagged-live finding for it.
func TestRunFlagsUntaggedLive(t *testing.T) {
	dir := t.TempDir()
	n := store.Note{ID: "N-0001", Title: "sem tag", State: domain.NoteActive}
	if err := store.SaveNote(dir, n); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}

	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var untagged []Finding
	for _, f := range got {
		if f.Rule == RuleUntaggedLive {
			untagged = append(untagged, f)
		}
	}
	if len(untagged) != 1 || untagged[0].Entity != "N-0001" {
		t.Fatalf("untagged-live findings = %+v, want exactly one for N-0001", untagged)
	}
}
