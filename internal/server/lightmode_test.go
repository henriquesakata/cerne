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

// TestState_ShowsLightMode (F-0364 crit. 4): /api/state, which the dashboard
// reads, says whether the project runs with the Gauntlet.
func TestState_ShowsLightMode(t *testing.T) {
	for v, want := range map[string]string{"": "on", "on": "on", "OFF": "on", "off": "off"} {
		repoDir, projDir := newBackend(t)
		if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Gauntlet: v}}); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Gauntlet string `json:"gauntlet"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
		if rec.Code != http.StatusOK || got.Gauntlet != want {
			t.Fatalf("gauntlet %q: status %d, gauntlet = %q, want %q", v, rec.Code, got.Gauntlet, want)
		}
	}
}
