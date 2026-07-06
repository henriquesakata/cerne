package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestVersionEndpoint: GET /api/version returns the binary's build version —
// the same string `cerne --version` reports ("dev" outside a release build).
func TestVersionEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != "dev" {
		t.Errorf("version = %q, want \"dev\"", body.Version)
	}
}
