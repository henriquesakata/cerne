package store

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
)

func TestChildrenByParentAndProgress(t *testing.T) {
	features := []Feature{
		{ID: "F-0001", State: domain.StateInDevelopment},                // epic
		{ID: "F-0002", Parent: "F-0001", State: domain.StateImplemented},
		{ID: "F-0003", Parent: "F-0001", State: domain.StateCandidate},
		{ID: "F-0004", State: domain.StateApproved}, // top-level, no children
	}

	byParent := ChildrenByParent(features)
	kids := byParent["F-0001"]
	if len(kids) != 2 || kids[0].ID != "F-0002" || kids[1].ID != "F-0003" {
		t.Fatalf("children of F-0001 = %+v, want [F-0002, F-0003] sorted", kids)
	}
	if _, ok := byParent["F-0004"]; ok {
		t.Fatalf("F-0004 has no children, must be absent from the map")
	}

	done, total := EpicProgress(kids)
	if done != 1 || total != 2 {
		t.Fatalf("EpicProgress = %d/%d, want 1/2", done, total)
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
