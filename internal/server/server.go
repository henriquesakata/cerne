// Package server exposes the cerne local UI: JSON API under /api plus the
// embedded front-end for every other path.
package server

import (
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

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/ask"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/ui"
)

// New returns the http.Handler serving /api/* JSON endpoints and the
// embedded UI (SPA fallback to index.html) for everything else.
func New(repoDir, projDir, projectID string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		handleState(w, repoDir)
	})
	mux.HandleFunc("GET /api/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleFeatureDetail(w, repoDir, r.PathValue("id"))
	})
	mux.HandleFunc("POST /api/features", func(w http.ResponseWriter, r *http.Request) {
		handleCreateFeature(w, r, repoDir, projDir)
	})
	mux.HandleFunc("POST /api/features/{id}/transition", func(w http.ResponseWriter, r *http.Request) {
		handleTransition(w, r, repoDir, projDir, r.PathValue("id"))
	})
	mux.HandleFunc("PUT /api/features/{id}/docs/{doc}", func(w http.ResponseWriter, r *http.Request) {
		handleFeatureDocPut(w, r, repoDir, projDir, r.PathValue("id"), r.PathValue("doc"))
	})
	mux.HandleFunc("GET /api/artifacts/{featureID}/{name}", func(w http.ResponseWriter, r *http.Request) {
		handleArtifactGet(w, repoDir, r.PathValue("featureID"), r.PathValue("name"))
	})
	mux.HandleFunc("PUT /api/artifacts/{featureID}/{name}", func(w http.ResponseWriter, r *http.Request) {
		handleArtifactPut(w, r, repoDir, projDir, r.PathValue("featureID"), r.PathValue("name"))
	})
	mux.HandleFunc("GET /api/decisions/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleDecisionDetail(w, repoDir, r.PathValue("id"))
	})
	mux.HandleFunc("GET /api/references/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleReferenceDetail(w, repoDir, r.PathValue("id"))
	})
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		handleSearch(w, r, repoDir, projectID)
	})
	mux.HandleFunc("POST /api/ask", func(w http.ResponseWriter, r *http.Request) {
		handleAsk(w, r, repoDir, projDir, projectID)
	})
	mux.HandleFunc("GET /api/config/llm", func(w http.ResponseWriter, r *http.Request) {
		handleLLMConfigGet(w, projDir)
	})
	mux.HandleFunc("PUT /api/config/llm", func(w http.ResponseWriter, r *http.Request) {
		handleLLMConfigPut(w, r, projDir)
	})
	mux.HandleFunc("GET /api/wiki", func(w http.ResponseWriter, r *http.Request) {
		handleWikiIndex(w, repoDir)
	})
	mux.HandleFunc("GET /api/wiki/{rel...}", func(w http.ResponseWriter, r *http.Request) {
		handleWikiPage(w, repoDir, r.PathValue("rel"))
	})
	mux.HandleFunc("GET /api/lint", func(w http.ResponseWriter, r *http.Request) {
		handleLint(w, repoDir)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		handleEvents(w, repoDir)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody("rota não encontrada"))
	})
	mux.Handle("/", spaHandler())
	return mux
}

// stateResponse is the GET /api/state payload: the full backend state;
// filtering by feature state is the UI's job.
type stateResponse struct {
	Project    store.Project     `json:"project"`
	Features   []store.Feature   `json:"features"`
	Decisions  []store.Decision  `json:"decisions"`
	References []store.Reference `json:"references"`
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
	if proj.Modules == nil {
		proj.Modules = []string{} // contract: modules is always a list
	}
	writeJSON(w, http.StatusOK, stateResponse{
		Project:    proj,
		Features:   features,
		Decisions:  decisions,
		References: references,
	})
}

// featureDetailResponse is the GET /api/features/{id} payload.
type featureDetailResponse struct {
	Feature   store.Feature `json:"feature"`
	Docs      featureDocs   `json:"docs"`
	Artifacts []artifact    `json:"artifacts"`
}

type featureDocs struct {
	Spec   string `json:"spec"`
	Plan   string `json:"plan"`
	Result string `json:"result"`
}

