package store

import (
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// LoadMeta labels an entity of every kind, even one with no title (the task
// package lists them by it, T-0017 critic L1): a historical by its name, an
// untitled idea by the first line of its body, a handoff by its next step,
// and anything else "(untitled)".
func TestLoadMetaLabelsEveryKind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	for _, err := range []error{
		SaveIdea(dir, Idea{ID: "I-0001", Body: "  First line of the idea\nthe rest", State: domain.IdeaActive, CreatedAt: now}),
		SaveIdea(dir, Idea{ID: "I-0002", State: domain.IdeaActive, CreatedAt: now}),
		SavePending(dir, Pending{ID: "P-0001", Title: "Pay the debt", State: domain.PendingOpen, CreatedAt: now, UpdatedAt: now}),
		SaveHistorical(dir, Historical{ID: "H-0001", Name: "Old billing", State: domain.StateSuperseded}),
		SaveWork(dir, Work{ID: "W-0001", Title: "Review the MR", State: domain.WorkActive}),
		SaveHandoff(dir, Handoff{ID: "HO-0001", FromWork: "W-0001", Next: "run the suite", State: domain.HandoffPending}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ id, kind, title, state string }{
		{"I-0001", "idea", "First line of the idea", "active"},
		{"I-0002", "idea", "(untitled)", "active"},
		{"P-0001", "pending", "Pay the debt", "open"},
		{"H-0001", "historical", "Old billing", "superseded"},
		{"W-0001", "work", "Review the MR", "active"},
		{"HO-0001", "handoff", "next: run the suite", "pending"},
	} {
		m, kind, err := LoadMeta(dir, tc.id)
		if err != nil || kind != tc.kind || m.Title != tc.title || m.State != tc.state {
			t.Errorf("LoadMeta(%s) = %+v %q %v, want %q %q %q", tc.id, m, kind, err, tc.kind, tc.title, tc.state)
		}
	}
}
