package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestGraphEndpoint: GET /api/graph returns the projected nodes and edges,
// the superseded_by edge carrying the entity's reason.
func TestGraphEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Bills", State: domain.StateSuperseded, SupersededBy: "F-0002", Reason: "absorvida pelo Finance", CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Finance", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/graph", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var g app.Graph
	if err := json.NewDecoder(rec.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(g.Nodes), g.Nodes)
	}
	if len(g.Edges) != 1 || g.Edges[0].Type != app.EdgeSupersededBy || g.Edges[0].Reason != "absorvida pelo Finance" {
		t.Fatalf("edges = %+v, want one superseded_by carrying the reason", g.Edges)
	}
}
