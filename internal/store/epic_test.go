package store

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

func TestChildrenByParentAndProgress(t *testing.T) {
	features := []Feature{
		{ID: "F-0001", State: domain.StateInDevelopment},                // epic
		{ID: "F-0002", Parent: "F-0001", State: domain.StateImplemented},
		{ID: "F-0003", Parent: "F-0001", State: domain.StateCandidate},
		{ID: "F-0005", Parent: "F-0001", State: domain.StateDone},
		{ID: "F-0004", State: domain.StateApproved}, // top-level, no children
	}

	byParent := ChildrenByParent(features)
	kids := byParent["F-0001"]
	if len(kids) != 3 || kids[0].ID != "F-0002" || kids[1].ID != "F-0003" || kids[2].ID != "F-0005" {
		t.Fatalf("children of F-0001 = %+v, want [F-0002, F-0003, F-0005] sorted", kids)
	}
	if _, ok := byParent["F-0004"]; ok {
		t.Fatalf("F-0004 has no children, must be absent from the map")
	}

	// Implemented and done (F-0131 acceptance) both count as concluded.
	done, total := EpicProgress(kids)
	if done != 2 || total != 3 {
		t.Fatalf("EpicProgress = %d/%d, want 2/3", done, total)
	}
	if done, total := EpicProgress(nil); done != 0 || total != 0 {
		t.Fatalf("EpicProgress(nil) = %d/%d, want 0/0", done, total)
	}
}

// TestEpicProgressCountsDone (F-0131): done is the human-accepted successor
// of implemented, so it counts as concluded in the epic n/m rollup too.
func TestEpicProgressCountsDone(t *testing.T) {
	children := []Feature{
		{ID: "F-0002", State: domain.StateDone},
		{ID: "F-0003", State: domain.StateImplemented},
		{ID: "F-0004", State: domain.StateCandidate},
	}
	done, total := EpicProgress(children)
	if done != 2 || total != 3 {
		t.Fatalf("EpicProgress = %d/%d, want 2/3 (done + implemented both count)", done, total)
	}
}

// TestChildrenByParentCanonicalKey: a parent stored in another case or with
// surrounding spaces (legacy data) groups under the canonical epic id, and a
// blank parent is no parent at all.
func TestChildrenByParentCanonicalKey(t *testing.T) {
	features := []Feature{
		{ID: "F-0001"},
		{ID: "F-0002", Parent: "f-0001"},
		{ID: "F-0003", Parent: " F-0001 "},
		{ID: "F-0004", Parent: "F-0001"},
		{ID: "F-0005", Parent: "  "},
	}
	byParent := ChildrenByParent(features)
	kids := byParent["F-0001"]
	if len(kids) != 3 || kids[0].ID != "F-0002" || kids[1].ID != "F-0003" || kids[2].ID != "F-0004" {
		t.Fatalf("children of F-0001 = %+v, want [F-0002 F-0003 F-0004]", kids)
	}
	if len(byParent) != 1 {
		t.Fatalf("byParent keys = %d (%v), want only F-0001", len(byParent), byParent)
	}
}

// TestLoadFeatureCanonicalizesParent (F-0326): the store is the single point
// every reader routes through, so a parent written in another case or padded
// (legacy data) comes back canonical from both loaders.
func TestLoadFeatureCanonicalizesParent(t *testing.T) {
	repo := t.TempDir()
	if err := SaveFeature(repo, Feature{ID: "F-0002", Parent: " f-0001 "}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if f, err := LoadFeature(repo, "F-0002"); err != nil || f.Parent != "F-0001" {
		t.Fatalf("LoadFeature parent = %q, %v; want F-0001", f.Parent, err)
	}
	all, err := LoadAllFeatures(repo)
	if err != nil || len(all) != 1 || all[0].Parent != "F-0001" {
		t.Fatalf("LoadAllFeatures = %+v, %v; want one feature with parent F-0001", all, err)
	}
}
