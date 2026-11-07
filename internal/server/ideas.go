package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// Ideas API (F-0312): thin handlers over app.*Idea — the same functions the
// CLI calls (D-0007), so both surfaces share one contract.

type ideaResponse struct {
	Idea store.Idea `json:"idea"`
}

func handleIdeas(w http.ResponseWriter, repoDir string) {
	ideas, err := app.ListIdeas(repoDir, false, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Ideas []store.Idea `json:"ideas"`
	}{ideas})
}

func handleCreateIdea(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) { // C5: a write route, same guard as the chat (F-0323 C2)
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Text       string   `json:"text"`
		Facets     []string `json:"facets"`
		Components []string `json:"components"`
		Token      string   `json:"token"` // C2: per submission, reused by "Tentar de novo"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	i, err := app.CreateIdea(repoDir, projDir, req.Text, req.Facets, req.Components, req.Token)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error())) // blank text, unknown facet
		return
	}
	writeJSON(w, http.StatusCreated, ideaResponse{i})
}

func handleDropIdea(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	if !sameOriginJSON(r) { // C5: a write route, same guard as the chat (F-0323 C2)
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	i, err := app.DropIdea(repoDir, projDir, id, req.Reason)
	if err != nil {
		writeIdeaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ideaResponse{i})
}

func handlePromoteIdea(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	if !sameOriginJSON(r) { // C5: a write route, same guard as the chat (F-0323 C2)
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Module string `json:"module"`
		Force  bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	i, f, err := app.PromoteIdea(repoDir, projDir, id, "", req.Module, req.Force)
	if err != nil {
		writeIdeaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Idea    store.Idea    `json:"idea"`
		Feature store.Feature `json:"feature"`
	}{i, f})
}

// writeIdeaError maps a drop/promote failure: unknown id 404, the created
// feature's duplicate guard 409 (F-0147 shape), anything else — a closed idea,
// a missing module — 422 with the message the CLI prints.
func writeIdeaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case errors.Is(err, app.ErrSimilarExists):
		writeJSON(w, http.StatusConflict, dupConflictBody(err))
	default:
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	}
}
