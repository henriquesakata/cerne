package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// newBackend creates a git-less backend tree for read-only endpoint tests.
func newBackend(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Projeto Demo", Modules: []string{"core", "web"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	return repoDir, t.TempDir()
}

// newGitBackend creates a full fixture with a bare origin and a docs.lock,
// as app.TransitionFeature commits, pushes and refreshes the lock snapshot.
func newGitBackend(t *testing.T) (repoDir, projDir, origin string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Projeto Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	projDir = filepath.Join(base, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	return repoDir, projDir, origin
}

func seedFeature(t *testing.T, repoDir, id, title string, state domain.FeatureState) {
	t.Helper()
	now := time.Now().UTC()
	f := store.Feature{ID: id, Title: title, State: state, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
}

// commitBackend commits (and pushes) state a test staged directly in the
// backend clone: mutations start from a clean HEAD (F-0121/F-0122), so staged
// state must be committed to be visible to them.
func commitBackend(t *testing.T, repoDir string) {
	t.Helper()
	if _, err := gitback.CommitAndPush(repoDir, "test: stage state"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatalf("commitBackend: %v", err)
	}
}

// writeDoc writes a feature lifecycle document into the backend tree.
func writeDoc(t *testing.T, repoDir, id, filename, content string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", filename, err)
	}
}

func realDoc(id, body string) string {
	// The Verificação/Barra tail satisfies the approve bar gate, which the
	// transition core enforces for the UI path too (F-0295).
	// The Gauntlet tail satisfies the complete result gate (F-0303).
	return "---\ncerne_id: " + id + "\n---\n\n# Título\n\n" + body + "\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n"
}

// writeContentDoc writes a decision/reference content doc at
// content/<kind>/<id>.md (kind is "decisions" or "references").
func writeContentDoc(t *testing.T, repoDir, kind, id, content string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", id, err)
	}
}

// doJSON performs a request against the handler and decodes the JSON body.
func doJSON(t *testing.T, h http.Handler, method, target, body string, out any) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s %s Content-Type = %q, want application/json", method, target, ct)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("%s %s: invalid JSON %q: %v", method, target, rec.Body.String(), err)
	}
	return rec
}

func TestStateEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateImplemented)
	now := time.Now().UTC()
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Decisão X", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := store.SaveReference(repoDir, store.Reference{ID: "R-0001", Title: "Referência Y", State: domain.ReferenceCurrent, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	// F-0188: the capability taxonomy travels in the same payload.
	if err := store.SaveCapability(repoDir, store.Capability{ID: "C-0001", Title: "Studio", State: domain.CapabilityActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveCapability: %v", err)
	}

	var got struct {
		Project      store.Project      `json:"project"`
		Features     []store.Feature    `json:"features"`
		Decisions    []store.Decision   `json:"decisions"`
		References   []store.Reference  `json:"references"`
		Capabilities []store.Capability `json:"capabilities"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Project.Name != "Projeto Demo" || len(got.Project.Modules) != 2 {
		t.Fatalf("project = %+v, want Projeto Demo with 2 modules", got.Project)
	}
	if len(got.Features) != 2 || got.Features[0].ID != "F-0001" || got.Features[1].State != domain.StateImplemented {
		t.Fatalf("features = %+v, want F-0001 + implemented F-0002", got.Features)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].ID != "D-0001" {
		t.Fatalf("decisions = %+v, want D-0001", got.Decisions)
	}
	if len(got.References) != 1 || got.References[0].ID != "R-0001" {
		t.Fatalf("references = %+v, want R-0001", got.References)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0].ID != "C-0001" {
		t.Fatalf("capabilities = %+v, want C-0001", got.Capabilities)
	}
}

func TestStateEndpointNoModules(t *testing.T) {
	// project.json may carry "modules": null; the API contract is always a list.
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Sem Módulos"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	rec := httptest.NewRecorder()
	New(repoDir, t.TempDir(), "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"modules": null`) {
		t.Fatalf("modules serialized as null, want []:\n%s", rec.Body.String())
	}
}

func TestFeatureDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateApproved)
	spec := realDoc("F-0001", "Problema real.")
	plan := realDoc("F-0001", "Passo 1.")
	writeDoc(t, repoDir, "F-0001", "spec.md", spec)
	writeDoc(t, repoDir, "F-0001", "plan.md", plan)
	artDir := filepath.Join(repoDir, "content", "artifacts", "F-0001")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artDir, "fluxo.excalidraw"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile artifact: %v", err)
	}

	var got struct {
		Feature store.Feature `json:"feature"`
		Docs    struct {
			Spec   string `json:"spec"`
			Plan   string `json:"plan"`
			Result string `json:"result"`
		} `json:"docs"`
		Artifacts []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"artifacts"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.ID != "F-0001" || got.Feature.State != domain.StateApproved {
		t.Fatalf("feature = %+v, want approved F-0001", got.Feature)
	}
	if got.Docs.Spec != spec || got.Docs.Plan != plan || got.Docs.Result != "" {
		t.Fatalf("docs = %+v, want spec+plan content and empty result", got.Docs)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0].Name != "fluxo.excalidraw" ||
		got.Artifacts[0].Path != "content/artifacts/F-0001/fluxo.excalidraw" {
		t.Fatalf("artifacts = %+v, want fluxo.excalidraw with backend-relative path", got.Artifacts)
	}
}

// docs_updated carries the per-document last-commit time: present for a
// committed doc, absent (nil) for one that does not exist. It resolves per
// document, so it needs a git-backed backend (newGitBackend).
func TestFeatureDetailDocsUpdated(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real."))
	if _, err := gitback.CommitAndPush(repoDir, "add F-0001 spec"); err != nil {
		t.Fatalf("CommitAndPush spec: %v", err)
	}

	var got struct {
		Docs        struct{ Spec string } `json:"docs"`
		DocsUpdated struct {
			Spec   *time.Time `json:"spec"`
			Plan   *time.Time `json:"plan"`
			Result *time.Time `json:"result"`
		} `json:"docs_updated"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.DocsUpdated.Spec == nil {
		t.Fatalf("docs_updated.spec = nil, want the spec's last commit time")
	}
	if since := time.Since(*got.DocsUpdated.Spec); since > time.Hour {
		t.Fatalf("docs_updated.spec = %v, want a recent commit time (since = %v)", got.DocsUpdated.Spec, since)
	}
	if got.DocsUpdated.Plan != nil || got.DocsUpdated.Result != nil {
		t.Fatalf("docs_updated plan/result = %v/%v, want nil (no such docs)", got.DocsUpdated.Plan, got.DocsUpdated.Result)
	}
}

// docs_real reports, per document, whether it carries real content beyond the
// scaffold cerne writes (headings + HTML-comment guidance). The UI needs this
// to show "Vazio" for a stub instead of rendering the raw template guidance.
func TestFeatureDetailDocsRealContent(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateInDevelopment)
	// spec: real content. plan: scaffold only (heading + comment guidance).
	// result: absent.
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real."))
	scaffold := "---\ncerne_id: F-0001\n---\n\n# Título\n\n<!-- COMO USAR: preencha. -->\n\n## Resultado\n\n<!-- Em 1-2 frases. -->\n"
	writeDoc(t, repoDir, "F-0001", "plan.md", scaffold)

	var got struct {
		DocsReal struct {
			Spec   bool `json:"spec"`
			Plan   bool `json:"plan"`
			Result bool `json:"result"`
		} `json:"docs_real"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !got.DocsReal.Spec {
		t.Fatalf("docs_real.spec = false, want true (spec has real content)")
	}
	if got.DocsReal.Plan {
		t.Fatalf("docs_real.plan = true, want false (plan is only scaffold)")
	}
	if got.DocsReal.Result {
		t.Fatalf("docs_real.result = true, want false (result absent)")
	}
}

func TestFeatureDetailChildren(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Épico", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Filha A", Parent: "F-0001", State: domain.StateApproved},
		{ID: "F-0003", Title: "Filha B", Parent: "F-0001", State: domain.StateImplemented},
	} {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}

	var got struct {
		Children []store.Feature `json:"children"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Children) != 2 || got.Children[0].ID != "F-0002" || got.Children[1].ID != "F-0003" {
		t.Fatalf("children = %+v, want [F-0002, F-0003] sorted", got.Children)
	}

	var leaf struct {
		Children []store.Feature `json:"children"`
	}
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &leaf)
	if leaf.Children == nil || len(leaf.Children) != 0 {
		t.Fatalf("leaf children = %v, want empty (non-null) array", leaf.Children)
	}
}

func TestFeatureDetailNoArtifactsDir(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)

	rec := httptest.NewRecorder()
	New(repoDir, projDir, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features/F-0001", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"artifacts": []`) && !strings.Contains(rec.Body.String(), `"artifacts":[]`) {
		t.Fatalf("body missing empty artifacts list:\n%s", rec.Body.String())
	}
}

func TestFeatureDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-9999", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "feature not found" {
		t.Fatalf("error = %q, want %q", got["error"], "feature not found")
	}
}

func TestDecisionDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Decisão X", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	doc := realDoc("D-0001", "Contexto e consequência da decisão.")
	writeContentDoc(t, repoDir, "decisions", "D-0001", doc)

	var got struct {
		Decision store.Decision `json:"decision"`
		Doc      string         `json:"doc"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/decisions/D-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Decision.ID != "D-0001" || got.Decision.State != domain.DecisionActive {
		t.Fatalf("decision = %+v, want active D-0001", got.Decision)
	}
	if got.Doc != doc {
		t.Fatalf("doc = %q, want the decision content markdown", got.Doc)
	}
}

func TestDecisionDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/decisions/D-9999", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "decision not found" {
		t.Fatalf("error = %q, want %q", got["error"], "decision not found")
	}
}

func TestRunbookDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Recuperar dev env", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveRunbook: %v", err)
	}
	doc := realDoc("RB-0001", "Quando usar e procedimento.")
	writeContentDoc(t, repoDir, "runbooks", "RB-0001", doc)

	var got struct {
		Runbook store.Runbook `json:"runbook"`
		Doc     string        `json:"doc"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/runbooks/RB-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Runbook.ID != "RB-0001" || got.Runbook.State != domain.RunbookActive {
		t.Fatalf("runbook = %+v, want active RB-0001", got.Runbook)
	}
	if got.Doc != doc {
		t.Fatalf("doc = %q, want the runbook content markdown", got.Doc)
	}
}

func TestRunbookDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/runbooks/RB-9999", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "runbook not found" {
		t.Fatalf("error = %q, want %q", got["error"], "runbook not found")
	}
}

func TestStateEndpointIncludesRunbooks(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Deploy", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveRunbook: %v", err)
	}
	var got struct {
		Runbooks []store.Runbook `json:"runbooks"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Runbooks) != 1 || got.Runbooks[0].ID != "RB-0001" {
		t.Fatalf("state runbooks = %+v, want [RB-0001]", got.Runbooks)
	}
}

func TestNoteDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "go-git SSH caveat", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	doc := realDoc("N-0001", "Conhecimento durável de time.")
	writeContentDoc(t, repoDir, "notes", "N-0001", doc)

	var got struct {
		Note store.Note `json:"note"`
		Doc  string     `json:"doc"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/notes/N-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Note.ID != "N-0001" || got.Note.State != domain.NoteActive {
		t.Fatalf("note = %+v, want active N-0001", got.Note)
	}
	if got.Doc != doc {
		t.Fatalf("doc = %q, want the note content markdown", got.Doc)
	}
}

func TestNoteDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/notes/N-9999", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "note not found" {
		t.Fatalf("error = %q, want %q", got["error"], "note not found")
	}
}

