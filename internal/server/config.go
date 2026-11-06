package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"sync"

	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// llmConfigResponse is the GET/PUT /api/config/llm payload: the effective LLM
// mode. Secrets never travel here — the byo-key API key stays in the env.
// Models is the resolved model catalog (F-0311: every visibility — the UI
// hides delisted entries but still resolves their labels) and DefaultModel its
// default, served here so the UI never hardcodes a model.
type llmConfigResponse struct {
	Mode         string         `json:"mode"`
	Models       []models.Entry `json:"models,omitempty"`
	DefaultModel string         `json:"default_model,omitempty"`
	// LiveQuery (F-0224) tells the UI whether the chat agent can consult the
	// cerne read commands live (claude-cli's agentic mode) or only sees the
	// static context (byo-key/delegation).
	LiveQuery bool `json:"live_query"`
	// Providers (F-0311) are the vendors the project already reaches (read
	// from [llm], never registered here); PreferredProvider is the global
	// choice the chat starts from and the catalog refresh queries.
	Providers         []string `json:"providers"`
	PreferredProvider string   `json:"preferred_provider,omitempty"`
}

// absentCatalogLogged holds the "no global file" line to once per process: it
// is the permanent state for delegation, claude-cli and offline users, and the
// chat asks for its options on every open.
var absentCatalogLogged sync.Once

// loadCatalog resolves the global model catalog. An unusable global file never
// reaches the page: the baseline is served and the reason logged (F-0149) —
// Info once per process for the expected absent file, Warn for an empty or
// malformed one.
func loadCatalog() models.Catalog {
	dir, err := project.ConfigDir()
	if err != nil {
		telemetry.Warn("model catalog: no user config dir, serving the baseline", "error", err.Error())
		return models.Baseline()
	}
	cat, warn := models.Load(dir)
	switch {
	case errors.Is(warn, fs.ErrNotExist):
		absentCatalogLogged.Do(func() {
			telemetry.Info("model catalog: no global file, serving the baseline", "dir", dir)
		})
	case warn != nil:
		telemetry.Warn("model catalog: global file unusable, serving the baseline", "error", warn.Error())
	}
	return cat
}

// llmConfig builds the GET payload for the project's [llm] config.
func llmConfig(cfg project.LLMConfig) llmConfigResponse {
	cat := loadCatalog()
	mode := llm.ResolveMode(cfg.Mode)
	return llmConfigResponse{
		Mode: mode, Models: cat.Entries, DefaultModel: cat.DefaultModel, LiveQuery: mode == llm.ModeClaudeCLI,
		Providers: llm.ConfiguredProviders(cfg), PreferredProvider: cat.PreferredProvider,
	}
}

// handlePreferredProviderPut records the chat's provider choice in the global
// catalog file (F-0311 criterion 16). Only a provider the project already
// reaches is accepted — this surface never registers one.
func handlePreferredProviderPut(w http.ResponseWriter, r *http.Request, projDir string) {
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil && !errors.Is(err, project.ErrNoProject) {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if !slices.Contains(llm.ConfiguredProviders(cfg.LLM), req.Provider) {
		writeJSON(w, http.StatusBadRequest, errorBody(fmt.Sprintf("provider %q is not configured for this project", req.Provider)))
		return
	}
	dir, err := project.ConfigDir()
	if err == nil {
		err = models.SetPreferredProvider(dir, req.Provider)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"preferred_provider": req.Provider})
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
		writeJSON(w, http.StatusOK, llmConfig(project.LLMConfig{}))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, llmConfig(cfg.LLM))
}

// handleLLMConfigPut sets the [llm] mode in .cernerc, preserving every other
// line of the file (F-0314: comments, order and tables Config does not model).
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
	_, dir, err := project.LoadConfig(projDir)
	if errors.Is(err, project.ErrNoProject) {
		// Serve/multiproject mode: no .cernerc in projDir — seed a minimal one
		// from the docs.lock backend so the mode (and the ask LLM config) persist
		// per project. The byo-key API key never lives here (only the mode).
		lock, lerr := project.LoadLock(projDir)
		if lerr != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(lerr.Error()))
			return
		}
		err = project.SaveConfig(projDir, project.Config{Backend: lock.Backend, LLM: project.LLMConfig{Mode: req.Mode}})
	} else if err == nil {
		// An existing .cernerc is the team's file: edit the one line, never
		// re-render it (comments and unmodeled tables would be lost).
		err = project.SetLLMMode(dir, req.Mode)
	}
	if err != nil {
		writeConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, llmConfigResponse{Mode: req.Mode})
}
