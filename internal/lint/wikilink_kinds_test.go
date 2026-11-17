package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestWikiLinkResolvesEveryEntityKind (B-0022): [[W-…]], [[C-…]] and [[HO-…]]
// to an existing entity are no broken-link (the id set is the one
// store.EntityExists knows); a missing id of the same kind still is.
func TestWikiLinkResolvesEveryEntityKind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Dono", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWork(dir, store.Work{ID: "W-0007", Title: "Investigar", State: domain.WorkActive, Actor: "a", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCapability(dir, store.Capability{ID: "C-0003", Title: "Cap", State: domain.CapabilityActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveHandoff(dir, store.Handoff{ID: "HO-0005", State: domain.HandoffPending, FromWork: "W-0007", Actor: "a", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Dono\n\nVeja [[W-0007]], [[C-0003]] e [[HO-0005]]; o alias [[W-0007|a investigação]] e [[W-0998|alias fantasma]]; e os que não existem [[W-0999]], [[C-0999]], [[HO-0999]].\n")
	got, err := RunConfig(dir, 14, 8, 0)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	broken := map[string]bool{}
	for _, f := range got {
		if f.Rule == RuleBrokenLink {
			for _, id := range []string{"W-0007", "C-0003", "HO-0005", "W-0999", "C-0999", "HO-0999", "W-0998"} {
				if strings.Contains(f.Detail, "[["+id+"]]") {
					broken[id] = true
				}
			}
		}
	}
	for _, id := range []string{"W-0007", "C-0003", "HO-0005"} {
		if broken[id] {
			t.Errorf("[[%s]] exists but is a broken-link", id)
		}
	}
	for _, id := range []string{"W-0999", "C-0999", "HO-0999", "W-0998"} {
		if !broken[id] {
			t.Errorf("[[%s]] does not exist and must stay a broken-link: %+v", id, got)
		}
	}
}