func TestStateEndpointIncludesNotes(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Invariante", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	var got struct {
		Notes []store.Note `json:"notes"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Notes) != 1 || got.Notes[0].ID != "N-0001" {
		t.Fatalf("state notes = %+v, want [N-0001]", got.Notes)
	}
}

func TestReferenceDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveReference(repoDir, store.Reference{ID: "R-0001", Title: "Referência Y", State: domain.ReferenceCurrent, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	doc := realDoc("R-0001", "Link e resumo da referência.")
	writeContentDoc(t, repoDir, "references", "R-0001", doc)

	var got struct {
		Reference store.Reference `json:"reference"`
		Doc       string          `json:"doc"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/references/R-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Reference.ID != "R-0001" || got.Reference.State != domain.ReferenceCurrent {
		t.Fatalf("reference = %+v, want current R-0001", got.Reference)
	}
	if got.Doc != doc {
		t.Fatalf("doc = %q, want the reference content markdown", got.Doc)
	}
}

func TestReferenceDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/references/R-9999", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "reference not found" {
		t.Fatalf("error = %q, want %q", got["error"], "reference not found")
	}
}

func TestTransitionApprove(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui."))
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"approve"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got["id"] != "F-0001" || got["from"] != "candidate" || got["to"] != "approved" {
		t.Fatalf("body = %v, want id/from/to of the approve transition", got)
	}

	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "feature.approved" {
		t.Fatalf("events = %+v, want trailing feature.approved", events)
	}

	// transition commits+pushes and refreshes the docs.lock snapshot.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot == "" || lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %q, want origin HEAD %q", lock.Snapshot, originHead)
	}
}

func TestTransitionGateFailure(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"approve"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got["error"], "spec.md") || !strings.Contains(got["error"], "write") {
		t.Fatalf("error = %q, want gate message naming spec.md", got["error"])
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("state after failed gate = %s, want candidate", f.State)
	}
}

func TestTransitionUnknownFeature(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-9999/transition",
		`{"action":"approve"}`, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "feature not found" {
		t.Fatalf("error = %q, want %q", got["error"], "feature not found")
	}
}

func TestTransitionInvalidBody(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{not json`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"explode"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown action status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(got["error"], "explode") {
		t.Fatalf("error = %q, want message naming the unknown action", got["error"])
	}
}

func TestStaticIndexAndSPAFallback(t *testing.T) {
	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "")

	for _, target := range []string{"/", "/features/F-0001"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s Content-Type = %q, want text/html", target, ct)
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
			t.Fatalf("GET %s body is not the SPA index:\n%s", target, rec.Body.String())
		}
	}
}

func TestAPIUnknownRouteIsJSON404(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/nope", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want JSON error message", got)
	}
}

func TestSearchEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir()) // index.db lands under a temp dir
	now := time.Now().UTC()
	// A superseded feature: search must reach history, with the successor shown.
	f := store.Feature{ID: "F-0001", Title: "Boletos recorrentes", State: domain.StateSuperseded, SupersededBy: "F-0002", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Pagamento de boletos mensais no satélite."))

	var got struct {
		Results []struct {
			EntityID     string `json:"entity_id"`
			Kind         string `json:"kind"`
			State        string `json:"state"`
			SupersededBy string `json:"superseded_by"`
		} `json:"results"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-search"), http.MethodGet, "/api/search?q=boletos", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, r := range got.Results {
		if r.EntityID == "F-0001" {
			found = true
			if r.Kind != "feature" || r.State != "superseded" || r.SupersededBy != "F-0002" {
				t.Fatalf("hit = %+v, want superseded feature F-0001 → F-0002", r)
			}
		}
	}
	if !found {
		t.Fatalf("F-0001 not found in results for 'boletos': %+v", got.Results)
	}
}

