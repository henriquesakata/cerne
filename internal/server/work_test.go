package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestWorkEndpoint: GET /api/work returns every work regardless of state —
// the UI filters active vs terminated, not the server.
func TestWorkEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	for _, w := range []store.Work{
		{ID: "W-0001", Title: "Investigar erro X", State: domain.WorkActive, Actor: "a@b", CreatedAt: now, UpdatedAt: now},
		{ID: "W-0002", Title: "Já terminou", State: domain.WorkDone, Actor: "a@b", Note: "ok", CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatalf("SaveWork %s: %v", w.ID, err)
		}
	}

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/work", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload workResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Works) != 2 {
		t.Fatalf("works = %d, want 2 (every state, UI filters)", len(payload.Works))
	}
	ids := map[string]bool{}
	for _, w := range payload.Works {
		ids[w.ID] = true
	}
	if !ids["W-0001"] || !ids["W-0002"] {
		t.Fatalf("works = %+v, want both W-0001 and W-0002", payload.Works)
	}
}

func TestWorkEndpointEmptyBackend(t *testing.T) {
	repoDir, projDir := newBackend(t)

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/work", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload workResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Works) != 0 {
		t.Fatalf("works = %+v, want empty", payload.Works)
	}
}
