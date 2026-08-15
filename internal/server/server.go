// Package server exposes the cerne local UI: JSON API under /api plus the
// embedded front-end for every other path.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/ask"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/materialize"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/stats"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/telemetry"
	"gitlab.com/rockt/cerne/internal/version"
	"gitlab.com/rockt/cerne/ui"
)

// New returns the http.Handler serving /api/* JSON endpoints and the embedded
// UI (SPA fallback to index.html) for everything else. It is the static,
// single-project handler with no background goroutines — used by tests. Live
// `cerne ui` uses NewUIMulti (uimulti.go), which adds per-project live refresh.
func New(repoDir, projDir, projectID string) http.Handler {
	return newMux(repoDir, projDir, projectID)
}

// newMux builds the single-project route table: the /api/* data routes plus the
// embedded SPA. The live SSE stream (GET /api/stream) is served only by the
// multi-project handler (NewUIMulti); this static mux has none.
func newMux(repoDir, projDir, projectID string) http.Handler {
	mux := http.NewServeMux()
	// Single-project binder: every request resolves to the one fixed project.
	bind := func(h scopedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { h(w, r, repoDir, projDir, projectID) }
	}
	mountDataRoutes(mux, "", bind, false)
	mountTelemetry(mux)
	mountPprof(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody("route not found"))
	})
	mux.Handle("/", spaHandler())
	return mux
}

// handleVersion returns the binary's build version (F-0129) — the same
// MAJOR.MINOR string `cerne --version` reports (F-0144), so the UI can show
// which build is serving it.
func handleVersion(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.Short()})
}

