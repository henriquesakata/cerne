package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// writeErrata drops content/errata/<id>.md straight into the backend, the
// same layout store.AppendErrata produces.
func writeErrata(t *testing.T, repoDir, id, body string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "errata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll errata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile errata: %v", err)
	}
}

// TestFeatureDetailExposesErrata pins F-0163 crit. 1: the feature detail
// carries content/errata/<id>.md verbatim; absence is the empty string.
func TestFeatureDetailExposesErrata(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateImplemented)
	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateImplemented)
	writeErrata(t, repoDir, "F-0001", "## 2026-07-09\n\nReferência movida.\n")

	var got struct {
		Errata string `json:"errata"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got.Errata == "" || got.Errata != "## 2026-07-09\n\nReferência movida.\n" {
		t.Fatalf("errata = %q, want the errata document verbatim", got.Errata)
	}

	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got.Errata != "" {
		t.Fatalf("errata = %q, want empty for a feature without one", got.Errata)
	}
}

// TestEntityDetailsExposeErrata pins F-0163 crit. 2: decision, runbook and
// note details carry the same errata field.
func TestEntityDetailsExposeErrata(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Dec", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Run", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveRunbook: %v", err)
	}
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	for _, id := range []string{"D-0001", "RB-0001", "N-0001"} {
		writeErrata(t, repoDir, id, "correção de "+id+"\n")
	}

	for _, tc := range []struct{ path, id string }{
		{"/api/decisions/D-0001", "D-0001"},
		{"/api/runbooks/RB-0001", "RB-0001"},
		{"/api/notes/N-0001", "N-0001"},
	} {
		var got struct {
			Errata string `json:"errata"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, tc.path, "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body: %s", tc.path, rec.Code, rec.Body.String())
		}
		if got.Errata != "correção de "+tc.id+"\n" {
			t.Fatalf("%s: errata = %q, want the errata document", tc.path, got.Errata)
		}
	}
}