type artifact struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func handleFeatureDetail(w http.ResponseWriter, repoDir, id string) {
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature não encontrada"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	docs := featureDocs{}
	for name, dst := range map[string]*string{
		"spec.md":   &docs.Spec,
		"plan.md":   &docs.Plan,
		"result.md": &docs.Result,
	} {
		data, err := os.ReadFile(app.FeatureDocPath(repoDir, id, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		*dst = string(data)
	}
	artifacts, err := listArtifacts(repoDir, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, featureDetailResponse{Feature: f, Docs: docs, Artifacts: artifacts})
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
	Decision store.Decision `json:"decision"`
	Doc      string         `json:"doc"`
}

func handleDecisionDetail(w http.ResponseWriter, repoDir, id string) {
	d, err := store.LoadDecision(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("decisão não encontrada"))
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
	writeJSON(w, http.StatusOK, decisionDetailResponse{Decision: d, Doc: doc})
}

// referenceDetailResponse is the GET /api/references/{id} payload.
type referenceDetailResponse struct {
	Reference store.Reference `json:"reference"`
	Doc       string          `json:"doc"`
}

func handleReferenceDetail(w http.ResponseWriter, repoDir, id string) {
	ref, err := store.LoadReference(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("referência não encontrada"))
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

// readContentDoc reads content/<kind>/<id>.md (decisions, references); ""
// when the file is absent, mirroring how the materializer and index treat a
// missing content document.
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

func handleSearch(w http.ResponseWriter, r *http.Request, repoDir, projectID string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("informe o termo de busca (parâmetro q)"))
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	dbPath, err := project.IndexPath(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	// Offline by design: build the index on first use from the cached clone,
	// never the remote — 'cerne pull' is the sync point (mirrors searchcmd).
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		if err := index.Rebuild(repoDir, dbPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
	} else if err != nil {
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
	Source string `json:"source"` // entity ID, or "wiki:<rel>" for a compiled page
	Kind   string `json:"kind"`   // feature | decision | reference | wiki
	State  string `json:"state"`  // lifecycle state; "current" for wiki pages
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
		writeJSON(w, http.StatusBadRequest, errorBody("corpo inválido: "+err.Error()))
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("informe a pergunta (campo query)"))
		return
	}
	dbPath, err := project.IndexPath(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	// Offline by design: build the index on first use from the cached clone
	// (mirrors handleSearch); 'cerne pull' is the sync point.
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		if err := index.Rebuild(repoDir, dbPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
	} else if err != nil {
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
	answer, err := prov.Complete(r.Context(), ask.BuildAskPrompt(query, snips))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, askResponse{Answered: true, Answer: answer, Sources: sources})
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
		writeJSON(w, http.StatusBadRequest, errorBody("caminho de página inválido"))
		return
	}
	body, ok := ask.ReadWikiPage(repoDir, rel)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorBody("página de wiki não encontrada"))
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
// by lint.RuleOrder.
type lintResponse struct {
	Findings []lintFinding `json:"findings"`
}

func handleLint(w http.ResponseWriter, repoDir string) {
	findings, err := lint.Run(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	out := make([]lintFinding, len(findings))
	for i, f := range findings {
		out[i] = lintFinding{Rule: f.Rule, Entity: f.Entity, File: f.File, Detail: f.Detail}
	}
	writeJSON(w, http.StatusOK, lintResponse{Findings: out})
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
	Title  string `json:"title"`
	Module string `json:"module"`
}

// createFeatureResponse is the POST /api/features 201 payload.
type createFeatureResponse struct {
	Feature store.Feature `json:"feature"`
}

func handleCreateFeature(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req createFeatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("corpo da requisição inválido"))
		return
	}
	f, err := app.CreateFeature(repoDir, projDir, req.Title, req.Module)
	if err != nil {
		// Validation/module failures surface as 422 with the same pt-BR
		// message the CLI prints, matching handleTransition.
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, createFeatureResponse{Feature: f})
}

func handleFeatureDocPut(w http.ResponseWriter, r *http.Request, repoDir, projDir, id, doc string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("corpo da requisição inválido"))
		return
	}
	switch err := app.SaveFeatureDoc(repoDir, projDir, id, doc, string(body)); {
	case errors.Is(err, app.ErrUnknownDoc):
		writeJSON(w, http.StatusBadRequest, errorBody("documento inválido (use spec, plan ou result)"))
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature não encontrada"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
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
		writeJSON(w, http.StatusBadRequest, errorBody("corpo da requisição inválido"))
		return
	}
	if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature não encontrada"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	out, err := app.TransitionFeature(repoDir, projDir, id, domain.Action(req.Action), req.By, req.Reason)
	if err != nil {
		// Domain, gate and validation failures surface as 422 with the
		// same pt-BR message the CLI prints.
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
