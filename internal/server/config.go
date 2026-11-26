package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/henriquesakata/cerne/internal/gitback"
	"io/fs"
	"net/http"
	"slices"
	"sync"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/models"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// newLLMProvider builds the provider the connection test calls; tests swap it
// for a fake so no CLI or network is reached.
var newLLMProvider = llm.NewProvider

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
	// ImageProviders (F-0520) are the Providers whose chat turn takes attached
	// images; the chat offers the attach button only for them.
	ImageProviders []string `json:"image_providers,omitempty"`
	// F-0414: what the AI settings panel shows. Source says where Mode came
	// from (configured, detected on PATH, default); CLIs are the three
	// subscription CLIs with whether each is installed; Key is the state of the
	// provider's API key, never its value.
	// Keys is every provider's key state; Key is the configured provider's.
	// Both and CLIs describe the machine that serves the page: the remote
	// `cerne serve` omits them.
	Source   string               `json:"source"`
	CLIs     []cliStatus          `json:"clis,omitempty"`
	Provider string               `json:"provider"`
	Model    string               `json:"model"`
	BaseURL  string               `json:"base_url"`
	Key      *keyStatus           `json:"key,omitempty"`
	Keys     map[string]keyStatus `json:"keys,omitempty"`
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

// reachableProviders is the vendors a chat turn can run on (F-0440): the
// configured mode's, then the one of every subscription CLI found on PATH —
// local ui only, the remote serve offers just the configured one. No repeats.
func reachableProviders(cfg project.LLMConfig, remote bool) []string {
	out := llm.ConfiguredProviders(cfg)
	if remote {
		return out
	}
	for _, c := range cliStatuses() {
		if !c.Found {
			continue
		}
		for _, p := range llm.ConfiguredProviders(project.LLMConfig{Mode: c.Mode}) {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// imageProviders is the subset of providers whose chat turn takes images
// (F-0520): the mode turnLLM would run each one on must have an image input.
// None on the remote serve, which has no attachment store.
func imageProviders(cfg project.LLMConfig, providers []string, remote bool) []string {
	if remote {
		return nil
	}
	var out []string
	for _, p := range providers {
		if llm.SupportsImages(llm.ResolveMode(llm.ForProvider(cfg, p).Mode)) {
			out = append(out, p)
		}
	}
	return out
}

func codexInstalled() bool { return slices.Contains(project.FoundSubscriptionCLIs(), "codex") }

// llmConfig builds the GET payload for the project's [llm] config.
func llmConfig(cfg project.LLMConfig, remote bool) llmConfigResponse {
	cat := loadCatalog()
	mode, source := llm.ResolveModeSource(cfg.Mode)
	eff := cfg.WithDefaults()
	// The model the mode in use is actually given: claude-cli is never forced
	// one, codex, gemini, cursor and copilot get it only when no byo-key provider is set.
	model := cfg.Model
	switch mode {
	case llm.ModeClaudeCLI:
		model = ""
	case llm.ModeCodexCLI, llm.ModeGeminiCLI, llm.ModeCursorCLI, llm.ModeCopilotCLI:
		if cfg.Provider != "" {
			model = ""
		}
	}
	resp := llmConfigResponse{
		Mode: mode, Models: cat.Entries, DefaultModel: cat.DefaultModel, LiveQuery: mode == llm.ModeClaudeCLI,
		Providers: reachableProviders(cfg, remote), PreferredProvider: cat.PreferredProvider,
		Source: source, Provider: eff.Provider, Model: model, BaseURL: eff.BaseURL,
	}
	resp.ImageProviders = imageProviders(cfg, resp.Providers, remote)
	// B-0006: the baseline knows no Codex model (each account sees its own);
	// the CLI's cache is what its picker offers.
	// F-0440: an installed codex offers them too, so picking OpenAI in a project
	// run on another CLI shows its models (local ui only).
	if mode == llm.ModeCodexCLI || (!remote && codexInstalled()) {
		for _, m := range llm.CodexModels() {
			if !slices.ContainsFunc(resp.Models, func(e models.Entry) bool { return e.ID == m.ID }) {
				resp.Models = append(resp.Models, m)
			}
		}
	}
	if !remote {
		resp.CLIs = cliStatuses()
		resp.Keys = keyStatuses(cfg)
		k := resp.Keys[eff.Provider]
		resp.Key = &k
	}
	return resp
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
	if !slices.Contains(reachableProviders(cfg.LLM, servedRemotely(r)), req.Provider) {
		writeJSON(w, http.StatusBadRequest, errorBody(fmt.Sprintf("provider %q is neither configured for this project nor an installed subscription CLI", req.Provider)))
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
	case llm.ModeDelegation, llm.ModeByoKey, llm.ModeClaudeCLI, llm.ModeCodexCLI, llm.ModeGeminiCLI, llm.ModeCursorCLI, llm.ModeCopilotCLI:
		return true
	}
	return false
}

// handleLLMConfigGet returns the project's effective LLM mode (empty → the
// normalized "delegation" default), read from .cernerc at projDir.
func handleLLMConfigGet(w http.ResponseWriter, r *http.Request, projDir string) {
	remote := servedRemotely(r)
	cfg, _, err := project.LoadConfig(projDir)
	if errors.Is(err, project.ErrNoProject) {
		// Serve/multiproject mode: the per-project dir holds only docs.lock, no
		// .cernerc yet. Report the normalized default; a PUT seeds the .cernerc.
		writeJSON(w, http.StatusOK, llmConfig(project.LLMConfig{}, remote))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	resp := llmConfig(cfg.LLM, remote)
	resp.LiveQuery = resp.LiveQuery || codexLiveQuery(projDir, resp.Mode, remote)
	writeJSON(w, http.StatusOK, resp)
}

// codexLiveQuery is the one answer to "does this codex-cli chat turn read the
// project": local, and the project has a commit for the files server to serve.
// The live_query flag, the MCP root and the prompt block all come from it, so
// the UI never promises what the turn lacks (F-0439).
func codexLiveQuery(projDir, mode string, remote bool) bool {
	if mode != llm.ModeCodexCLI || remote {
		return false
	}
	_, err := gitback.ProjectTree(projDir)
	return err == nil
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
			errorBody(`invalid mode (expected "delegation", "claude-cli", "codex-cli", "gemini-cli", "cursor-cli", "copilot-cli" or "byo-key")`))
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
