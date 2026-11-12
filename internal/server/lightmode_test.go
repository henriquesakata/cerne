package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
)

// TestFeatureDetailReadiness_LightMode (F-0364 crit. 1): in light mode the
// detail the UI's approve button reads carries no bar impediment, so a spec
// with no bar is approvable from the page as well.
func TestFeatureDetailReadiness_LightMode(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Gauntlet: "off"}}); err != nil {
		t.Fatal(err)
	}
	seedFeature(t, repoDir, "F-0001", "Sem barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\nProblema real.\n")
	var got struct {
		Readiness []struct {
			Code string `json:"code"`
			Hard bool   `json:"hard"`
		} `json:"approve_readiness"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, im := range got.Readiness {
		if im.Hard {
			t.Fatalf("light mode readiness carries a hard impediment: %+v", got.Readiness)
		}
	}
}
