package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
)

// TestRoastByoKey pins F-0271 criteria 1/2: an explicit POST runs the three
// personas, persists the report next to the candidate (artifact) and returns
// it; the feature detail then exposes the persisted report.
func TestRoastByoKey(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y."))
	// Committed so the artifact save's snapshot reconcile keeps the spec —
	// the detail assertions below compare the round's stamp against it.
	commitBackend(t, repoDir)

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

	// the feature page reads the round back from the detail payload (F-0277
	// criteria 1/4): listed, stamped, and not marked stale — the spec is
	// exactly what this round judged
	var detail struct {
		RoastRounds []roastRoundJSON `json:"roast_rounds"`
	}
	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	if len(detail.RoastRounds) != 1 {
		t.Fatalf("roast_rounds = %v, want 1 round", detail.RoastRounds)
	}
	round := detail.RoastRounds[0]
	if round.Name != filepath.Base(files[0]) || round.At == nil || round.SpecChanged {
		t.Fatalf("round = %+v, want name=%s, stamped at, spec_changed=false", round, filepath.Base(files[0]))
	}
}

type roastRoundJSON struct {
	Name        string  `json:"name"`
	At          *string `json:"at"`
	SpecChanged bool    `json:"spec_changed"`
}

// TestRoastRoundsSpecChanged pins F-0277 criteria 1/4: the detail payload
// lists every persisted round newest-first, flags a round whose stamped spec
// sha no longer matches the current spec, tolerates a stampless legacy
// roast.md (no timestamp, never flagged), and yields [] with no rounds.
func TestRoastRoundsSpecChanged(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	spec := realDoc("F-0001", "corpo")
	writeDoc(t, repoDir, "F-0001", "spec.md", spec)

	srvNew := New(repoDir, projDir, "proj-x")
	var detail struct {
		RoastRounds []roastRoundJSON `json:"roast_rounds"`
	}
	rec := doJSON(t, srvNew, http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK || detail.RoastRounds == nil || len(detail.RoastRounds) != 0 {
		t.Fatalf("no rounds: status=%d rounds=%v, want empty non-null list", rec.Code, detail.RoastRounds)
	}

	curSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(spec)))[:8]
	dir := filepath.Join(repoDir, "content", "artifacts", "F-0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// older round, judged a different spec → stale
	write("roast-20260101T000000.000000000Z.md",
		"# Roast\n\nRodada: 2026-01-01T00:00:00Z · spec sha256:deadbeef — se a spec mudou depois disso, os trechos citados podem não existir mais.\n\nachados\n")
	// newest round, judged the current spec → fresh
	write("roast-20260201T000000.000000000Z.md",
		"# Roast\n\nRodada: 2026-02-01T00:00:00Z · spec sha256:"+curSHA+" — se a spec mudou depois disso, os trechos citados podem não existir mais.\n\nachados\n")
	// legacy single-file report without a stamp: still a round, never flagged
	write("roast.md", "# Roast\n\nachados antigos\n")

	rec = doJSON(t, srvNew, http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	if len(detail.RoastRounds) != 3 {
		t.Fatalf("roast_rounds = %+v, want 3", detail.RoastRounds)
	}
	r0, r1, r2 := detail.RoastRounds[0], detail.RoastRounds[1], detail.RoastRounds[2]
	if r0.Name != "roast-20260201T000000.000000000Z.md" || r0.At == nil || r0.SpecChanged {
		t.Fatalf("newest round = %+v, want fresh round first", r0)
	}
	if r1.Name != "roast-20260101T000000.000000000Z.md" || r1.At == nil || !r1.SpecChanged {
		t.Fatalf("older round = %+v, want spec_changed=true", r1)
	}
	if r2.Name != "roast.md" || r2.At != nil || r2.SpecChanged {
		t.Fatalf("legacy round = %+v, want stampless last, never flagged", r2)
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

// TestArtifactsExcludeRoastReports pins F-0277 criterion 5: roast reports
// live in the artifacts dir but are rounds, not diagrams — the detail's
// artifacts list (the Diagrams section) must not show them.
func TestArtifactsExcludeRoastReports(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	dir := filepath.Join(repoDir, "content", "artifacts", "F-0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// roast-findings.excalidraw: a user diagram whose slug happens to start
	// with "roast-" — a report filter keyed on prefix alone would hide it.
	for _, name := range []string{"roast.md", "roast-20260101T000000.000000000Z.md", "fluxo.excalidraw", "roast-findings.excalidraw"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var detail struct {
		Artifacts []struct {
			Name string `json:"name"`
		} `json:"artifacts"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	names := []string{}
	for _, a := range detail.Artifacts {
		names = append(names, a.Name)
	}
	if len(names) != 2 || names[0] != "fluxo.excalidraw" || names[1] != "roast-findings.excalidraw" {
		t.Fatalf("artifacts = %v, want [fluxo.excalidraw roast-findings.excalidraw]", names)
	}
}

// TestRoastRoundUnreadableTolerated: one unreadable round (here a directory
// matching the round glob) must not 500 the whole feature page; readable
// rounds still list, and two stampless rounds order newest-name first.
func TestRoastRoundUnreadableTolerated(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	dir := filepath.Join(repoDir, "content", "artifacts", "F-0001")
	if err := os.MkdirAll(filepath.Join(dir, "roast-20260101T000000.000000000Z.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"roast-a.md", "roast-b.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("sem carimbo"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var detail struct {
		RoastRounds []roastRoundJSON `json:"roast_rounds"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200 despite the unreadable round", rec.Code)
	}
	if len(detail.RoastRounds) != 2 || detail.RoastRounds[0].Name != "roast-b.md" || detail.RoastRounds[1].Name != "roast-a.md" {
		t.Fatalf("roast_rounds = %+v, want [roast-b.md roast-a.md]", detail.RoastRounds)
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
