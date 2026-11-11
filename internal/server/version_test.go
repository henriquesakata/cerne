package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henriquesakata/cerne/internal/version"
)

// TestVersionEndpoint: GET /api/version returns the shortened MAJOR.MINOR
// version (F-0144) — the same string `cerne --version` reports ("dev" outside
// a release build, "0.3" for a stamped "v0.3.0-12-g…").
func TestVersionEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)

	for _, tt := range []struct{ stamped, want string }{
		{"dev", "dev"},
		{"v0.3.0-12-gd7ed7433", "0.3"},
	} {
		orig := version.Version
		version.Version = tt.stamped
		h := New(repoDir, projDir, "proj")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
		version.Version = orig

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Version != tt.want {
			t.Errorf("version with stamp %q = %q, want %q", tt.stamped, body.Version, tt.want)
		}
	}
}
