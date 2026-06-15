package app

import (
	"sort"

	"gitlab.com/rockt/cerne/internal/store"
)

// Node kinds and edge types of the entity graph.
const (
	KindFeature    = "feature"
	KindDecision   = "decision"
	KindReference  = "reference"
	KindHistorical = "historical"

	// EdgeSupersededBy points from a terminal entity to the one that replaced
	// it; its Reason is the mandatory supersession motive (the explanatory
	// payload). EdgeInforms points from a reference to the feature/decision it
	// motivated. EdgeLinks points from one entity's content doc to another
	// entity it cites with a resolvable markdown link.
	EdgeSupersededBy = "superseded_by"
	EdgeInforms      = "informs"
	EdgeLinks        = "links"
)

// Node is one entity in the graph projection.
type Node struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	State string `json:"state"`
}

// Edge is a directed relationship From → To of a given Type. Reason is set
// only for superseded_by edges (the persisted supersession motive); empty
// otherwise.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
}

// Graph is the deterministic projection of the backend's current+terminal
// entities as nodes and their structured relationships as edges.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// BuildGraph loads every entity in the backend at repoDir and projects the
// latent relationship graph: a node per entity (kind + lifecycle state) and an
// edge per structured relationship. Edges whose target is not a known node are
// dropped so the graph never dangles (e.g. a historical entity absorbed by a
// free label rather than a cerne entity). Output is deterministic: nodes
// sorted by ID, edges by (From, To, Type). No LLM, no network — lifecycle is
// data.
func BuildGraph(repoDir string) (Graph, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return Graph{}, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return Graph{}, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return Graph{}, err
	}
	historicals, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return Graph{}, err
	}

	var nodes []Node
	nodeSet := map[string]bool{}
	add := func(id, kind, title, state string) {
		nodes = append(nodes, Node{ID: id, Kind: kind, Title: title, State: state})
		nodeSet[id] = true
	}
	for _, f := range features {
		add(f.ID, KindFeature, f.Title, string(f.State))
	}
	for _, d := range decisions {
		add(d.ID, KindDecision, d.Title, string(d.State))
	}
	for _, r := range references {
		add(r.ID, KindReference, r.Title, string(r.State))
	}
	for _, h := range historicals {
		add(h.ID, KindHistorical, h.Name, string(h.State))
	}

	var edges []Edge
	seen := map[string]bool{}
	addEdge := func(from, to, typ, reason string) {
		if from == to || !nodeSet[from] || !nodeSet[to] {
			return
		}
		key := from + "\x00" + to + "\x00" + typ
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, Edge{From: from, To: to, Type: typ, Reason: reason})
	}
	for _, f := range features {
		if f.SupersededBy != "" {
			addEdge(f.ID, f.SupersededBy, EdgeSupersededBy, f.Reason)
		}
	}
	for _, d := range decisions {
		if d.SupersededBy != "" {
			addEdge(d.ID, d.SupersededBy, EdgeSupersededBy, d.Reason)
		}
	}
	for _, h := range historicals {
		if h.SupersededBy != "" {
			addEdge(h.ID, h.SupersededBy, EdgeSupersededBy, h.Reason)
		}
	}
	for _, r := range references {
		for _, to := range r.Informs {
			addEdge(r.ID, to, EdgeInforms, "")
		}
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Type < edges[j].Type
	})
	return Graph{Nodes: nodes, Edges: edges}, nil
}