// F-0343 criterion 6: same broadening rule and "broadened" field as the CLI.
func TestSearchEndpointBroadened(t *testing.T) {
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	now := time.Now().UTC()
	for id, body := range map[string]string{"F-0001": "troca de conta e chave", "F-0002": "criptografia do switcher dual"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: id, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, body))
	}
	var got struct {
		Results []struct {
			EntityID string `json:"entity_id"`
		} `json:"results"`
		Broadened bool `json:"broadened"`
	}
	srv := New(repoDir, projDir, "proj-search")
	rec := doJSON(t, srv, http.MethodGet, "/api/search?q=troca+conta+chave+criptografia+switcher+dual", "", &got)
	if rec.Code != http.StatusOK || !got.Broadened || len(got.Results) != 2 {
		t.Fatalf("code=%d broadened=%v results=%d, want 200/true/2: %s", rec.Code, got.Broadened, len(got.Results), rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/search?q=troca+conta+chave", "", &struct{}{})
	if strings.Contains(rec.Body.String(), "broadened") {
		t.Fatalf("exact hit must not carry broadened: %s", rec.Body.String())
	}
}

func TestSearchEndpointEmptyQuery(t *testing.T) {
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-search"), http.MethodGet, "/api/search?q=", "", &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
}

func TestLintEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	// An approved feature with no real spec.md is a missing-gate-doc finding.
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateApproved)

	var got struct {
		Findings []struct {
			Rule   string `json:"rule"`
			Entity string `json:"entity"`
			Detail string `json:"detail"`
		} `json:"findings"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/lint", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, f := range got.Findings {
		if f.Rule == "missing-gate-doc" && f.Entity == "F-0001" {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v, want a missing-gate-doc for F-0001", got.Findings)
	}
}

func TestLintEndpointClean(t *testing.T) {
	repoDir, projDir := newBackend(t)
	// A candidate with a real, template-complete spec naming its bar (F-0308)
	// leaves nothing stale or incomplete.
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	// Tagged so untagged-live (features joined that rule in D-0003) stays silent —
	// this test asserts a genuinely clean backend.
	if f, err := store.LoadFeature(repoDir, "F-0001"); err == nil {
		f.Facets = []string{"code"}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("tag F-0001: %v", err)
		}
	}
	writeDoc(t, repoDir, "F-0001", "spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Feature A\n\n## Problema\n\nReal.\n\n## Objetivo e critérios de aceitação\n\n1. Critério.\n\n## Decisões do design\n\nN/A\n\n## Desenho\n\nReal.\n\n## Verificação\n\nBarra: referência nomeada — acesso\n\n## Execução recomendada (agente)\n\nReal.\n\n## Fora de escopo\n\nN/A\n")

	var got struct {
		Findings []struct {
			Rule string `json:"rule"`
		} `json:"findings"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/lint", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none on a clean backend", got.Findings)
	}
}

func TestEventsEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0001", "feature.created", map[string]string{"title": "A"}, "tester")); err != nil {
		t.Fatalf("AppendEvent 1: %v", err)
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0001", "feature.approved", nil, "tester")); err != nil {
		t.Fatalf("AppendEvent 2: %v", err)
	}

	var got struct {
		Events []struct {
			EntityID string `json:"entity_id"`
			Type     string `json:"type"`
			Actor    string `json:"actor"`
		} `json:"events"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/events", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Events) != 2 {
		t.Fatalf("events = %+v, want 2", got.Events)
	}
	// Newest first: the approved event (appended last) leads the timeline.
	if got.Events[0].Type != "feature.approved" || got.Events[1].Type != "feature.created" {
		t.Fatalf("order = [%s, %s], want [feature.approved, feature.created]", got.Events[0].Type, got.Events[1].Type)
	}
	if got.Events[0].EntityID != "F-0001" || got.Events[0].Actor != "tester" {
		t.Fatalf("lead event = %+v, want F-0001 by tester", got.Events[0])
	}
}

func TestEventsEndpointEmpty(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got struct {
		Events []struct{} `json:"events"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/events", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Events) != 0 {
		t.Fatalf("events = %+v, want none", got.Events)
	}
	if !strings.Contains(rec.Body.String(), `"events": []`) {
		t.Fatalf("empty events not serialized as []:\n%s", rec.Body.String())
	}
}

func TestCreateFeatureEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // no declared modules → module optional

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features",
		`{"title":"Nova feature da UI"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.ID == "" || got.Feature.Title != "Nova feature da UI" || got.Feature.State != domain.StateCandidate {
		t.Fatalf("feature = %+v, want a candidate with the given title", got.Feature)
	}
	if f, err := store.LoadFeature(repoDir, got.Feature.ID); err != nil || f.State != domain.StateCandidate {
		t.Fatalf("not persisted: %+v %v", f, err)
	}
}

func TestCreateFeatureEndpointWithParent(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001 (candidate)

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features",
		`{"title":"Filha","parent":"F-0001"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.Feature.Parent != "F-0001" {
		t.Fatalf("feature.Parent = %q, want F-0001", got.Feature.Parent)
	}

	var errBody map[string]string
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features",
		`{"title":"X","parent":"F-9999"}`, &errBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown parent status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFeatureEndpointEmptyTitle(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features", `{"title":"  "}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("want pt-BR error message")
	}
}

func TestCreateFeatureEndpointInvalidBody(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features", `{not json`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRenameTitleEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001 (candidate)
	h := New(repoDir, projDir, "")

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, h, http.MethodPut, "/api/features/F-0001/title", `{"title":"Título novo"}`, &got)
	if rec.Code != http.StatusOK || got.Feature.Title != "Título novo" {
		t.Fatalf("rename: status %d title %q, want 200 + 'Título novo'; body: %s", rec.Code, got.Feature.Title, rec.Body.String())
	}

	var e map[string]string
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-9999/title", `{"title":"X"}`, &e); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown feature status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-0001/title", `{"title":"   "}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty title status = %d, want 422", rec.Code)
	}

	// Freeze the feature into a terminal state, then a rename must be refused.
	f, _ := store.LoadFeature(repoDir, "F-0001")
	f.State = domain.StateRejected
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	commitBackend(t, repoDir)
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-0001/title", `{"title":"Tentativa"}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("terminal rename status = %d, want 422", rec.Code)
	}
}

func TestSetParentEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001 (candidate)
	h := New(repoDir, projDir, "")

	var created struct {
		Feature store.Feature `json:"feature"`
	}
	doJSON(t, h, http.MethodPost, "/api/features", `{"title":"Filha"}`, &created)
	child := created.Feature.ID

	var got struct {
		Feature store.Feature `json:"feature"`
	}
	rec := doJSON(t, h, http.MethodPut, "/api/features/"+child+"/parent", `{"parent":"F-0001"}`, &got)
	if rec.Code != http.StatusOK || got.Feature.Parent != "F-0001" {
		t.Fatalf("reparent: status %d parent %q, want 200 + F-0001; body: %s", rec.Code, got.Feature.Parent, rec.Body.String())
	}

	// Clear: the response omits an empty parent (omitempty), so assert against
	// the persisted state rather than the reused struct.
	rec = doJSON(t, h, http.MethodPut, "/api/features/"+child+"/parent", `{"parent":""}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if f, _ := store.LoadFeature(repoDir, child); f.Parent != "" {
		t.Fatalf("clear: persisted parent = %q, want empty", f.Parent)
	}

	var e map[string]string
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-9999/parent", `{"parent":"F-0001"}`, &e); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown feature status = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/features/"+child+"/parent", `{"parent":"F-9999"}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown parent status = %d, want 422", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/features/"+child+"/parent", `{"parent":"`+child+`"}`, &e); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("self parent status = %d, want 422", rec.Code)
	}
}

