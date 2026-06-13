// Package server exposes the cerne local UI: JSON API under /api plus the
// embedded front-end for every other path.
package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/ui"
)

// New returns the http.Handler serving /api/* JSON endpoints and the
// embedded UI (SPA fallback to index.html) for everything else.
func New(repoDir, projDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		handleState(w, repoDir)
	})
	mux.HandleFunc("GET /api/features/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleFeatureDetail(w, repoDir, r.PathValue("id"))
	})
	mux.HandleFunc("POST /api/features/{id}/transition", func(w http.ResponseWriter, r *http.Request) {
		handleTransition(w, r, repoDir, projDir, r.PathValue("id"))
	})
	mux.HandleFunc("GET /api/artifacts/{featureID}/{name}", func(w http.ResponseWriter, r *http.Request) {
		handleArtifactGet(w, repoDir, r.PathValue("featureID"), r.PathValue("name"))
	})
	mux.HandleFunc("PUT /api/artifacts/{featureID}/{name}", func(w http.ResponseWriter, r *http.Request) {
		handleArtifactPut(w, r, repoDir, projDir, r.PathValue("featureID"), r.PathValue("name"))
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
