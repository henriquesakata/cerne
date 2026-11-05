package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"gitlab.com/rockt/cerne/internal/project"
)

// handleSettingsGet serves the settings registry resolved for projDir
// (F-0314): every key's declaration, effective value and winning scope, plus
// the problems the resolution skipped. Always 200: a bad file is data the
// settings screen shows so the user can fix it.
func handleSettingsGet(w http.ResponseWriter, projDir string) {
	writeJSON(w, http.StatusOK, project.ResolveSettings(projDir))
}

// handleSettingsPut writes {"scope", "value"} for key id and answers with the
// new view. value is passed as its raw JSON text, so `false` is the same
// literal the CLI takes and a JSON string "false" is refused.
func handleSettingsPut(w http.ResponseWriter, r *http.Request, projDir, id string) {
	var req struct {
		Scope string          `json:"scope"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	if err := project.SetSetting(projDir, id, req.Scope, string(req.Value)); err != nil {
		writeConfigError(w, err)
		return
	}
	handleSettingsGet(w, projDir)
}

// handleSettingsDelete removes key id from ?scope= and answers with the new
// view.
func handleSettingsDelete(w http.ResponseWriter, r *http.Request, projDir, id string) {
	// A key the scope does not hold is a no-op here (the CLI says so and
	// exits 1): the UI's unset is idempotent.
	if err := project.UnsetSetting(projDir, id, r.URL.Query().Get("scope")); err != nil && !errors.Is(err, project.ErrSettingNotSet) {
		writeConfigError(w, err)
		return
	}
	handleSettingsGet(w, projDir)
}

// writeConfigError answers a refused config write (/settings, /config/llm)
// with the status its sentinel names; anything else is a 500.
func writeConfigError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, project.ErrUnknownSetting):
		status = http.StatusNotFound
	case errors.Is(err, project.ErrInvalidSettingValue), errors.Is(err, project.ErrSettingScope):
		status = http.StatusBadRequest
	case errors.Is(err, project.ErrConfigLayout), errors.Is(err, project.ErrConfigParse):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, project.ErrConfigBusy), errors.Is(err, project.ErrNoProject):
		status = http.StatusConflict
	}
	writeJSON(w, status, errorBody(err.Error()))
}