func TestFeatureDocPut(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001 candidate, no docs
	realSpec := realDoc("F-0001", "Problema real descrito aqui.")

	var got struct {
		Saved bool `json:"saved"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0001/docs/spec", realSpec, &got)
	if rec.Code != http.StatusOK || !got.Saved {
		t.Fatalf("status=%d saved=%v body=%s, want 200 saved", rec.Code, got.Saved, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil || string(data) != realSpec {
		t.Fatalf("spec not written to backend: %v / %q", err, string(data))
	}

	// The approve gate now passes (spec is real) — checked via the endpoint.
	var tr map[string]string
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition", `{"action":"approve"}`, &tr)
	if rec.Code != http.StatusOK || tr["to"] != "approved" {
		t.Fatalf("approve after real spec: status=%d body=%s, want 200 to=approved", rec.Code, rec.Body.String())
	}
}

func TestFeatureDocPutBadDoc(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0001/docs/notes", "x", &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestFeatureDocPutUnknownFeature(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-9999/docs/spec", realDoc("F-9999", "x"), &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] != "feature not found" {
		t.Fatalf("error = %q, want 'feature not found'", got["error"])
	}
}

// TestFeatureDocPutRefusesFrozen (F-0305 crit. 6): the feature page writes only
// while the feature is live; past it the PUT is a 422 naming the state's path,
// with the document and the event log untouched.
func TestFeatureDocPutRefusesFrozen(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	frozen := []struct {
		id    string
		state domain.FeatureState
		path  string
	}{
		{"F-0002", domain.StateImplemented, ""},
		{"F-0003", domain.StateDone, ""},
		{"F-0004", domain.StateRejected, ""},
		{"F-0005", domain.StateSuperseded, ""},
	}
	for i := range frozen { // the state's path, read from the one correction table (F-0307)
		kind, cmd := lint.Correction(frozen[i].state, frozen[i].id, "result")
		frozen[i].path = cmd
		if kind == lint.CorrectNone {
			frozen[i].path = app.NoCorrectionNote(frozen[i].id)
		}
	}
	for _, f := range frozen {
		seedFeature(t, repoDir, f.id, "Congelada", f.state)
		writeDoc(t, repoDir, f.id, "result.md", realDoc(f.id, "Result original."))
	}
	seedFeature(t, repoDir, "F-0006", "Viva", domain.StateInDevelopment)
	commitBackend(t, repoDir)
	evsBefore, _ := store.ReadEvents(repoDir)

	for _, f := range frozen {
		t.Run(string(f.state), func(t *testing.T) {
			var got map[string]string
			rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/"+f.id+"/docs/result", realDoc(f.id, "Reescrito."), &got)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(got["error"], f.path) {
				t.Fatalf("error = %q, want the state's path %q", got["error"], f.path)
			}
			t.Logf("422: %s", got["error"])
			data, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", f.id, "result.md"))
			if string(data) != realDoc(f.id, "Result original.") {
				t.Fatalf("result.md changed:\n%s", data)
			}
		})
	}
	// The amend reaches only the result: the spec of an implemented feature is
	// corrected by errata, and the refusal says so.
	t.Run("spec of implemented", func(t *testing.T) {
		specPath := filepath.Join(repoDir, "content", "features", "F-0002", "spec.md")
		before, _ := os.ReadFile(specPath)
		var got map[string]string
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0002/docs/spec", realDoc("F-0002", "Spec reescrita."), &got)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
		}
		if _, errata := lint.Correction(domain.StateImplemented, "F-0002", "spec"); !strings.Contains(got["error"], errata) || strings.Contains(got["error"], "amend") {
			t.Fatalf("error = %q, want the errata path %q and no amend", got["error"], errata)
		}
		if after, _ := os.ReadFile(specPath); string(after) != string(before) {
			t.Fatalf("spec.md changed:\n%s", after)
		}
	})
	if evs, _ := store.ReadEvents(repoDir); len(evs) != len(evsBefore) {
		t.Fatalf("events = %d, want %d (a refused PUT appends nothing)", len(evs), len(evsBefore))
	}

	var ok map[string]bool
	if rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0006/docs/result", realDoc("F-0006", "Vivo."), &ok); rec.Code != http.StatusOK {
		t.Fatalf("in_development status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// F-0195: the board needs to see a pending clarification without opening the
// feature, so /state carries a per-entity count of open markers.
func TestStateIncludesOpenClarificationCounts(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001",
		"[ESCLARECER: síncrona ou assíncrona?]\n\n[ESCLARECIDO: qual banco? → sqlite]\n\n[ESCLARECER: qual timeout?]"))
	// Implemented is settled history, not an answerable queue: it must not count.
	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateImplemented)
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "[ESCLARECER: pergunta antiga?]"))

	var got struct {
		OpenClarifications map[string]int `json:"open_clarifications"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.OpenClarifications["F-0001"] != 2 {
		t.Fatalf("open_clarifications[F-0001] = %d, want 2 (resolved marker must not count); got %+v",
			got.OpenClarifications["F-0001"], got.OpenClarifications)
	}
	if _, ok := got.OpenClarifications["F-0002"]; ok {
		t.Fatalf("implemented feature must not appear: %+v", got.OpenClarifications)
	}
}

// F-0202: the board needs to see agent collisions without opening the CLI, so
// /state carries the active W-items referencing each feature.
func TestStateIncludesActiveWork(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateApproved)
	now := time.Now().UTC()
	for _, w := range []store.Work{
		{ID: "W-0001", Title: "Investigar X", State: domain.WorkActive, Actor: "alice@host", Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now},
		{ID: "W-0002", Title: "Investigar Y", State: domain.WorkActive, Actor: "bob@host", Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now},
		{ID: "W-0003", Title: "Já terminou", State: domain.WorkDone, Actor: "alice@host", Refs: []string{"F-0001"}, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatalf("SaveWork %s: %v", w.ID, err)
		}
	}

	var got struct {
		ActiveWork map[string][]activeWorkItem `json:"active_work"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	items := got.ActiveWork["F-0001"]
	if len(items) != 2 {
		t.Fatalf("active_work[F-0001] = %+v, want 2 items (done work must not count)", items)
	}
	actors := map[string]bool{}
	for _, it := range items {
		actors[it.Actor] = true
	}
	if !actors["alice@host"] || !actors["bob@host"] {
		t.Fatalf("active_work[F-0001] actors = %+v, want alice@host and bob@host", items)
	}
}

func TestStateActiveWorkEmptyNotNull(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)

	var raw map[string]json.RawMessage
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &raw)
	body, ok := raw["active_work"]
	if !ok {
		t.Fatal("active_work missing from /state")
	}
	if string(body) != "{}" {
		t.Fatalf("active_work = %s, want {}", body)
	}
}

func TestStateOpenClarificationsEmptyNotNull(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)

	// Asserted on the raw JSON: the decoded struct cannot tell null from {}.
	var raw map[string]json.RawMessage
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &raw)
	body, ok := raw["open_clarifications"]
	if !ok {
		t.Fatal("open_clarifications missing from /state")
	}
	if string(body) != "{}" {
		t.Fatalf("open_clarifications = %s, want {}", body)
	}
}

func TestStateSurvivesClarifyScanFailure(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	// A file where the feature's content directory belongs: reading it as a
	// directory fails on every platform (unlike a chmod-based fixture).
	if err := os.MkdirAll(filepath.Join(repoDir, "content", "features"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "content", "features", "F-0001"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var got struct {
		Features           []store.Feature `json:"features"`
		OpenClarifications map[string]int  `json:"open_clarifications"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Features) != 1 {
		t.Fatalf("features = %+v, want the board served anyway", got.Features)
	}
	if len(got.OpenClarifications) != 0 {
		t.Fatalf("open_clarifications = %+v, want empty on scan failure", got.OpenClarifications)
	}
}

// F-0367 criterion 7: the UI search returns one hit per entity with its
// matching docs; the fields of today keep their names.
func TestSearchEndpointOneHitPerEntity(t *testing.T) {
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Pagamento de boletos mensais."))
	writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "Gerar os boletos no lote."))
	var got struct {
		Results []struct {
			EntityID string   `json:"entity_id"`
			Doc      string   `json:"doc"`
			Snippet  string   `json:"snippet"`
			Docs     []string `json:"docs"`
		} `json:"results"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-search"), http.MethodGet, "/api/search?q=boletos", "", &got)
	if rec.Code != http.StatusOK || len(got.Results) != 1 {
		t.Fatalf("code=%d results=%d, want 200 and one hit: %s", rec.Code, len(got.Results), rec.Body.String())
	}
	r := got.Results[0]
	if r.EntityID != "F-0001" || r.Doc == "" || r.Snippet == "" || strings.Join(r.Docs, ",") != "spec,plan" {
		t.Fatalf("hit = %+v, want F-0001 with docs [spec plan]", r)
	}
}
