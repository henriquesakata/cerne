package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCreateNoteWithTags: facets/components are validated and normalized
// (lower/slug/dedup) and persisted on the note.
func TestCreateNoteWithTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n, err := CreateNote(repoDir, projDir, "Gitea install gotcha", "corpo real da nota", nil,
		[]string{"Infra", "INFRA", "code"}, []string{"Gitea", "KPack"}, false, false)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if len(n.Facets) != 2 || n.Facets[0] != "infra" || n.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", n.Facets)
	}
	if len(n.Components) != 2 || n.Components[0] != "gitea" || n.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", n.Components)
	}
	got, err := store.LoadNote(repoDir, n.ID)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if len(got.Facets) != 2 || len(got.Components) != 2 {
		t.Fatalf("persisted tags = %v / %v", got.Facets, got.Components)
	}
}

// TestCreateNoteDurable (F-0249): a note born durable carries the flag from the
// single create commit — the chat's proposals are vocabulary/rejected scope,
// which do not expire with a feature, so lint's stale-note must skip them
// without a second keep round-trip.
func TestCreateNoteDurable(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	n, err := CreateNote(repoDir, projDir, "Vocabulário do domínio", "corpo real da nota", nil,
		[]string{"code"}, []string{"backlog"}, true, false)
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if !n.Durable {
		t.Fatalf("returned note is not durable")
	}
	got, err := store.LoadNote(repoDir, n.ID)
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if !got.Durable {
		t.Fatalf("persisted note is not durable")
	}
}

// TestCreateNoteInvalidFacet: an unknown facet aborts with ErrUnknownFacet and
// writes nothing (validated before any side effect).
func TestCreateNoteInvalidFacet(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := CreateNote(repoDir, projDir, "T", "body", nil, []string{"prod"}, nil, false, false)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownFacet", err)
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("invalid create persisted %d note(s), want 0", len(notes))
	}
}
