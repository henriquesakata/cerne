package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
)

// llmConfigResponse is the GET/PUT /api/config/llm payload: the effective LLM
// mode. Secrets never travel here — the byo-key API key stays in the env.
// Models/Efforts (F-0218) are the known options for the Backlog chat's
// model/effort selector — served here so the UI never hardcodes them.
type llmConfigResponse struct {
	Mode    string   `json:"mode"`
	Models  []string `json:"models,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	// LiveQuery (F-0224) tells the UI whether the chat agent can consult the
	// cerne read commands live (claude-cli's agentic mode) or only sees the
	// static context (byo-key/delegation).
	LiveQuery bool `json:"live_query"`
}

// llmConfigRequest is the PUT /api/config/llm body.
type llmConfigRequest struct {
	Mode string `json:"mode"`
}

// validLLMMode reports whether mode is one cerne understands.
func validLLMMode(mode string) bool {
	switch mode {
	case llm.ModeDelegation, llm.ModeByoKey, llm.ModeClaudeCLI:
		return true
	}
	return false
}

// handleLLMConfigGet returns the project's effective LLM mode (empty → the
// normalized "delegation" default), read from .cernerc at projDir.
func handleLLMConfigGet(w http.ResponseWriter, projDir string) {
	cfg, _, err := project.LoadConfig(projDir)
	if errors.Is(err, project.ErrNoProject) {
		// Serve/multiproject mode: the per-project dir holds only docs.lock, no
		// .cernerc yet. Report the normalized default; a PUT seeds the .cernerc.
		writeJSON(w, http.StatusOK, llmConfigResponse{Mode: llm.ResolveMode(""), Models: llm.KnownModels, Efforts: llm.KnownEfforts})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	mode := llm.ResolveMode(cfg.LLM.Mode)
	writeJSON(w, http.StatusOK, llmConfigResponse{Mode: mode, Models: llm.KnownModels, Efforts: llm.KnownEfforts, LiveQuery: mode == llm.ModeClaudeCLI})
}

// handleLLMConfigPut sets the [llm] mode in .cernerc, preserving every other
// config field (project_id/backend/default_scope and the other [llm] fields).
// It writes only the mode — never a secret. The /api/ask handler reloads the
// config per request, so the change takes effect on the next question.
func handleLLMConfigPut(w http.ResponseWriter, r *http.Request, projDir string) {
	var req llmConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	if !validLLMMode(req.Mode) {
		writeJSON(w, http.StatusBadRequest,
			errorBody(`invalid mode (expected "delegation", "claude-cli" or "byo-key")`))
		return
	}
	cfg, dir, err := project.LoadConfig(projDir)
	if errors.Is(err, project.ErrNoProject) {
		// Serve/multiproject mode: no .cernerc in projDir — seed a minimal one
		// from the docs.lock backend so the mode (and the ask LLM config) persist
		// per project. The byo-key API key never lives here (only the mode).
		lock, lerr := project.LoadLock(projDir)
		if lerr != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(lerr.Error()))
			return
		}
		cfg = project.Config{Backend: lock.Backend}
		dir = projDir
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	cfg.LLM.Mode = req.Mode
	if err := project.SaveConfig(dir, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, llmConfigResponse{Mode: req.Mode})
}
