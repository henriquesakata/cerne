package app

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// retagKind is one entity kind of the F-0482 table: every state of its FSM must
// accept a retag, and a facet outside the closed set stays refused in each.
type retagKind struct {
	name   string
	states []string
	// seed saves an entity in the given state and returns its id.
	seed func(t *testing.T, repoDir string, n int, state string) string
	// retag runs the kind's Retag function and returns the tags it left.
	retag func(repoDir, projDir, id string, facets []string, setFacets bool) ([]string, error)
}

func retagKinds() []retagKind {
	featureKind := func(name, prefix string) retagKind {
		return retagKind{
			name:   name,
			states: []string{"candidate", "approved", "in_development", "implemented", "done", "rejected", "superseded"},
			seed: func(t *testing.T, repoDir string, n int, state string) string {
				id := fmt.Sprintf("%s-%04d", prefix, n)
				if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: name + " " + state, State: domain.FeatureState(state)}); err != nil {
					t.Fatalf("SaveFeature: %v", err)
				}
				return id
			},
			retag: func(repoDir, projDir, id string, facets []string, set bool) ([]string, error) {
				f, err := RetagFeature(repoDir, projDir, id, facets, nil, set, false)
				return f.Facets, err
			},
		}
	}
	return []retagKind{
		featureKind("feature", "F"), featureKind("task", "T"), featureKind("fix", "B"), featureKind("epic", "E"),
		{
			name: "note", states: []string{"active", "superseded"},
			seed: func(t *testing.T, repoDir string, n int, state string) string {
				id := fmt.Sprintf("N-%04d", n)
				if err := store.SaveNote(repoDir, store.Note{ID: id, Title: "n " + state, State: domain.NoteState(state)}); err != nil {
					t.Fatalf("SaveNote: %v", err)
				}
				return id
			},
			retag: func(repoDir, projDir, id string, facets []string, set bool) ([]string, error) {
				n, err := RetagNote(repoDir, projDir, id, facets, nil, set, false)
				return n.Facets, err
			},
		},
		{
			name: "reference", states: []string{"current", "obsolete"},
			seed: func(t *testing.T, repoDir string, n int, state string) string {
				id := fmt.Sprintf("R-%04d", n)
				if err := store.SaveReference(repoDir, store.Reference{ID: id, Title: "r " + state, State: domain.ReferenceState(state)}); err != nil {
					t.Fatalf("SaveReference: %v", err)
				}
				return id
			},
			retag: func(repoDir, projDir, id string, facets []string, set bool) ([]string, error) {
				r, err := RetagReference(repoDir, projDir, id, facets, nil, set, false)
				return r.Facets, err
			},
		},
		{
			name: "decision", states: []string{"active", "superseded", "retired"},
			seed: func(t *testing.T, repoDir string, n int, state string) string {
				id := fmt.Sprintf("D-%04d", n)
				if err := store.SaveDecision(repoDir, store.Decision{ID: id, Title: "d " + state, State: domain.DecisionState(state)}); err != nil {
					t.Fatalf("SaveDecision: %v", err)
				}
				return id
			},
			retag: func(repoDir, projDir, id string, facets []string, set bool) ([]string, error) {
				d, err := RetagDecision(repoDir, projDir, id, facets, nil, set, false)
				return d.Facets, err
			},
		},
		{
			name: "runbook", states: []string{"active", "superseded"},
			seed: func(t *testing.T, repoDir string, n int, state string) string {
				id := fmt.Sprintf("RB-%04d", n)
				if err := store.SaveRunbook(repoDir, store.Runbook{ID: id, Title: "rb " + state, State: domain.RunbookState(state)}); err != nil {
					t.Fatalf("SaveRunbook: %v", err)
				}
				return id
			},
			retag: func(repoDir, projDir, id string, facets []string, set bool) ([]string, error) {
				rb, err := RetagRunbook(repoDir, projDir, id, facets, nil, set, false)
				return rb.Facets, err
			},
		},
	}
}

// TestRetagAcceptsEveryState (F-0482 criteria 1, 2, 6): each kind retags in
// every state of its FSM, and an unknown facet is refused in every one.
func TestRetagAcceptsEveryState(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n := 0
	for _, k := range retagKinds() {
		for _, st := range k.states {
			n++
			id := k.seed(t, repoDir, n, st)
			commitBackend(t, repoDir)
			t.Run(k.name+"/"+st, func(t *testing.T) {
				got, err := k.retag(repoDir, projDir, id, []string{"code"}, true)
				if err != nil {
					t.Fatalf("retag of a %s %s refused: %v", st, k.name, err)
				}
				if !slices.Equal(got, []string{"code"}) {
					t.Fatalf("facets = %v, want [code]", got)
				}
				if _, err := k.retag(repoDir, projDir, id, []string{"bogus"}, true); !errors.Is(err, domain.ErrUnknownFacet) {
					t.Fatalf("unknown facet err = %v, want ErrUnknownFacet", err)
				}
			})
		}
	}
}
