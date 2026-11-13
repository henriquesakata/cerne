package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
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
// scene JSON, .mmd files are Mermaid text; anything else falls back to a
// generic byte stream.
func handleArtifactGet(w http.ResponseWriter, repoDir, featureID, name string) {
	if !safeSegment(featureID) || !safeSegment(name) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid artifact name"))
		return
	}
	data, err := os.ReadFile(artifactPath(repoDir, featureID, name))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, errorBody("artifact not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	switch {
	case strings.HasSuffix(name, ".excalidraw"):
		w.Header().Set("Content-Type", "application/json")
	case strings.HasSuffix(name, ".mmd"):
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleArtifactPut creates or updates a .mmd Mermaid artifact. It validates
// the name and body, writes the file into the backend clone and syncs it
// (commit + push + docs.lock snapshot) via app.SaveArtifact. .excalidraw is
// refused: the panel no longer edits it (F-0400), though GET still serves it.
func handleArtifactPut(w http.ResponseWriter, r *http.Request, repoDir, projDir, featureID, name string) {
	if strings.HasSuffix(name, ".excalidraw") {
		writeJSON(w, http.StatusBadRequest,
			errorBody("excalidraw artifacts are no longer editable; only .mmd can be saved"))
		return
	}
	if !safeSegment(featureID) || !safeSegment(name) || !strings.HasSuffix(name, ".mmd") {
		writeJSON(w, http.StatusBadRequest,
			errorBody("invalid artifact name (expected a single segment ending in .mmd)"))
		return
	}
	if _, err := store.LoadFeature(repoDir, featureID); errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
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
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody(".mmd artifact body cannot be empty"))
		return
	}
	if err := app.SaveArtifact(repoDir, projDir, featureID, name, body); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
