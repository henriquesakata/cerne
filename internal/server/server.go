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
	"strconv"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/index"
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