// handleSavings returns the context-savings metric (F-0127): full corpus vs
// current projection, per kind and total, computed by materialize.ComputeStats.
func handleSavings(w http.ResponseWriter, repoDir string) {
	s, err := materialize.ComputeStats(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// handleFlowStats returns flow metrics (F-0205): lead/cycle time per
// completed feature plus median/p90 and monthly throughput, computed purely
// from the event log.
func handleFlowStats(w http.ResponseWriter, repoDir string) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	m, err := stats.Compute(events, time.Time{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleGraph returns the deterministic entity relationship graph
// (nodes + edges) projected from the backend by app.BuildGraph.
func handleGraph(w http.ResponseWriter, repoDir string) {
	g, err := app.BuildGraph(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// stateResponse is the GET /api/state payload: the full backend state;
// filtering by feature state is the UI's job.
type stateResponse struct {
	Project      store.Project      `json:"project"`
	Features     []store.Feature    `json:"features"`
	Decisions    []store.Decision   `json:"decisions"`
	References   []store.Reference  `json:"references"`
	Runbooks     []store.Runbook    `json:"runbooks"`
	Notes        []store.Note       `json:"notes"`
	Capabilities []store.Capability `json:"capabilities"` // F-0188: the functional taxonomy
	// OpenClarifications counts the open [ESCLARECER]/[CLARIFY] markers per live
	// entity (F-0195), so the board can flag a card waiting on the human without
	// opening it. Derived from the docs on every request — never persisted. Only
	// entities with at least one open marker; always present, never null.
	OpenClarifications map[string]int `json:"open_clarifications"`
	// ActiveWork lists the active W-items referencing each feature (F-0202), so
	// the board can flag agent collisions without a CLI round-trip. Derived on
	// every request — never persisted. Only entities with active work; always
	// present, never null.
	ActiveWork map[string][]activeWorkItem `json:"active_work"`
}

// activeWorkItem is one active W-item referencing a feature: enough to badge
// the card and name who is working it.
type activeWorkItem struct {
	ID    string `json:"id"`
	Actor string `json:"actor"`
}

// activeWorkByRef groups every active work by each of its referenced entity
// ids. Best-effort by design, mirroring countOpenClarifications: a load
// failure degrades to an empty map instead of breaking the whole board.
func activeWorkByRef(repoDir string) map[string][]activeWorkItem {
	byRef := map[string][]activeWorkItem{}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return byRef
	}
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		for _, ref := range w.Refs {
			byRef[ref] = append(byRef[ref], activeWorkItem{ID: w.ID, Actor: w.Actor})
		}
	}
	return byRef
}

// countOpenClarifications tallies the open markers per live entity. Best-effort
// by design: the count is a secondary signal, so a scan failure degrades to an
// empty map instead of taking the whole board down with it.
func countOpenClarifications(repoDir string) map[string]int {
	open := map[string]int{}
	items, err := app.ListClarifications(repoDir, "")
	if err != nil {
		return open
	}
	for _, c := range items {
		open[c.Entity]++
	}
	return open
}

func handleState(w http.ResponseWriter, repoDir string) {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	capabilities, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if proj.Modules == nil {
		proj.Modules = []string{} // contract: modules is always a list
	}
	writeJSON(w, http.StatusOK, stateResponse{
		Project:            proj,
		Features:           features,
		Decisions:          decisions,
		References:         references,
		Runbooks:           runbooks,
		Notes:              notes,
		Capabilities:       capabilities,
		OpenClarifications: countOpenClarifications(repoDir),
		ActiveWork:         activeWorkByRef(repoDir),
	})
}

// workResponse is the GET /api/work payload (F-0130): every work regardless of
// state (active/done/abandoned) — the UI filters, same contract as GET /state.
type workResponse struct {
	Works []store.Work `json:"works"`
}

func handleWork(w http.ResponseWriter, repoDir string) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, workResponse{Works: works})
}

// featureDetailResponse is the GET /api/features/{id} payload.
type featureDetailResponse struct {
	Feature     store.Feature     `json:"feature"`
	Docs        featureDocs       `json:"docs"`
	DocsReal    featureDocReal    `json:"docs_real"`    // per-doc: carries real content beyond the scaffold (vs stub)
	DocsUpdated featureDocTimes   `json:"docs_updated"` // per-doc last-commit time; nil when uncommitted
	Artifacts   []artifact        `json:"artifacts"`
	InformedBy  []store.Reference `json:"informed_by"` // external sources that motivated this feature
	Children    []store.Feature   `json:"children"`    // features that decompose this one (epic view)
	Errata      string            `json:"errata"`      // additive corrections (F-0163); "" when none
	// SpecChangedAfterApproval is true when the spec was edited while approved,
	// after the last approval (F-0262) — the page warns the approval is stale.
	SpecChangedAfterApproval bool `json:"spec_changed_after_approval"`
	// RoastReport is the persisted adversarial spec critique (F-0271); ""
	// when no roast ran yet.
	RoastReport string `json:"roast_report,omitempty"`
}

type featureDocs struct {
	Spec   string `json:"spec"`
	Plan   string `json:"plan"`
	Result string `json:"result"`
}

// featureDocReal mirrors featureDocs with a per-document flag: true when the
// document carries real content beyond the scaffold cerne writes (frontmatter,
// headings, HTML-comment guidance). The UI shows "Vazio" for a false flag so a
// freshly scaffolded stub does not render its raw template guidance as content.
type featureDocReal struct {
	Spec   bool `json:"spec"`
	Plan   bool `json:"plan"`
	Result bool `json:"result"`
}

// featureDocTimes is the last-modified time of each lifecycle document, taken
// from the most recent backend commit that touched it (git log). A nil pointer
// means the document has no git history yet — never committed, or the backend
// is not a git repo — and serializes as null.
type featureDocTimes struct {
	Spec   *time.Time `json:"spec"`
	Plan   *time.Time `json:"plan"`
	Result *time.Time `json:"result"`
}

type artifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func handleFeatureDetail(w http.ResponseWriter, repoDir, id string) {
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	docs := featureDocs{}
	real := featureDocReal{}
	updated := featureDocTimes{}
	docFields := []struct {
		name    string
		content *string
		real    *bool
		updated **time.Time
	}{
		{"spec.md", &docs.Spec, &real.Spec, &updated.Spec},
		{"plan.md", &docs.Plan, &real.Plan, &updated.Plan},
		{"result.md", &docs.Result, &real.Result, &updated.Result},
	}
	var docPaths []string
	for _, d := range docFields {
		data, err := os.ReadFile(app.FeatureDocPath(repoDir, id, d.name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		*d.content = string(data)
		*d.real = doccheck.HasRealContent(data)
		if len(data) > 0 {
			docPaths = append(docPaths, "content/features/"+id+"/"+d.name)
		}
	}
	// A single history walk resolves the last-updated time of every present
	// doc. Best-effort: a non-git backend (tests) or any git error simply omits
	// the timestamps — the document content is what the page needs.
	if len(docPaths) > 0 {
		if times, err := gitback.LastCommitTimes(repoDir, docPaths); err == nil {
			for _, d := range docFields {
				if t, ok := times["content/features/"+id+"/"+d.name]; ok {
					t := t
					*d.updated = &t
				}
			}
		}
	}
	artifacts, err := listArtifacts(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	informedBy, err := informedByRefs(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	all, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	children := store.ChildrenByParent(all)[id]
	if children == nil {
		children = []store.Feature{}
	}
	errata, err := store.LoadErrata(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	specChanged := false
	if f.State == domain.StateApproved {
		events, err := store.ReadEvents(repoDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		specChanged = lint.SpecChangedAfterApproval(events, id)
	}
	roast, err := roastReportFor(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, featureDetailResponse{Feature: f, Docs: docs, DocsReal: real, DocsUpdated: updated, Artifacts: artifacts, InformedBy: informedBy, Children: children, Errata: errata, SpecChangedAfterApproval: specChanged, RoastReport: roast})
}

// informedByRefs returns the external references whose Informs backlink targets
// entityID, as a non-nil slice (so the JSON is [] not null).
func informedByRefs(repoDir, entityID string) ([]store.Reference, error) {
	refs, err := app.ReferencesInforming(repoDir, entityID)
	if err != nil {
		return nil, err
	}
	if refs == nil {
		refs = []store.Reference{}
	}
	return refs, nil
}

// listArtifacts lists content/artifacts/<id>/ files (dotfiles like .gitkeep
// excluded); a missing directory yields an empty list.
func listArtifacts(repoDir, id string) ([]artifact, error) {
	artifacts := []artifact{}
	dirEntries, err := os.ReadDir(filepath.Join(repoDir, "content", "artifacts", id))
	if errors.Is(err, os.ErrNotExist) {
		return artifacts, nil
	}
	if err != nil {
		return nil, err
	}
	for _, de := range dirEntries {
		if de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		artifacts = append(artifacts, artifact{
			Name: de.Name(),
			Path: "content/artifacts/" + id + "/" + de.Name(),
		})
	}
	return artifacts, nil
}

// decisionDetailResponse is the GET /api/decisions/{id} payload: the entity
// plus its content document (empty when the doc file is absent).
type decisionDetailResponse struct {
	Decision   store.Decision    `json:"decision"`
	Doc        string            `json:"doc"`
	InformedBy []store.Reference `json:"informed_by"` // external sources that motivated this decision
	Errata     string            `json:"errata"`      // additive corrections (F-0163); "" when none
}

func handleDecisionDetail(w http.ResponseWriter, repoDir, id string) {
	d, err := store.LoadDecision(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("decision not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	doc, err := readContentDoc(repoDir, "decisions", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	informedBy, err := informedByRefs(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	errata, err := store.LoadErrata(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, decisionDetailResponse{Decision: d, Doc: doc, InformedBy: informedBy, Errata: errata})
}

// referenceDetailResponse is the GET /api/references/{id} payload.
type referenceDetailResponse struct {
	Reference store.Reference `json:"reference"`
	Doc       string          `json:"doc"`
}

func handleReferenceDetail(w http.ResponseWriter, repoDir, id string) {
	ref, err := store.LoadReference(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("reference not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	doc, err := readContentDoc(repoDir, "references", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, referenceDetailResponse{Reference: ref, Doc: doc})
}

// runbookDetailResponse is the GET /api/runbooks/{id} payload.
type runbookDetailResponse struct {
	Runbook store.Runbook `json:"runbook"`
	Doc     string        `json:"doc"`
	Errata  string        `json:"errata"` // additive corrections (F-0163); "" when none
}

func handleRunbookDetail(w http.ResponseWriter, repoDir, id string) {
	rb, err := store.LoadRunbook(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("runbook not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	doc, err := readContentDoc(repoDir, "runbooks", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	errata, err := store.LoadErrata(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, runbookDetailResponse{Runbook: rb, Doc: doc, Errata: errata})
}

// noteDetailResponse is the GET /api/notes/{id} payload.
type noteDetailResponse struct {
	Note   store.Note `json:"note"`
	Doc    string     `json:"doc"`
	Errata string     `json:"errata"` // additive corrections (F-0163); "" when none
}

func handleNoteDetail(w http.ResponseWriter, repoDir, id string) {
	n, err := store.LoadNote(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("note not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	doc, err := readContentDoc(repoDir, "notes", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	errata, err := store.LoadErrata(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, noteDetailResponse{Note: n, Doc: doc, Errata: errata})
}

// readContentDoc reads content/<kind>/<id>.md (decisions, references, runbooks,
// notes); "" when the file is absent, mirroring how the materializer and index
// treat a missing content document.
func readContentDoc(repoDir, kind, id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", kind, id+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// searchHit is one full-text search result in the API's snake_case contract,
// projected from index.Result (which carries no JSON tags).
type searchHit struct {
	EntityID     string `json:"entity_id"`
	Kind         string `json:"kind"`
	Doc          string `json:"doc"`
	State        string `json:"state"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

// searchResponse is the GET /api/search payload.
type searchResponse struct {
	Results []searchHit `json:"results"`
}

// ensureIndex resolves the project's FTS index path and builds it on first
// use. Offline by design: it reads the cached clone, never the remote —
// 'cerne pull' is the sync point (mirrors searchcmd). Shared by search, ask
// and the chat turn priming (F-0253).
func ensureIndex(repoDir, projectID string) (string, error) {
	dbPath, err := project.IndexPath(projectID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		if err := index.Rebuild(repoDir, dbPath); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return dbPath, nil
}

func handleSearch(w http.ResponseWriter, r *http.Request, repoDir, projectID string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("provide the search term (parameter q)"))
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	dbPath, err := ensureIndex(repoDir, projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	results, err := index.Search(dbPath, query, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	hits := make([]searchHit, len(results))
	for i, res := range results {
		hits[i] = searchHit{
			EntityID:     res.EntityID,
			Kind:         res.Kind,
			Doc:          res.Doc,
			State:        res.State,
			Title:        res.Title,
			Snippet:      res.Snippet,
			SupersededBy: res.SupersededBy,
		}
	}
	writeJSON(w, http.StatusOK, searchResponse{Results: hits})
}

// askRequest is the POST /api/ask body: a natural-language question and
// whether to dive into superseded/rejected/obsolete history.
type askRequest struct {
	Query   string `json:"query"`
	History bool   `json:"history"`
}

// askSource is one retrieved context source in the API's snake_case contract,
// projected from ask.Snippet (the body text is dropped; the UI links by source).
type askSource struct {
	Source string `json:"source"`        // entity ID, or "wiki:<rel>" for a compiled page
	Kind   string `json:"kind"`          // feature | decision | reference | wiki
	State  string `json:"state"`         // lifecycle state; "current" for wiki pages
	URL    string `json:"url,omitempty"` // source URL for external references
}

// askResponse is the POST /api/ask payload. Answered is false in delegation
// mode (no provider to answer for the human): the sources are still returned,
// and never a fabricated answer.
type askResponse struct {
	Answered      bool        `json:"answered"`
	NeedsProvider bool        `json:"needs_provider,omitempty"`
	Answer        string      `json:"answer,omitempty"`
	Sources       []askSource `json:"sources"`
}

// handleAsk answers a natural-language question over the wiki for a human in
// the browser. Retrieval mirrors handleSearch (same offline index). The config
// is loaded per-request from projDir so editing .cernerc takes effect without
// restarting `cerne ui`; delegation (the agent's mode) can't answer here, so
// the endpoint returns the sources with needs_provider instead of inventing one.
func handleAsk(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("provide the question (field query)"))
		return
	}
	dbPath, err := ensureIndex(repoDir, projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	snips, err := ask.Retrieve(repoDir, dbPath, query, req.History, 10)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	sources := make([]askSource, len(snips))
	for i, s := range snips {
		sources[i] = askSource{Source: s.Source, Kind: s.Kind, State: s.State}
	}

	// Load the config per-request: delegation (or no .cernerc) means there is no
	// provider to answer for the human — return the sources, not a fabrication.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, askResponse{Answered: false, NeedsProvider: true, Sources: sources})
		return
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	answer, err := prov.Complete(r.Context(), ask.BuildAskPrompt(query, snips, true))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, askResponse{Answered: true, Answer: answer, Sources: sources})
}

// generateDiagramRequest is the POST /api/features/{id}/diagram body: an
// optional artifact name (default "sequence", normalized to <name>.mmd).
type generateDiagramRequest struct {
	Name string `json:"name"`
}

// generateDiagramResponse is the POST /api/features/{id}/diagram payload.
// Generated is false in delegation mode (no provider to synthesize for the
// human in the browser): the UI then hints to switch the AI mode, and the
// endpoint never fabricates a diagram.
type generateDiagramResponse struct {
	Generated     bool   `json:"generated"`
	NeedsProvider bool   `json:"needs_provider,omitempty"`
	Name          string `json:"name,omitempty"`
	Mermaid       string `json:"mermaid,omitempty"`
}

// handleGenerateDiagram AI-drafts a Mermaid sequence diagram for a feature on
// behalf of a human in the browser — the web counterpart of `cerne spec
// diagram`. It mirrors handleAsk: the config is loaded per-request from projDir
// (so changing the AI mode in the UI takes effect without restarting), and
// delegation — the agent's mode, with no provider to synthesize here —
// degrades to needs_provider instead of inventing a diagram. On success it
// saves <name>.mmd to the backend (commit + push) and returns the Mermaid text
// so the UI can convert it to an editable Excalidraw scene without a round-trip.
func handleGenerateDiagram(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req generateDiagramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	name := app.MermaidArtifactName(req.Name)

	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	switch f.State {
	case domain.StateDone, domain.StateRejected, domain.StateSuperseded:
		writeJSON(w, http.StatusConflict, errorBody("feature is terminal — nothing to diagram"))
		return
	}

	// The current spec grounds the diagram; absent or scaffold-only means there
	// is no context to draw from — refuse instead of inventing (mirrors
	// handleRoast, and fires before the provider/mode question).
	specBytes, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusConflict, errorBody("feature has no spec to ground the diagram"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if !doccheck.HasRealContent(specBytes) {
		writeJSON(w, http.StatusConflict, errorBody("feature has no spec to ground the diagram"))
		return
	}

	// Load the config per-request: delegation (or no .cernerc) means there is no
	// provider to synthesize for the human — signal needs_provider, write nothing.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, generateDiagramResponse{Generated: false, NeedsProvider: true})
		return
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	// Project structure grounds the diagram in real components (best-effort).
	structure, _ := app.ProjectStructure(projDir)
	// The provider call runs under the agent chat budget: r.Context() alone
	// lives as long as the caller waits, so a hung provider would hang the
	// request forever.
	ctx, cancel := context.WithTimeout(r.Context(), cfg.Agent.ChatTimeoutOrDefault())
	defer cancel()
	body, err := prov.Complete(ctx, app.BuildSequenceDiagramPrompt(f, string(specBytes), structure))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	mermaid, err := app.SanitizeMermaid(body)
	if err != nil {
		if errors.Is(err, app.ErrNotMermaidSequence) {
			writeJSON(w, http.StatusUnprocessableEntity,
				errorBody("the model did not return a Mermaid sequence diagram — try again"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if err := app.SaveArtifact(repoDir, projDir, id, name, []byte(mermaid+"\n")); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, generateDiagramResponse{Generated: true, Name: name, Mermaid: mermaid})
}

// wikiPageRef is one compiled wiki page in the index: its canonical name and
// the rel used to fetch it (GET /api/wiki/<rel>).
type wikiPageRef struct {
	Name string `json:"name"`
	Rel  string `json:"rel"`
}

// wikiIndexResponse is the GET /api/wiki payload: the compiled pages that exist
// on disk, so the human browses what `cerne compile` actually produced.
type wikiIndexResponse struct {
	Pages []wikiPageRef `json:"pages"`
}

// wikiPageResponse is the GET /api/wiki/<rel> payload: the synthesis body with
// the provenance frontmatter stripped.
type wikiPageResponse struct {
	Rel  string `json:"rel"`
	Body string `json:"body"`
}

// safeWikiRel guards path traversal for serving wiki pages by relative path:
// non-empty, no "..", no backslash, no absolute or hidden segment. Allows a
// single "modules/" nesting (e.g. "modules/finance").
func safeWikiRel(rel string) bool {
	if rel == "" || strings.Contains(rel, "..") || strings.ContainsRune(rel, '\\') || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// handleWikiIndex lists the compiled pages under content/wiki/ — the top-level
// pages (project/decisions/references) and the nested modules/ pages — so the
// human can browse the synthesis. An absent wiki dir is an empty list, not an
// error.
func handleWikiIndex(w http.ResponseWriter, repoDir string) {
	wikiDir := filepath.Join(repoDir, "content", "wiki")
	pages := []wikiPageRef{}
	entries, err := os.ReadDir(wikiDir)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, wikiIndexResponse{Pages: pages})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name != "modules" {
				continue
			}
			mods, err := os.ReadDir(filepath.Join(wikiDir, "modules"))
			if err != nil {
				continue
			}
			for _, m := range mods {
				if !m.IsDir() && strings.HasSuffix(m.Name(), ".md") {
					base := strings.TrimSuffix(m.Name(), ".md")
					pages = append(pages, wikiPageRef{Name: base, Rel: "modules/" + base})
				}
			}
			continue
		}
		if strings.HasSuffix(name, ".md") {
			base := strings.TrimSuffix(name, ".md")
			pages = append(pages, wikiPageRef{Name: base, Rel: base})
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Rel < pages[j].Rel })
	writeJSON(w, http.StatusOK, wikiIndexResponse{Pages: pages})
}

// handleWikiPage serves one compiled page body (frontmatter stripped) by its
// relative path, guarding traversal via safeWikiRel.
func handleWikiPage(w http.ResponseWriter, repoDir, rel string) {
	if !safeWikiRel(rel) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid page path"))
		return
	}
	body, ok := ask.ReadWikiPage(repoDir, rel)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorBody("wiki page not found"))
		return
	}
	writeJSON(w, http.StatusOK, wikiPageResponse{Rel: rel, Body: body})
}

// lintFinding is one lint problem in the API's snake_case contract,
// projected from lint.Finding (which carries no JSON tags).
type lintFinding struct {
	Rule   string `json:"rule"`
	Entity string `json:"entity,omitempty"`
	File   string `json:"file,omitempty"`
	Detail string `json:"detail"`
}

// lintResponse is the GET /api/lint payload; findings come already ordered
// by lint.RuleOrder. Stale marks findings served from a superseded cache
// while the background revalidation runs (F-0155) — the SSE change event
// announces the fresh pass, so a stale response is always transient.
type lintResponse struct {
	Findings []lintFinding `json:"findings"`
	Stale    bool          `json:"stale"`
}

func handleLint(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
	var p lintParams
	if cfg, _, err := project.LoadConfig(projDir); err == nil {
		p = lintParams{
			staleReadyDays: cfg.StaleReadyWindowDays(),
			staleWorkHours: cfg.StaleWorkWindowHours(),
			ignore:         cfg.UnregisteredSourceIgnore,
		}
	}
	// Stale-while-revalidate per backend HEAD (F-0153/F-0155): the request
	// never waits for a pass when any cache exists; histograms measure only
	// real passes (inside lintFor).
	findings, stale, err := lintFor(telemetry.OpFromContext(r.Context()), projectID, repoDir, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	out := make([]lintFinding, len(findings))
	for i, f := range findings {
		out[i] = lintFinding{Rule: f.Rule, Entity: f.Entity, File: f.File, Detail: f.Detail}
	}
	writeJSON(w, http.StatusOK, lintResponse{Findings: out, Stale: stale})
}

// eventsResponse is the GET /api/events payload: the lifecycle event log,
// newest first for a timeline view.
type eventsResponse struct {
	Events []domain.Event `json:"events"`
}

func handleEvents(w http.ResponseWriter, repoDir string) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	// ReadEvents yields chronological order; reverse to newest-first.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: events})
}

// createFeatureRequest is the POST /api/features body.
type createFeatureRequest struct {
	Title      string   `json:"title"`
	Module     string   `json:"module"`
	Parent     string   `json:"parent"`
	Facets     []string `json:"facets"`     // closed-set axis (D-0002/D-0003); unknown facet → 422
	Components []string `json:"components"` // curated subsystem/tool axis (D-0002/D-0003)
}

// createFeatureResponse is the POST /api/features 201 payload.
type createFeatureResponse struct {
	Feature store.Feature `json:"feature"`
}

func handleCreateFeature(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req createFeatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	f, err := app.CreateFeature(repoDir, projDir, req.Title, req.Module, req.Parent, req.Facets, req.Components)
	if errors.Is(err, app.ErrSimilarExists) {
		// Duplicate-registration guard (F-0147): the human sees the same
		// match list the CLI prints. 409: the conflict is with existing state.
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
		return
	}
	if err != nil {
		// Validation/module failures surface as 422 with the same
		// message the CLI prints, matching handleTransition.
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, createFeatureResponse{Feature: f})
}

// createReferenceRequest is the POST /api/references body: an external source
// (article/doc) with optional informs backlinks. No secret travels here.
type createReferenceRequest struct {
	Title      string   `json:"title"`
	URL        string   `json:"url"`
	Content    string   `json:"content"`
	Informs    []string `json:"informs"`
	Facets     []string `json:"facets"`     // closed-set axis (D-0002); unknown facet → 422
	Components []string `json:"components"` // curated subsystem/tool axis (D-0002)
}

// createReferenceResponse is the POST /api/references 201 payload.
type createReferenceResponse struct {
	Reference store.Reference `json:"reference"`
}

func handleCreateReference(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req createReferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	ref, err := app.CreateExternalReference(repoDir, projDir, req.Title, req.URL, req.Content, req.Informs, req.Facets, req.Components)
	if err != nil {
		// Validation (empty url, dangling informs, unknown facet) surfaces as 422, like features.
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, createReferenceResponse{Reference: ref})
}

func handleFeatureDocPut(w http.ResponseWriter, r *http.Request, repoDir, projDir, id, doc string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	switch err := app.SaveFeatureDoc(repoDir, projDir, id, doc, string(body)); {
	case errors.Is(err, app.ErrUnknownDoc):
		writeJSON(w, http.StatusBadRequest, errorBody("invalid document (use spec, plan or result)"))
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	}
}

// clarifyAnswerRequest is the POST /api/clarify/{id}/answer body: the exact
// anchor of ONE open marker (backend-relative doc path, byte offset, raw
// marker text — what the UI chip already carries) plus the answer.
type clarifyAnswerRequest struct {
	Doc    string `json:"doc"`
	Offset int    `json:"offset"`
	Raw    string `json:"raw"`
	Answer string `json:"answer"`
}

// handleClarifyAnswer resolves one open [ESCLARECER]/[CLARIFY] marker through
// the same core as `cerne clarify answer` (F-0193). 409 when the anchor no
// longer matches (doc advanced under the reader — reload), 404 for an unknown
// entity, 422 for a terminal one (history is answered via errata, not here).
func handleClarifyAnswer(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req clarifyAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if strings.TrimSpace(req.Answer) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("answer must not be empty"))
		return
	}
	ans, err := app.AnswerClarificationAt(repoDir, projDir, id, req.Doc, req.Offset, req.Raw, req.Answer)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("entity not found"))
	case errors.Is(err, app.ErrEntityNotLive):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("entity is not live — append an errata instead"))
	case errors.Is(err, app.ErrClarificationGone):
		writeJSON(w, http.StatusConflict, errorBody("clarification not found at the given anchor — reload the document"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, ans)
	}
}

// handleClarifyCorrect is the correction twin of handleClarifyAnswer (F-0197):
// same anchored body, same error mapping — it only targets an already answered
// marker and appends to its trail instead of resolving an open one.
func handleClarifyCorrect(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req clarifyAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if strings.TrimSpace(req.Answer) == "" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("answer must not be empty"))
		return
	}
	ans, err := app.CorrectClarificationAt(repoDir, projDir, id, req.Doc, req.Offset, req.Raw, req.Answer)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("entity not found"))
	case errors.Is(err, app.ErrEntityNotLive):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("entity is not live — append an errata instead"))
	case errors.Is(err, app.ErrClarificationGone):
		writeJSON(w, http.StatusConflict, errorBody("clarification not found at the given anchor — reload the document"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, ans)
	}
}

// setParentRequest is the PUT /api/features/{id}/parent body; an empty parent
// detaches the feature from its epic.
type setParentRequest struct {
	Parent string `json:"parent"`
}

func handleSetParent(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req setParentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	switch err := app.SetFeatureParent(repoDir, projDir, id, req.Parent); {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
	case errors.Is(err, app.ErrParentNotFound),
		errors.Is(err, app.ErrParentTerminal),
		errors.Is(err, domain.ErrParentSelf),
		errors.Is(err, domain.ErrParentCycle):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, createFeatureResponse{Feature: f})
	}
}

// renameRequest is the PUT /api/features/{id}/title body.
type renameRequest struct {
	Title string `json:"title"`
}

func handleRenameTitle(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req renameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	switch err := app.RenameFeature(repoDir, projDir, id, req.Title); {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
	case errors.Is(err, app.ErrTitleFrozen):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	case err != nil && strings.TrimSpace(req.Title) == "":
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, createFeatureResponse{Feature: f})
	}
}

// transitionRequest is the POST /api/features/{id}/transition body.
type transitionRequest struct {
	Action string `json:"action"`
	By     string `json:"by"`
	Reason string `json:"reason"`
}

func handleTransition(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	var req transitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	out, err := app.TransitionFeatureOp(telemetry.OpFromContext(r.Context()), repoDir, projDir, id, domain.Action(req.Action), req.By, req.Reason, nil)
	if err != nil {
		// Domain, gate and validation failures surface as 422 with the
		// same message the CLI prints.
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":   out.ID,
		"from": string(out.From),
		"to":   string(out.To),
	})
}

// spaHandler serves the embedded ui/dist tree; any path that is not an
// existing file falls back to index.html (SPA routing).
func spaHandler() http.Handler {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic(err) // embed layout is fixed at build time
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if f, err := dist.Open(name); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, dist, "index.html")
	})
}

func errorBody(msg string) map[string]string {
	return map[string]string{"error": msg}
}

// writeJSON writes v as the JSON response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
