package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
)

// cliStatus is one subscription CLI and whether it is on PATH (F-0414).
type cliStatus struct {
	Name  string `json:"name"`
	Mode  string `json:"mode"`
	Found bool   `json:"found"`
}

// cliStatuses lists the subscription CLIs in detection order.
func cliStatuses() []cliStatus {
	found := project.FoundSubscriptionCLIs()
	var out []cliStatus
	for _, name := range []string{"claude", "codex", "gemini", "cursor-agent", "copilot"} {
		out = append(out, cliStatus{Name: name, Mode: project.CLIMode(name), Found: slices.Contains(found, name)})
	}
	return out
}

// keyStatus is the state of an API key: the variable it lives in, whether one
// is set and where (env wins over the host file), and its last four characters
// when it is long enough for that to reveal nothing. The value itself never
// leaves the process. Problem names a credentials file that was refused.
type keyStatus struct {
	Env     string `json:"env"`
	Set     bool   `json:"set"`
	Source  string `json:"source,omitempty"`
	Last4   string `json:"last4,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// keyStatusFor is the state of the key c (a raw config: Provider, KeyEnv) reads.
func keyStatusFor(c project.LLMConfig) keyStatus {
	v, source, env, err := c.ResolveKey()
	st := keyStatus{Env: env}
	if err != nil {
		st.Problem = err.Error()
		return st
	}
	if v == "" {
		st.Problem = c.LegacyKeyNote()
		return st
	}
	st.Set, st.Source = true, source
	if len(v) >= 12 {
		st.Last4 = v[len(v)-4:]
	}
	return st
}

// keyProviders are the byo-key providers whose key state the status reports.
var keyProviders = []string{"anthropic", "openai", "gemini", "openrouter", "compatible"}

// keyStatuses is the key state of every provider: Settings enables "Use" when
// the provider's own variable or file key exists, not only for the configured
// provider (F-0414). A saved key_env belongs to the configured provider alone.
func keyStatuses(cfg project.LLMConfig) map[string]keyStatus {
	out := map[string]keyStatus{}
	eff := cfg.WithDefaults().Provider
	for _, p := range keyProviders {
		c := project.LLMConfig{Provider: p}
		if p == eff {
			c.KeyEnv = cfg.KeyEnv
		}
		out[p] = keyStatusFor(c)
	}
	return out
}

// llmTestTimeout bounds the connection test; a CLI cold start is seconds.
var llmTestTimeout = 60 * time.Second

// loadLLMConfig is the project's [llm] (host values included), or the zero one
// in serve mode, where the per-project dir has no .cernerc yet.
func loadLLMConfig(projDir string) (project.LLMConfig, error) {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil && !errors.Is(err, project.ErrNoProject) {
		return project.LLMConfig{}, err
	}
	return cfg.LLM, nil
}

// handleLLMTest makes one minimal call in the requested mode (default: the
// resolved one), optionally over an unsaved provider form whose key is used for
// this call only and answers {ok} or {ok:false,error} — the provider's own error
// already names the fix (login command, install hint, variable). Not a 5xx: a
// failed connection is the answer, not a server fault.
func handleLLMTest(w http.ResponseWriter, r *http.Request, projDir string) {
	var req struct {
		Mode     string `json:"mode"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		BaseURL  string `json:"base_url"`
		Key      string `json:"key"`
	}
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
			return
		}
	}
	if req.Mode != "" && !validLLMMode(req.Mode) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid mode"))
		return
	}
	cfg, err := loadLLMConfig(projDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if req.Mode != "" {
		cfg.Mode = req.Mode
	}
	var note string
	if req.Provider != "" {
		// The unsaved form (F-0414): test what is on screen, not what is saved.
		saved := cfg
		sameProvider := saved.WithDefaults().Provider == req.Provider
		cfg.Provider, cfg.Model, cfg.BaseURL, cfg.TestKey = req.Provider, req.Model, req.BaseURL, req.Key
		if !sameProvider {
			cfg.KeyEnv = "" // a saved key_env belongs to the saved provider
		}
		if req.Mode == "" {
			cfg.Mode = llm.ModeByoKey
		}
		stored, source, env, _ := cfg.ResolveKey()
		if req.Key == "" && stored != "" {
			// A stored key goes only to the provider's own endpoint or to the
			// very URL that is saved, never to one typed in the form.
			official := project.LLMConfig{Provider: req.Provider}.WithDefaults().BaseURL
			base := cfg.WithDefaults().BaseURL
			if base != official && !(sameProvider && base == saved.WithDefaults().BaseURL) {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "refusing to send a stored key to a base URL that is not the saved one: type the key to test it, or save the URL first"})
				return
			}
		}
		if req.Key != "" && stored != "" && source == "env" {
			note = env + " is set in the environment and wins over a saved key in real use."
		}
	}
	reply := func(m map[string]any) {
		if note != "" {
			m["note"] = note
		}
		writeJSON(w, http.StatusOK, m)
	}
	prov, err := newLLMProvider(cfg)
	if errors.Is(err, llm.ErrNoProviderForDelegation) {
		reply(map[string]any{"ok": false, "error": "delegation mode has nothing to test: the agent already running does the work"})
		return
	}
	if err != nil {
		reply(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), llmTestTimeout)
	defer cancel()
	if _, err := prov.Complete(ctx, "Reply with the single word: ok"); err != nil {
		reply(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply(map[string]any{"ok": true})
}

// keyEnvFor is the variable a key saved from Settings for provider (default:
// the configured one) lives in: a saved key_env for the configured provider,
// otherwise the provider's own name — one per vendor, so a key never overwrites
// another provider's. "" for an unknown provider.
func keyEnvFor(cfg project.LLMConfig, provider string) string {
	eff := cfg.WithDefaults().Provider
	if provider == "" {
		provider = eff
	}
	if provider == eff && cfg.KeyEnv != "" {
		return cfg.KeyEnv
	}
	return project.ProviderKeyEnv(provider)
}

// handleLLMKey writes (PUT) or removes (DELETE) the provider's key in the host
// credentials file and answers the key's status — never its value.
func handleLLMKey(w http.ResponseWriter, r *http.Request, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Value    string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	cfg, err := loadLLMConfig(projDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	env := keyEnvFor(cfg, req.Provider)
	if env == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("unknown provider"))
		return
	}
	if r.Method == http.MethodPut {
		err = project.WriteCredential(env, strings.TrimSpace(req.Value))
	} else {
		err = project.DeleteCredential(env)
	}
	switch {
	case errors.Is(err, project.ErrCredentialValue), errors.Is(err, project.ErrCredentialName):
		writeJSON(w, http.StatusBadRequest, errorBody("the key must be one non-empty line"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		provider := req.Provider
		if provider == "" {
			provider = cfg.WithDefaults().Provider
		}
		writeJSON(w, http.StatusOK, keyStatuses(cfg)[provider])
	}
}
