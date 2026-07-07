package server

import (
	"encoding/json"
	"net/http"

	"gitlab.com/rockt/cerne/internal/prompt"
)

// promptComposeRequest is the POST /api/prompt/compose body (F-0152).
type promptComposeRequest struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Register bool   `json:"register"`
}

// handlePromptTypes lists the available prompt templates (GET /api/prompt/types).
func handlePromptTypes(w http.ResponseWriter, repoDir string) {
	types, err := prompt.ListTypes(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if types == nil {
		types = []prompt.Type{}
	}
	writeJSON(w, http.StatusOK, map[string][]prompt.Type{"types": types})
}

// handlePromptCompose composes a dispatch prompt (POST /api/prompt/compose):
// same core as the CLI; register=true also mints the W- and returns its id.
func handlePromptCompose(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	var req promptComposeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if req.Type == "" || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("type and text are required"))
		return
	}
	var composed, workID string
	var err error
	if req.Register {
		composed, workID, err = prompt.ComposeRegister(repoDir, projDir, req.Type, req.Text)
	} else {
		composed, err = prompt.Compose(repoDir, projDir, req.Type, req.Text)
	}
	if err != nil {
		// Unknown type, dup-guard and domain failures alike surface as 422
		// with the same message the CLI prints.
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"prompt": composed, "work_id": workID})
}
