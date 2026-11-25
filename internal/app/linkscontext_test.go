package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// TestContextAcceptedLinks: an accepted relation out of the task brings its
// target into ring (a), like a wiki-link; one into the task from a note
// brings the note, like an informs; an open or rejected one brings nothing.
func TestContextAcceptedLinks(t *testing.T) { // F-0502 crit. 6
	repo := contextFixture(t)
	ringOf := func() map[string]string {
		p, err := TaskContext(repo, "T-0002", DefaultContextBudget)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, it := range p.Items {
			m[it.ID] = it.Ring
		}
		return m
	}
	if r := ringOf(); r["D-0003"] != "" || r["N-0007"] != "c" {
		t.Fatalf("before: D-0003 %q, N-0007 %q", r["D-0003"], r["N-0007"])
	}
	queue := []store.LinkSuggestion{
		{N: 1, From: "T-0002", To: "D-0003", Status: store.LinkOpen},
		{N: 2, From: "N-0007", To: "T-0002", Status: store.LinkOpen},
		{N: 3, From: "T-0002", To: "D-0001", Status: store.LinkRejected},
	}
	if err := store.SaveLinkSuggestions(repo, queue); err != nil {
		t.Fatal(err)
	}
	if r := ringOf(); r["D-0003"] != "" || r["N-0007"] != "c" {
		t.Fatalf("open suggestions changed the package: D-0003 %q, N-0007 %q", r["D-0003"], r["N-0007"])
	}
	queue[0].Status, queue[1].Status = store.LinkAccepted, store.LinkAccepted
	if err := store.SaveLinkSuggestions(repo, queue); err != nil {
		t.Fatal(err)
	}
	if r := ringOf(); r["D-0003"] != "a" || r["N-0007"] != "a" {
		t.Fatalf("after accept: D-0003 %q, N-0007 %q, want ring a for both", r["D-0003"], r["N-0007"])
	}
}

// TestBuildGraphAcceptedLink: an accepted suggestion on a done feature is a
// links edge; an open one is not.
func TestBuildGraphAcceptedLink(t *testing.T) { // F-0502 crit. 6
	repo := linksRepo(t) // F-0001 is done
	has := func() bool {
		g, err := BuildGraph(repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range g.Edges {
			if e == (Edge{From: "F-0001", To: "D-0001", Type: EdgeLinks}) {
				return true
			}
		}
		return false
	}
	q := []store.LinkSuggestion{{N: 2, From: "F-0001", To: "D-0001", Status: store.LinkOpen}}
	if err := store.SaveLinkSuggestions(repo, q); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Fatal("an open suggestion is an edge")
	}
	q[0].Status = store.LinkAccepted
	if err := store.SaveLinkSuggestions(repo, q); err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Fatal("an accepted suggestion is not an edge")
	}
}

// TestBuildGraphSurvivesACorruptLinksQueue: an unreadable queue file leaves
// the graph without the accepted edges and without an error, like the context.
func TestBuildGraphSurvivesACorruptLinksQueue(t *testing.T) { // F-0502 crit. 6
	repo := linksRepo(t)
	writeDoc(t, repo, "links/suggestions.json", "<<<<<<< HEAD\n[{\"n\": 1\n")
	g, err := BuildGraph(repo)
	if err != nil || len(g.Nodes) == 0 {
		t.Fatalf("BuildGraph = %d nodes, %v; want it to go on", len(g.Nodes), err)
	}
	if _, err := TaskContext(repo, "F-0001", DefaultContextBudget); err != nil {
		t.Errorf("TaskContext: %v", err)
	}
}

// TestContextAcceptedLinksBySourceKind: an accepted relation that reaches the
// task brings its source into ring (a) when it is a decision or a reference,
// like an informs; a feature as source does not.
func TestContextAcceptedLinksBySourceKind(t *testing.T) { // F-0502 crit. 6
	repo := contextFixture(t)
	err := store.SaveLinkSuggestions(repo, []store.LinkSuggestion{
		{N: 1, From: "D-0003", To: "T-0002", Status: store.LinkAccepted},
		{N: 2, From: "R-0004", To: "T-0002", Status: store.LinkAccepted},
		{N: 3, From: "F-0020", To: "T-0002", Status: store.LinkAccepted},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := TaskContext(repo, "T-0002", DefaultContextBudget)
	if err != nil {
		t.Fatal(err)
	}
	ring := map[string]string{}
	for _, it := range p.Items {
		ring[it.ID] = it.Ring
	}
	if ring["D-0003"] != "a" || ring["R-0004"] != "a" {
		t.Errorf("decision %q and reference %q as sources, want ring a for both", ring["D-0003"], ring["R-0004"])
	}
	if r, in := ring["F-0020"]; in {
		t.Errorf("a feature as the source came in ring %q", r)
	}
}
