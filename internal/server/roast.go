package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// roastResponse mirrors generateDiagramResponse: delegation mode signals
// needs_provider instead of failing, so the UI can explain what to configure.
type roastResponse struct {
	Generated     bool   `json:"generated"`
	NeedsProvider bool   `json:"needs_provider"`
	Partial       bool   `json:"partial"`
	Report        string `json:"report,omitempty"`
}

// handleRoast (F-0271): explicit trigger only — runs the three fresh-context
// critic personas over the feature's current spec, persists the report as the
// roast.md artifact next to the candidate, and returns it. The roast never
// approves or rejects; the human gate consumes the findings.
func handleRoast(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	f, err := store.LoadFeature(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("feature not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	switch f.State {
	case domain.StateDone, domain.StateRejected, domain.StateSuperseded:
		writeJSON(w, http.StatusConflict, errorBody("feature is terminal — nothing to roast"))
		return
	}

	specBytes, err := os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusConflict, errorBody("feature has no spec to roast"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || llm.ResolveMode(cfg.LLM.Mode) == llm.ModeDelegation {
		writeJSON(w, http.StatusOK, roastResponse{Generated: false, NeedsProvider: true})
		return
	}
	prov, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}

	rep := app.RunRoast(r.Context(), prov, id, f.Title, string(specBytes), cfg.Agent.ChatTimeoutOrDefault())
	// A round where no critic finished is a cancelled request or a dead
	// provider, not a judgment — persisting it would overwrite a good report
	// with committed garbage.
	if rep.AllFailed() {
		writeJSON(w, http.StatusBadGateway, errorBody("roast: no critic finished ("+rep.Results[0].Err+")"))
		return
	}
	md := app.FormatRoastReport(rep)
	if err := app.SaveArtifact(repoDir, projDir, id, "roast.md", []byte(md)); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, roastResponse{Generated: true, Partial: rep.Partial, Report: md})
}

// roastReportFor reads the persisted roast artifact for the detail payload
// (absent file → empty string, same contract as the lifecycle docs).
func roastReportFor(repoDir, id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "artifacts", id, "roast.md"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
