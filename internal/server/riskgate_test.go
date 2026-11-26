package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// riskGateBackend is a backend under the risk gate, past calibration, with
// F-0001 accepted as low risk, F-0002 held on go.mod and accepted on the
// merge, and F-0003 in development.
func riskGateBackend(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newGitBackend(t)
	zero := int64(0)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Accept: project.GateRisk, RiskCalibrationN: &zero}}); err != nil {
		t.Fatal(err)
	}
	seedFeature(t, repoDir, "F-0001", "Low", domain.StateDone)
	seedFeature(t, repoDir, "F-0002", "Held", domain.StateDone)
	seedFeature(t, repoDir, "F-0003", "Live", domain.StateInDevelopment)
	for _, ev := range []domain.Event{
		domain.NewEvent("F-0001", "feature.completed", map[string]string{"risk": "low", "risk_files": "internal/x/x.go, internal/y/y.go"}, "agent"),
		domain.NewEvent("F-0002", "feature.completed", map[string]string{"risk": "held", "risk_files": "go.mod", "risk_patterns": "go.mod"}, "agent"),
	} {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return repoDir, projDir
}

// crossOrigin is a write a page on another site sends.
var crossOrigin = map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json"}

// TestRiskFeedbackRoute: the feature page reads the gate's decision and the
// folders the delivery touched, and the owner's answer from the page lands
// with the folders marked; a write from another origin is refused, unwritten.
// F-0518 crit. 1, 3, 5
func TestRiskFeedbackRoute(t *testing.T) {
	repoDir, projDir := riskGateBackend(t)
	h := New(repoDir, projDir, "")
	var detail struct {
		RiskDecision *struct {
			Risk string   `json:"risk"`
			Dirs []string `json:"dirs"`
		} `json:"risk_decision"`
	}
	doJSON(t, h, http.MethodGet, "/api/features/F-0001", "", &detail)
	if detail.RiskDecision == nil || detail.RiskDecision.Risk != "low" || !slices.Equal(detail.RiskDecision.Dirs, []string{"internal/x/**", "internal/y/**"}) {
		t.Fatalf("risk_decision = %+v, want low with its folders", detail.RiskDecision)
	}
	body := `{"answer":"should_have_asked","reason":"touched billing","add_paths":["internal/x/**"]}`
	if rec := doJSONWith(t, h, http.MethodPost, "/api/features/F-0001/risk-feedback", body, crossOrigin, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin feedback: status %d, want 403", rec.Code)
	}
	if evs, _ := store.ReadEvents(repoDir); slices.ContainsFunc(evs, func(e domain.Event) bool { return e.Type == "feature.risk_feedback" }) {
		t.Fatal("a cross-origin feedback was written")
	}
	var got map[string]any
	if rec := doJSON(t, h, http.MethodPost, "/api/features/F-0001/risk-feedback", body, &got); rec.Code != http.StatusOK {
		t.Fatalf("feedback: status %d, %s", rec.Code, rec.Body.String())
	}
	if cfg, _, _ := project.LoadConfig(projDir); !slices.Contains(cfg.Gates.RiskHumanPaths(), "internal/x/**") {
		t.Errorf("paths = %v, want internal/x/** added at once", cfg.Gates.RiskHumanPaths())
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/features/F-0002/risk-feedback", `{"answer":"should_have_asked"}`, &got); rec.Code != http.StatusConflict {
		t.Errorf("should-have-asked on a held card: status %d, want 409", rec.Code)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/features/F-0001/risk-feedback", `{"answer":"should_have_asked","add_facets":["billing"]}`, &got); rec.Code != http.StatusBadRequest {
		t.Errorf("a facet outside the closed set: status %d, want 400", rec.Code)
	}
}

// TestAcceptModeRoute: the owner marks a live card from the page or the
// kanban; a concluded card, an unknown value and another origin are refused.
// F-0518 crit. 14, 16
func TestAcceptModeRoute(t *testing.T) {
	repoDir, projDir := riskGateBackend(t)
	h := New(repoDir, projDir, "")
	var got map[string]any
	if rec := doJSON(t, h, http.MethodPut, "/api/features/F-0003/accept-mode", `{"mode":"human"}`, &got); rec.Code != http.StatusOK {
		t.Fatalf("mark: status %d, %s", rec.Code, rec.Body.String())
	}
	if f, _ := store.LoadFeature(repoDir, "F-0003"); f.AcceptMode != "human" {
		t.Fatalf("accept_mode = %q, want human", f.AcceptMode)
	}
	for _, c := range []struct {
		id, body string
		headers  map[string]string
		want     int
	}{
		{"F-0003", `{"mode":"always"}`, nil, http.StatusBadRequest},
		{"F-0001", `{"mode":"auto"}`, nil, http.StatusConflict},
		{"F-0003", `{"mode":"auto"}`, crossOrigin, http.StatusForbidden},
	} {
		if rec := doJSONWith(t, h, http.MethodPut, "/api/features/"+c.id+"/accept-mode", c.body, c.headers, nil); rec.Code != c.want {
			t.Errorf("%s %s: status %d, want %d", c.id, c.body, rec.Code, c.want)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0003"); f.AcceptMode != "human" {
		t.Errorf("a refused mark changed it to %q", f.AcceptMode)
	}
}

// TestGateProposalRoute: the Settings screen reads the counts of a period
// and the open proposals, and the owner accepts or dismisses one; a write
// from another origin is refused.
// F-0518 crit. 8, 9, 10
func TestGateProposalRoute(t *testing.T) {
	repoDir, projDir := riskGateBackend(t)
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0009", "feature.created", map[string]string{"fixes": "F-0001"}, "human")); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	h := New(repoDir, projDir, "")
	var v struct {
		Stats struct {
			Accepted, Held struct{ Cards, Fixed int }
		} `json:"stats"`
		Proposals []struct {
			ID  string   `json:"id"`
			Add []string `json:"add"`
		} `json:"proposals"`
	}
	doJSON(t, h, http.MethodGet, "/api/gate/risk?days=7", "", &v)
	if v.Stats.Accepted.Cards != 1 || v.Stats.Accepted.Fixed != 1 || v.Stats.Held.Cards != 1 || len(v.Proposals) != 1 || v.Proposals[0].ID != "tighten:F-0001" {
		t.Fatalf("view = %+v, want one accepted and fixed, one held, the tighten proposal", v)
	}
	if rec := doJSONWith(t, h, http.MethodGet, "/api/gate/risk?days=0", "", nil, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("days=0: status %d, want 400", rec.Code)
	}
	body := `{"id":"tighten:F-0001","decision":"accepted"}`
	if rec := doJSONWith(t, h, http.MethodPost, "/api/gate/proposals", body, crossOrigin, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin decision: status %d, want 403", rec.Code)
	}
	if cfg, _, _ := project.LoadConfig(projDir); cfg.Gates.RiskPaths != nil {
		t.Fatalf("a cross-origin decision wrote the list: %v", cfg.Gates.RiskPaths)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/gate/proposals", body, &v); rec.Code != http.StatusOK || len(v.Proposals) != 0 {
		t.Fatalf("accept: status %d, proposals %+v", rec.Code, v.Proposals)
	}
	if cfg, _, _ := project.LoadConfig(projDir); !slices.Contains(cfg.Gates.RiskHumanPaths(), "internal/y/**") {
		t.Errorf("paths = %v, want the folders in", cfg.Gates.RiskHumanPaths())
	}
	if rec := doJSONWith(t, h, http.MethodPost, "/api/gate/proposals", body, nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("deciding a closed proposal: status %d, want 404", rec.Code)
	}
}
