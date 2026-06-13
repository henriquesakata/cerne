package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// safeSegment reports whether s is usable as a single path segment under
// content/artifacts/: non-empty, no separators, no traversal, not hidden.
func safeSegment(s string) bool {
	return s != "" && s != "." && s != ".." &&
		!strings.ContainsAny(s, `/\`) && !strings.HasPrefix(s, ".")
}

func artifactPath(repoDir, featureID, name string) string {
	return filepath.Join(repoDir, "content", "artifacts", featureID, name)
}

// handleArtifactGet serves the raw artifact file. .excalidraw files are
// scene JSON; anything else falls back to a generic byte stream.
func handleArtifactGet(w http.ResponseWriter, repoDir, featureID, name string) {
	if !safeSegment(featureID) || !safeSegment(name) {
		writeJSON(w, http.StatusBadRequest, errorBody("nome de artefato inválido"))
		return
	}
	data, err := os.ReadFile(artifactPath(repoDir, featureID, name))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, errorBody("artefato não encontrado"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if strings.HasSuffix(name, ".excalidraw") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleArtifactPut creates or updates an .excalidraw artifact: validates
// name and body, writes the file into the backend clone and syncs it
// (commit + push + docs.lock snapshot) via app.SaveArtifact.
func handleArtifactPut(w http.ResponseWriter, r *http.Request, repoDir, projDir, featureID, name string) {
	if !safeSegment(featureID) || !safeSegment(name) || !strings.HasSuffix(name, ".excalidraw") {
		writeJSON(w, http.StatusBadRequest,
			errorBody("nome de artefato inválido (esperado um único segmento terminado em .excalidraw)"))
		return
	}
	if _, err := store.LoadFeature(repoDir, featureID); errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature não encontrada"))
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if !json.Valid(body) {
		writeJSON(w, http.StatusBadRequest, errorBody("corpo da requisição não é JSON válido"))
		return
	}
	if err := app.SaveArtifact(repoDir, projDir, featureID, name, body); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
