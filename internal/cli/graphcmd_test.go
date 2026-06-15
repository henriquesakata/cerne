package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedGraphFeatures saves a superseded→current feature pair (with a reason)
// into the backend clone and pushes it.
func seedGraphFeatures(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Bills", State: domain.StateSuperseded, SupersededBy: "F-0002", Reason: "absorvida pelo Finance"},
		{ID: "F-0002", Title: "Finance", State: domain.StateImplemented},
	} {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed graph fixtures"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestGraphJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedGraphFeatures(t, repoDir)

	out, err := execCerne(t, "graph", "--dir", projDir)
	if err != nil {
		t.Fatalf("graph: %v\n%s", err, out)
	}
	var g app.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(g.Nodes), g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].Type != app.EdgeSupersededBy || g.Edges[0].Reason != "absorvida pelo Finance" {
		t.Fatalf("edges = %+v, want one superseded_by carrying the reason", g.Edges)
	}
}

func TestGraphDOT(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedGraphFeatures(t, repoDir)

	out, err := execCerne(t, "graph", "--format", "dot", "--dir", projDir)
	if err != nil {
		t.Fatalf("graph dot: %v\n%s", err, out)
	}
	for _, want := range []string{"digraph cerne {", `"F-0001" -> "F-0002"`, "superseded_by: absorvida pelo Finance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dot output missing %q:\n%s", want, out)
		}
	}
}

func TestGraphInvalidFormat(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "graph", "--format", "xml", "--dir", projDir); err == nil {
		t.Fatalf("invalid format should error")
	}
}
