package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
)

// TestRoastByoKey pins F-0271 criteria 1/2: an explicit POST runs the three
// personas, persists the report next to the candidate (artifact) and returns
// it; the feature detail then exposes the persisted report.
func TestRoastByoKey(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y."))

	var gotPrompt string
	srv := fakeDiagramProvider(t, &gotPrompt, `- [alta] critério vago — trecho: \"QUANDO x\"`)
	configureByoKey(t, projDir, srv.URL)

	var got struct {
		Generated     bool   `json:"generated"`
		NeedsProvider bool   `json:"needs_provider"`
		Partial       bool   `json:"partial"`
		Report        string `json:"report"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/roast", `{}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !got.Generated || got.NeedsProvider || got.Partial {
		t.Fatalf("generated=%v needs_provider=%v partial=%v, want true/false/false", got.Generated, got.NeedsProvider, got.Partial)
	}
	for _, p := range app.RoastPersonas {
		if !strings.Contains(got.Report, p.Title) {
			t.Fatalf("report missing persona %q:\n%s", p.Title, got.Report)
		}
	}
	if !strings.Contains(got.Report, "[alta]") {
		t.Fatalf("report missing findings:\n%s", got.Report)
	}

	// persisted next to the candidate, in the backend content tree — one
	// artifact per round (F-0277)
	files, err := filepath.Glob(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast-*.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("persisted roast rounds = %v (err=%v), want 1", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("persisted roast: %v", err)
	}
	if !strings.Contains(string(data), "Roast da spec") {
		t.Fatalf("persisted roast content:\n%s", data)
	}

	// the feature page reads it back from the detail payload
	var detail struct {
		RoastReport string `json:"roast_report"`
	}
	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	if !strings.Contains(detail.RoastReport, "[alta]") {
		t.Fatalf("detail roast_report missing findings:\n%s", detail.RoastReport)
	}
}

// TestRoastRoundsPersistSeparately pins F-0277 criterion 3: each run persists
// its own roast-<stamp>.md artifact — a second round never overwrites the
// first, so the history of rounds survives.
func TestRoastRoundsPersistSeparately(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "corpo"))
	// Committed: the first round's snapshot reconcile would sweep an
	// uncommitted spec away before the second round reads it.
	commitBackend(t, repoDir)

	var gotPrompt string
	srv := fakeDiagramProvider(t, &gotPrompt, `- [alta] achado — trecho: \"corpo\"`)
	configureByoKey(t, projDir, srv.URL)

	for i := 0; i < 2; i++ {
		var got struct {
			Generated bool `json:"generated"`
		}
		rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
			"/api/features/F-0001/roast", `{}`, &got)
		if rec.Code != http.StatusOK || !got.Generated {
			t.Fatalf("run %d: status=%d generated=%v body=%s", i, rec.Code, got.Generated, rec.Body.String())
		}
	}

	files, err := filepath.Glob(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("roast-*.md count = %d (%v), want 2 — a round must not overwrite the previous one", len(files), files)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast.md")); !os.IsNotExist(err) {
		t.Fatalf("bare roast.md must no longer be written (err=%v)", err)
	}
}

// TestRoastNeedsProvider: delegation mode has no server-side model — the
// endpoint signals needs_provider and writes nothing.
func TestRoastNeedsProvider(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "corpo"))

	var got struct {
		Generated     bool `json:"generated"`
		NeedsProvider bool `json:"needs_provider"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-0001/roast", `{}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got.Generated || !got.NeedsProvider {
		t.Fatalf("generated=%v needs_provider=%v, want false/true", got.Generated, got.NeedsProvider)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast.md")); !os.IsNotExist(err) {
		t.Fatalf("roast.md must not exist in delegation mode (err=%v)", err)
	}
}

// TestRoastUnknownFeature: 404, mirror of every id-scoped handler.
func TestRoastUnknownFeature(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var errBody struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-9999/roast", `{}`, &errBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestRoastAllCriticsFail: a round where no critic finishes (dead provider,
// cancelled request) must NOT overwrite roast.md — committed garbage is worse
// than no report.
func TestRoastAllCriticsFail(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "corpo"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	configureByoKey(t, projDir, srv.URL)

	var errBody struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/roast", `{}`, &errBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast.md")); !os.IsNotExist(err) {
		t.Fatalf("roast.md must not be written on an all-failed round (err=%v)", err)
	}
}

// TestRoastStates: the roast targets a live elaboration; terminal features
// (rejected/superseded) refuse instead of burning three model calls.
func TestRoastTerminalState(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0002", "Rejeitada", domain.StateRejected)
	var errBody struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-0002/roast", `{}`, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}
