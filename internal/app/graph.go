package app

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/mdlink"
	"gitlab.com/rockt/cerne/internal/store"
)

// Node kinds and edge types of the entity graph.
const (
	KindFeature    = "feature"
	KindDecision   = "decision"
	KindReference  = "reference"
	KindHistorical = "historical"
	KindNote       = "note"
	KindRunbook    = "runbook"

	// EdgeSupersededBy points from a terminal entity to the one that replaced
	// it; its Reason is the mandatory supersession motive (the explanatory
	// payload). EdgeInforms points from a reference to the feature/decision it
	// motivated. EdgeLinks points from one entity's content doc to another
	// entity it cites with a resolvable markdown link. EdgePartOf points from a
	// child feature to the epic (parent feature) it decomposes.
	EdgeSupersededBy = "superseded_by"
	EdgeInforms      = "informs"
	EdgeLinks        = "links"
	EdgePartOf       = "part_of"
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
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return Graph{}, err
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return Graph{}, err
	}

	nodes := []Node{}
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
	// F-0167: notes and runbooks are nodes too — the most common wiki-link
	// target is a note, and backlinks need the node to point at.
	for _, n := range notes {
		add(n.ID, KindNote, n.Title, string(n.State))
	}
	for _, rb := range runbooks {
		add(rb.ID, KindRunbook, rb.Title, string(rb.State))
	}

	edges := []Edge{}
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
	for _, f := range features {
		if f.Parent != "" {
			addEdge(f.ID, f.Parent, EdgePartOf, "")
		}
	}

	// links — markdown citations between content docs that resolve to a known
	// entity. Reuses the lint content-link primitive with a path→entity-ID
	// resolver; unresolved targets are the broken-link rule's concern, not edges.
	docs, err := contentDocs(repoDir, features, decisions, references, notes, runbooks)
	if err != nil {
		return Graph{}, err
	}
	for _, cd := range docs {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(cd.rel)))
		if err != nil {
			return Graph{}, err
		}
		docDir := path.Dir(cd.rel)
		resolved, _ := mdlink.ContentLinks(string(data), func(clean string) (string, bool) {
			id := entityIDFromContentPath(path.Join(docDir, clean))
			if id != "" && nodeSet[id] {
				return id, true
			}
			return "", false
		})
		for _, to := range resolved {
			addEdge(cd.id, to, EdgeLinks, "")
		}
		// F-0167: id-form wiki-links are citations too — same edge type, same
		// dedupe; resolution goes through the shared canonical-id helper, so
		// lint and graph canonicalize ids the same way. The id sets differ on
		// purpose: lint also resolves P- (pendings, F-0322) and I- (ideas,
		// F-0312), which are not graph nodes (the graph is a UI surface), so a
		// [[P-…]] or [[I-…]] is a valid link with no edge here.
		wikiResolved, _ := mdlink.WikiLinks(string(data), func(target string) (string, bool) {
			id := mdlink.CanonicalID(target)
			return id, nodeSet[id]
		})
		for _, to := range wikiResolved {
			addEdge(cd.id, to, EdgeLinks, "")
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

// contentDoc is one entity content document scanned for link edges.
type contentDoc struct{ id, rel string }

// contentDocs lists the content documents of every entity that has them: each
// *.md under content/features/<ID>/, plus content/decisions/<ID>.md,
// content/references/<ID>.md, content/notes/<ID>.md and
// content/runbooks/<ID>.md when present.
func contentDocs(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, notes []store.Note, runbooks []store.Runbook) ([]contentDoc, error) {
	var docs []contentDoc
	for _, f := range features {
		entries, err := os.ReadDir(filepath.Join(repoDir, "content", "features", f.ID))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			docs = append(docs, contentDoc{f.ID, "content/features/" + f.ID + "/" + e.Name()})
		}
	}
	addFile := func(id, rel string) error {
		_, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		docs = append(docs, contentDoc{id, rel})
		return nil
	}
	for _, d := range decisions {
		if err := addFile(d.ID, "content/decisions/"+d.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, r := range references {
		if err := addFile(r.ID, "content/references/"+r.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, n := range notes {
		if err := addFile(n.ID, "content/notes/"+n.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, rb := range runbooks {
		if err := addFile(rb.ID, "content/runbooks/"+rb.ID+".md"); err != nil {
			return nil, err
		}
	}
	return docs, nil
}

// entityIDFromContentPath maps a backend-relative slash path of a content doc
// to the entity ID that owns it: content/features/<ID>/..., content/decisions/
// <ID>.md, content/references/<ID>.md. Returns "" when the path is not under a
// known content kind.
func entityIDFromContentPath(rel string) string {
	switch {
	case strings.HasPrefix(rel, "content/features/"):
		rest := rel[len("content/features/"):]
		if i := strings.IndexByte(rest, '/'); i > 0 {
			return rest[:i]
		}
	case strings.HasPrefix(rel, "content/decisions/") && strings.HasSuffix(rel, ".md"):
		if id := strings.TrimSuffix(rel[len("content/decisions/"):], ".md"); !strings.Contains(id, "/") {
			return id
		}
	case strings.HasPrefix(rel, "content/references/") && strings.HasSuffix(rel, ".md"):
		if id := strings.TrimSuffix(rel[len("content/references/"):], ".md"); !strings.Contains(id, "/") {
			return id
		}
	}
	return ""
}
