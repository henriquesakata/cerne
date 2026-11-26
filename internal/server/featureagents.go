package server

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// handleFeatureAgents serves the sessions of this machine that worked on card
// id, with where each one is (F-0486). Local only: it carries session ids,
// folders and the resume command.
func handleFeatureAgents(w http.ResponseWriter, repoDir, projectID, id string) {
	sessions, err := cardSessions(repoDir, projectID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found: "+id))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	}
}

func cardSessions(repoDir, projectID, id string) ([]app.EntitySession, error) {
	if !domain.IsFeatureID(id) {
		return nil, fmt.Errorf("%w: not a feature id: %s", store.ErrNotFound, id)
	}
	if _, err := store.LoadFeature(repoDir, id); err != nil {
		return nil, err
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return nil, err
	}
	// Claude Code's transcripts and Cursor's agent-transcripts (F-0513).
	return app.EntitySessions(cacheDir, repoDir, id, app.CostRoots(), cost.CursorRoots())
}

// checkCardRecipients is the trust boundary of a post from a card's page
// (F-0486): it names a card, and only sessions that worked on it.
func checkCardRecipients(repoDir, projectID, ref string, to []string) error {
	if ref == "" || len(to) == 0 {
		return errors.New("a message from a card's page names the card and at least one of its sessions")
	}
	sessions, err := cardSessions(repoDir, projectID, ref)
	if err != nil {
		return err
	}
	for _, s := range to {
		if !slices.ContainsFunc(sessions, func(e app.EntitySession) bool { return e.Session == s }) {
			return fmt.Errorf("session %s did not work on %s", s, ref)
		}
	}
	return nil
}
