package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestConventionsExport covers the F-0474 export contract: only ACTIVE notes
// carrying BOTH the convention and platform components are rendered, each as a
// "## <id> — <title>" section with a synthesized components marker, body with
// the frontmatter stripped.
func TestConventionsExport(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()

	save := func(n store.Note) {
		t.Helper()
		if err := store.SaveNote(repoDir, n); err != nil {
			t.Fatalf("SaveNote %s: %v", n.ID, err)
		}
	}
	save(store.Note{ID: "N-0001", Title: "Campo de data", State: domain.NoteActive,
		Components: []string{"convention", "field-date", "platform"}, CreatedAt: now, UpdatedAt: now})
	save(store.Note{ID: "N-0002", Title: "Hub-only netpol", State: domain.NoteActive,
		Components: []string{"convention", "arch-netpol"}, CreatedAt: now, UpdatedAt: now})
	save(store.Note{ID: "N-0003", Title: "Superseded platform", State: domain.NoteSuperseded,
		Components: []string{"convention", "platform"}, CreatedAt: now, UpdatedAt: now})
	save(store.Note{ID: "N-0004", Title: "Sem convention", State: domain.NoteActive,
		Components: []string{"platform"}, CreatedAt: now, UpdatedAt: now})
	writeContentDoc(t, repoDir, "notes", "N-0001", realDoc("N-0001", "Use o DatePicker."))
	writeContentDoc(t, repoDir, "notes", "N-0002", realDoc("N-0002", "Registrar em PLATFORM_WORKLOADS."))

	req := httptest.NewRequest(http.MethodGet, "/api/conventions-export", nil)
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("Content-Type = %q, want text/markdown", ct)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatalf("missing ETag header")
	}
	body := rec.Body.String()

	if !strings.Contains(body, "## N-0001 — Campo de data\n<!-- components: convention,field-date,platform -->") {
		t.Fatalf("platform note section missing or marker wrong; body:\n%s", body)
	}
	if !strings.Contains(body, "Use o DatePicker.") {
		t.Fatalf("platform note body missing; body:\n%s", body)
	}
	if strings.Contains(body, "cerne_id:") {
		t.Fatalf("frontmatter leaked into export; body:\n%s", body)
	}
	for _, excluded := range []string{"N-0002", "N-0003", "N-0004", "PLATFORM_WORKLOADS"} {
		if strings.Contains(body, excluded) {
			t.Fatalf("non-platform/superseded content %q leaked; body:\n%s", excluded, body)
		}
	}
}

// TestConventionsExportEmptyCatalog: no matching notes → 200 with empty body,
// never an error (the consumer treats empty as "no platform conventions yet").
func TestConventionsExportEmptyCatalog(t *testing.T) {
	repoDir, projDir := newBackend(t)
	req := httptest.NewRequest(http.MethodGet, "/api/conventions-export", nil)
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

// TestConventionsExportStableETag: same catalog → same ETag; catalog change →
// different ETag (the harness provider uses it for cheap cache validation).
func TestConventionsExportStableETag(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteActive,
		Components: []string{"convention", "platform"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	writeContentDoc(t, repoDir, "notes", "N-0001", realDoc("N-0001", "Corpo v1."))

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/conventions-export", nil)
		rec := httptest.NewRecorder()
		New(repoDir, projDir, "").ServeHTTP(rec, req)
		return rec
	}
	first, second := get(), get()
	if first.Header().Get("ETag") != second.Header().Get("ETag") {
		t.Fatalf("ETag unstable for same catalog: %q vs %q", first.Header().Get("ETag"), second.Header().Get("ETag"))
	}
	writeContentDoc(t, repoDir, "notes", "N-0001", realDoc("N-0001", "Corpo v2."))
	third := get()
	if third.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Fatalf("ETag did not change with catalog content")
	}
}
