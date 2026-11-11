package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// mkTaggedRunbook: an active runbook seeded with the given tags (helper for retag
// tests). CreateRunbook is bodyless-and-untagged, so we set the initial tags via a
// first RetagRunbook — the same code path a user takes.
func mkTaggedRunbook(t *testing.T, repoDir, projDir string, facets, components []string) store.Runbook {
	t.Helper()
	rb, err := CreateRunbook(repoDir, projDir, "Deploy", "# corpo")
	if err != nil {
		t.Fatalf("CreateRunbook: %v", err)
	}
	if facets == nil && components == nil {
		return rb
	}
	rb, err = RetagRunbook(repoDir, projDir, rb.ID, facets, components, facets != nil, components != nil)
	if err != nil {
		t.Fatalf("seed RetagRunbook: %v", err)
	}
	return rb
}

// TestRetagRunbookReplacesAxes: both axes set → replaced with normalized values.
func TestRetagRunbookReplacesAxes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb := mkTaggedRunbook(t, repoDir, projDir, []string{"code"}, []string{"old"})
	got, err := RetagRunbook(repoDir, projDir, rb.ID, []string{"Infra"}, []string{"Gitback"}, true, true)
	if err != nil {
		t.Fatalf("RetagRunbook: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "infra" || len(got.Components) != 1 || got.Components[0] != "gitback" {
		t.Fatalf("retag = %v / %v, want [infra] / [gitback]", got.Facets, got.Components)
	}
	reload, _ := store.LoadRunbook(repoDir, rb.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "infra" {
		t.Fatalf("persisted facets = %v", reload.Facets)
	}
}

// TestRetagRunbookPartialKeepsOtherAxis: setting only components preserves facets.
func TestRetagRunbookPartialKeepsOtherAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb := mkTaggedRunbook(t, repoDir, projDir, []string{"infra"}, []string{"gitback"})
	got, err := RetagRunbook(repoDir, projDir, rb.ID, nil, []string{"materialize"}, false, true)
	if err != nil {
		t.Fatalf("RetagRunbook: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "infra" {
		t.Fatalf("facets changed = %v, want preserved [infra]", got.Facets)
	}
	if len(got.Components) != 1 || got.Components[0] != "materialize" {
		t.Fatalf("components = %v, want [materialize]", got.Components)
	}
}

// TestRetagRunbookClearAxis: set=true with an empty slice clears that axis.
func TestRetagRunbookClearAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb := mkTaggedRunbook(t, repoDir, projDir, []string{"infra"}, []string{"gitback"})
	got, err := RetagRunbook(repoDir, projDir, rb.ID, nil, nil, true, false)
	if err != nil {
		t.Fatalf("RetagRunbook: %v", err)
	}
	if len(got.Facets) != 0 {
		t.Fatalf("facets = %v, want cleared", got.Facets)
	}
	if len(got.Components) != 1 {
		t.Fatalf("components changed = %v, want preserved", got.Components)
	}
}

// TestRetagRunbookInvalidFacetNoOp: an unknown facet errors and leaves the runbook as-is.
func TestRetagRunbookInvalidFacetNoOp(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb := mkTaggedRunbook(t, repoDir, projDir, []string{"infra"}, nil)
	_, err := RetagRunbook(repoDir, projDir, rb.ID, []string{"prod"}, nil, true, false)
	if err == nil {
		t.Fatalf("want error for unknown facet 'prod'")
	}
	reload, _ := store.LoadRunbook(repoDir, rb.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "infra" {
		t.Fatalf("facets mutated on invalid retag = %v, want untouched [infra]", reload.Facets)
	}
}

// TestRetagRunbookPreservesTitleState: retag touches only tags, not title/state/body.
func TestRetagRunbookPreservesTitleState(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	rb := mkTaggedRunbook(t, repoDir, projDir, nil, nil)
	got, err := RetagRunbook(repoDir, projDir, rb.ID, []string{"process"}, nil, true, false)
	if err != nil {
		t.Fatalf("RetagRunbook: %v", err)
	}
	if got.Title != "Deploy" || got.State != domain.RunbookActive {
		t.Fatalf("retag mutated title/state: %q / %s", got.Title, got.State)
	}
}

// TestRetagRunbookNotFoundOrSuperseded: missing id and superseded runbook both error.
func TestRetagRunbookNotFoundOrSuperseded(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := RetagRunbook(repoDir, projDir, "RB-9999", []string{"infra"}, nil, true, false); err == nil {
		t.Fatalf("want error for missing runbook")
	}
	rb := mkTaggedRunbook(t, repoDir, projDir, nil, nil)
	by, err := CreateRunbook(repoDir, projDir, "Novo deploy", "# corpo")
	if err != nil {
		t.Fatalf("CreateRunbook by: %v", err)
	}
	if _, err := TransitionRunbook(repoDir, projDir, rb.ID, by.ID, "obsoleto"); err != nil {
		t.Fatalf("TransitionRunbook: %v", err)
	}
	if _, err := RetagRunbook(repoDir, projDir, rb.ID, []string{"infra"}, nil, true, false); err == nil {
		t.Fatalf("want error retagging a superseded runbook")
	}
}
