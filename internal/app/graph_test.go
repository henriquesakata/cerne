package app

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestBuildGraphStructuredEdges: BuildGraph projects every entity as a node
// (kind + state) and the structured relationships as edges — superseded_by
// (carrying the entity's mandatory reason) and informs (reference → F/D).
// Edges to unknown targets (a historical absorbed by a free label) are dropped
// so the graph never dangles; output is deterministic (nodes by ID, edges by
// from/to/type).
func TestBuildGraphStructuredEdges(t *testing.T) {
	repo := t.TempDir()
	now := time.Now()
	save := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// F-0001 (Bills) superseded by F-0002 (Finance), with a reason.
	save(store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "Bills", Module: "finance", State: domain.StateSuperseded, SupersededBy: "F-0002", Reason: "absorvida pelo Finance", CreatedAt: now, UpdatedAt: now}))
	save(store.SaveFeature(repo, store.Feature{ID: "F-0002", Title: "Finance", Module: "finance", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}))
	// D-0001 superseded by D-0002.
	save(store.SaveDecision(repo, store.Decision{ID: "D-0001", Title: "ADR antiga", State: domain.DecisionSuperseded, SupersededBy: "D-0002", Reason: "revista", CreatedAt: now, UpdatedAt: now}))
	save(store.SaveDecision(repo, store.Decision{ID: "D-0002", Title: "ADR nova", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}))
	// R-0001 informs F-0002 and D-0002.
	save(store.SaveReference(repo, store.Reference{ID: "R-0001", Title: "Artigo", State: domain.ReferenceCurrent, External: true, URL: "http://x", Informs: []string{"F-0002", "D-0002"}, CreatedAt: now, UpdatedAt: now}))
	// H-0001 absorbed by an existing feature (edge); H-0002 absorbed by a free
	// label with no node (edge dropped).
	save(store.SaveHistorical(repo, store.Historical{ID: "H-0001", Name: "Investments", SupersededBy: "F-0002", Reason: "absorvida", State: domain.StateSuperseded, CreatedAt: now}))
	save(store.SaveHistorical(repo, store.Historical{ID: "H-0002", Name: "Legado", SupersededBy: "Externo", Reason: "descontinuado", State: domain.StateSuperseded, CreatedAt: now}))

	g, err := BuildGraph(repo)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	if len(g.Nodes) != 7 {
		t.Fatalf("nodes = %d, want 7: %+v", len(g.Nodes), g.Nodes)
	}
	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	if n := byID["F-0001"]; n.Kind != KindFeature || n.State != "superseded" || n.Title != "Bills" {
		t.Fatalf("F-0001 node = %+v", n)
	}
	if n := byID["R-0001"]; n.Kind != KindReference {
		t.Fatalf("R-0001 kind = %q, want reference", n.Kind)
	}
	if n := byID["H-0001"]; n.Kind != KindHistorical {
		t.Fatalf("H-0001 kind = %q, want historical", n.Kind)
	}

	want := []Edge{
		{From: "D-0001", To: "D-0002", Type: EdgeSupersededBy, Reason: "revista"},
		{From: "F-0001", To: "F-0002", Type: EdgeSupersededBy, Reason: "absorvida pelo Finance"},
		{From: "H-0001", To: "F-0002", Type: EdgeSupersededBy, Reason: "absorvida"},
		{From: "R-0001", To: "D-0002", Type: EdgeInforms},
		{From: "R-0001", To: "F-0002", Type: EdgeInforms},
	}
	if len(g.Edges) != len(want) {
		t.Fatalf("edges = %d, want %d: %+v", len(g.Edges), len(want), g.Edges)
	}
	for i := range want {
		if g.Edges[i] != want[i] {
			t.Fatalf("edge[%d] = %+v, want %+v", i, g.Edges[i], want[i])
		}
	}
}
