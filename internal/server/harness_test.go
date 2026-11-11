package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/harness"
	"github.com/henriquesakata/cerne/internal/hooks"
)

// TestHarnessEndpoint: GET /api/harness returns the inventory with the hooks
// class populated and the layer report, HTTP 200 even with degraded layers
// (F-0251 crits. 1, 14, 15).
func TestHarnessEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if _, err := hooks.Install(projDir); err != nil {
		t.Fatal(err)
	}
	// Malformed local settings: degraded layer, never a blank error.
	if err := os.WriteFile(filepath.Join(projDir, ".claude", "settings.local.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeHome := t.TempDir()
	orig := harnessHome
	harnessHome = func() string { return fakeHome }
	defer func() { harnessHome = orig }()

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/harness", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var inv harness.Inventory
	if err := json.NewDecoder(rec.Body).Decode(&inv); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if inv.Version == "" {
		t.Error("payload without cerne version")
	}
	var hookOK, layerMarked bool
	for _, c := range inv.Classes {
		if c.Name == "hooks" {
			for _, it := range c.Items {
				if it.State == harness.StateOK {
					hookOK = true
				}
			}
		}
	}
	for _, l := range inv.Layers {
		if !l.Readable && filepath.Base(l.Path) == "settings.local.json" && l.Error != "" {
			layerMarked = true
		}
	}
	if !hookOK {
		t.Error("installed hooks not reported ok")
	}
	if !layerMarked {
		t.Error("malformed layer not marked with reason")
	}
}
