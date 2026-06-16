package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeDoc writes content at the backend-relative slash path rel under repo.
func writeDoc(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

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

// TestBuildGraphPartOfEdges: a feature with a Parent yields a part_of edge
// (child → epic); an edge to a missing parent is dropped so the graph never
// dangles.
func TestBuildGraphPartOfEdges(t *testing.T) {
	repo := t.TempDir()
	now := time.Now()
	save := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	save(store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "Épico", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}))
	save(store.SaveFeature(repo, store.Feature{ID: "F-0002", Title: "Filha A", Parent: "F-0001", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}))
	save(store.SaveFeature(repo, store.Feature{ID: "F-0003", Title: "Filha B", Parent: "F-0001", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}))
	save(store.SaveFeature(repo, store.Feature{ID: "F-0004", Title: "Órfã", Parent: "F-9999", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}))

	g, err := BuildGraph(repo)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	var parts []Edge
	for _, e := range g.Edges {
		if e.Type == EdgePartOf {
			parts = append(parts, e)
		}
	}
	want := []Edge{
		{From: "F-0002", To: "F-0001", Type: EdgePartOf},
		{From: "F-0003", To: "F-0001", Type: EdgePartOf},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("part_of edges = %+v, want %+v (missing-parent edge dropped)", parts, want)
	}
}

// TestBuildGraphContentLinkEdges: a markdown citation between content docs that
// resolves to a known entity becomes a links edge; a self link, a non-entity
// link and an external link produce none.
func TestBuildGraphContentLinkEdges(t *testing.T) {
	repo := t.TempDir()
	now := time.Now()
	save := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	save(store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "A", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}))
	save(store.SaveFeature(repo, store.Feature{ID: "F-0002", Title: "B", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}))
	save(store.SaveDecision(repo, store.Decision{ID: "D-0001", Title: "Dec", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}))

	writeDoc(t, repo, "content/features/F-0001/spec.md",
		"[dec](../../decisions/D-0001.md) [other](../F-0002/spec.md) [self](plan.md) [ext](https://x)")

	g, err := BuildGraph(repo)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	var links []Edge
	for _, e := range g.Edges {
		if e.Type == EdgeLinks {
			links = append(links, e)
		}
	}
	want := []Edge{
		{From: "F-0001", To: "D-0001", Type: EdgeLinks},
		{From: "F-0001", To: "F-0002", Type: EdgeLinks},
	}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("link edges = %+v, want %+v", links, want)
	}
}
