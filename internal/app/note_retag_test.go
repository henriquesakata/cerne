package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// mkTaggedNote: an active note with the given tags (helper for retag tests).
func mkTaggedNote(t *testing.T, repoDir, projDir string, facets, components []string) store.Note {
	t.Helper()
	n, err := CreateNote(repoDir, projDir, "Nota", "corpo", nil, facets, components, false, true) // fixture helper: several same-titled notes on purpose
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

// TestRetagNoteReplacesAxes: both axes set → replaced with normalized values.
func TestRetagNoteReplacesAxes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n := mkTaggedNote(t, repoDir, projDir, []string{"code"}, []string{"old"})
	got, err := RetagNote(repoDir, projDir, n.ID, []string{"Infra"}, []string{"Gitea"}, true, true)
	if err != nil {
		t.Fatalf("RetagNote: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "infra" || len(got.Components) != 1 || got.Components[0] != "gitea" {
		t.Fatalf("retag = %v / %v, want [infra] / [gitea]", got.Facets, got.Components)
	}
	reload, _ := store.LoadNote(repoDir, n.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "infra" {
		t.Fatalf("persisted facets = %v", reload.Facets)
	}
}

// TestRetagNotePartialKeepsOtherAxis: setting only components preserves facets.
func TestRetagNotePartialKeepsOtherAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n := mkTaggedNote(t, repoDir, projDir, []string{"infra"}, []string{"gitea"})
	got, err := RetagNote(repoDir, projDir, n.ID, nil, []string{"kpack"}, false, true)
	if err != nil {
		t.Fatalf("RetagNote: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "infra" {
		t.Fatalf("facets changed = %v, want preserved [infra]", got.Facets)
	}
	if len(got.Components) != 1 || got.Components[0] != "kpack" {
		t.Fatalf("components = %v, want [kpack]", got.Components)
	}
}

// TestRetagNoteClearAxis: set=true with an empty slice clears that axis.
func TestRetagNoteClearAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n := mkTaggedNote(t, repoDir, projDir, []string{"infra"}, []string{"gitea"})
	got, err := RetagNote(repoDir, projDir, n.ID, nil, nil, true, false)
	if err != nil {
		t.Fatalf("RetagNote: %v", err)
	}
	if len(got.Facets) != 0 {
		t.Fatalf("facets = %v, want cleared", got.Facets)
	}
	if len(got.Components) != 1 {
		t.Fatalf("components changed = %v, want preserved", got.Components)
	}
}

// TestRetagNoteInvalidFacetNoOp: an unknown facet errors and leaves the note as-is.
func TestRetagNoteInvalidFacetNoOp(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n := mkTaggedNote(t, repoDir, projDir, []string{"infra"}, []string{"gitea"})
	_, err := RetagNote(repoDir, projDir, n.ID, []string{"prod"}, nil, true, false)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}
	reload, _ := store.LoadNote(repoDir, n.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "infra" {
		t.Fatalf("note mutated on invalid retag: facets = %v", reload.Facets)
	}
}

// TestRetagNotePreservesDurableInforms: retag touches only tags, not Durable/Informs.
func TestRetagNotePreservesDurableInforms(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	n, err := CreateNote(repoDir, projDir, "Nota", "corpo", []string{"F-0001"}, nil, nil, false, false)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := KeepNote(repoDir, projDir, n.ID); err != nil {
		t.Fatalf("KeepNote: %v", err)
	}
	got, err := RetagNote(repoDir, projDir, n.ID, []string{"process"}, nil, true, false)
	if err != nil {
		t.Fatalf("RetagNote: %v", err)
	}
	if !got.Durable {
		t.Fatalf("retag dropped Durable")
	}
	if len(got.Informs) != 1 || got.Informs[0] != "F-0001" {
		t.Fatalf("retag changed Informs = %v", got.Informs)
	}
}

// TestRetagNoteNotFoundOrSuperseded: missing id and superseded note both error.
func TestRetagNoteNotFoundOrSuperseded(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := RetagNote(repoDir, projDir, "N-9999", []string{"infra"}, nil, true, false); err == nil {
		t.Fatalf("retag of missing note should error")
	}
	n := mkTaggedNote(t, repoDir, projDir, nil, nil)
	n2 := mkTaggedNote(t, repoDir, projDir, nil, nil)
	if _, err := TransitionNote(repoDir, projDir, n.ID, n2.ID, "obsoleta"); err != nil {
		t.Fatalf("TransitionNote: %v", err)
	}
	if _, err := RetagNote(repoDir, projDir, n.ID, []string{"infra"}, nil, true, false); err == nil {
		t.Fatalf("retag of superseded note should error")
	}
}
