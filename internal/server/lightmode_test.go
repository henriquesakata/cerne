package server

import (
	"net/http"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func saveGauntlet(t *testing.T, projDir, v string) {
	t.Helper()
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Gauntlet: v}}); err != nil {
		t.Fatal(err)
	}
}

// TestFeatureDetailReadiness_LightMode (F-0364 crit. 1, 2): the detail the
// UI's approve button reads lists the hard bar-missing impediment with the
// Gauntlet on, and none in light mode — so the button does not block there.
func TestFeatureDetailReadiness_LightMode(t *testing.T) {
	for v, wantBar := range map[string]bool{"": true, "off": false} {
		repoDir, projDir := newBackend(t)
		saveGauntlet(t, projDir, v)
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
		bar, hard := false, false
		for _, im := range got.Readiness {
			bar = bar || im.Code == "bar-missing"
			hard = hard || im.Hard
		}
		if bar != wantBar || hard != wantBar {
			t.Fatalf("gauntlet %q: readiness %+v, want bar-missing (hard) = %v", v, got.Readiness, wantBar)
		}
	}
}

// TestTransitionApprove_LightMode (F-0364 crit. 1): the UI's transition route
// approves a spec with no bar in light mode.
func TestTransitionApprove_LightMode(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	saveGauntlet(t, projDir, "off")
	writeDoc(t, repoDir, "F-0001", "spec.md", "---\ncerne_id: F-0001\n---\n\n# Título\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition", `{"action":"approve"}`, &map[string]string{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
}

// TestLintEndpoint_LightMode (F-0364 crit. 3): /api/lint charges
// spec-missing-bar with the Gauntlet on and drops it in light mode.
func TestLintEndpoint_LightMode(t *testing.T) {
	for v, want := range map[string]bool{"": true, "off": false} {
		repoDir, projDir := newBackend(t)
		saveGauntlet(t, projDir, v)
		seedFeature(t, repoDir, "F-0001", "Sem barra", domain.StateCandidate)
		writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\nProblema real.\n")
		var got struct {
			Findings []struct {
				Rule string `json:"rule"`
			} `json:"findings"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/lint", "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		has := false
		for _, f := range got.Findings {
			has = has || f.Rule == "spec-missing-bar"
		}
		if has != want {
			t.Fatalf("gauntlet %q: spec-missing-bar present = %v, want %v (%+v)", v, has, want, got.Findings)
		}
	}
}

// TestState_ShowsLightMode (F-0364 crit. 4): /api/state, which the dashboard
// reads, says whether the project runs with the Gauntlet.
func TestState_ShowsLightMode(t *testing.T) {
	for v, want := range map[string]string{"": "on", "on": "on", "OFF": "on", "off": "off"} {
		repoDir, projDir := newBackend(t)
		saveGauntlet(t, projDir, v)
		var got struct {
			Gauntlet string `json:"gauntlet"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
		if rec.Code != http.StatusOK || got.Gauntlet != want {
			t.Fatalf("gauntlet %q: status %d, gauntlet = %q, want %q", v, rec.Code, got.Gauntlet, want)
		}
	}
}
