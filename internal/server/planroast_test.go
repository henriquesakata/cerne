package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gitback"
)

// planRoastBody is the explicit target the UI sends (F-0275); an absent or
// empty doc field means the F-0271 spec roast, pinned by TestRoastByoKey.
const planRoastBody = `{"doc":"plan"}`

// TestPlanRoast pins F-0275 criteria 1/2/4: the plan target runs the plan
// personas over spec+plan, persists its OWN artifact without touching the
// spec roast, and the detail payload exposes it.
func TestPlanRoast(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y."))
	writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "## Sequência de tarefas\n\n1. Implementar y em foo.go, teste primeiro."))
	// Commit the docs: each SaveArtifact commits the clone, so uncommitted
	// working-tree docs would not survive the first round.
	if _, err := gitback.CommitAndPush(repoDir, "docs"); err != nil {
		t.Fatalf("CommitAndPush docs: %v", err)
	}
	var gotPrompt string
	srv := fakeDiagramProvider(t, &gotPrompt, `- [alta] critério 1 descoberto — trecho: \"1. Implementar y\"`)
	configureByoKey(t, projDir, srv.URL)

	var got struct {
		Generated bool   `json:"generated"`
		Partial   bool   `json:"partial"`
		Report    string `json:"report"`
	}
	// A spec roast first, through the real path: the plan roast must land
	// beside it, not on top of it.
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/roast", `{}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("spec roast status = %d, body: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/roast", planRoastBody, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !got.Generated || got.Partial {
		t.Fatalf("generated=%v partial=%v, want true/false", got.Generated, got.Partial)
	}
	for _, p := range app.PlanRoastPersonas {
		if !strings.Contains(got.Report, p.Title) {
			t.Fatalf("report missing plan persona %q:\n%s", p.Title, got.Report)
		}
	}
	// Criterion 2: the critic saw both documents, each fenced.
	for _, want := range []string{"=== SPEC (início) ===", "=== PLANO (início) ===", "QUANDO x O SISTEMA DEVE y", "1. Implementar y"} {
		if !strings.Contains(gotPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, gotPrompt)
		}
	}

	// Criterion 4 under the F-0277 model: one artifact per round, under the
	// plan prefix, stamped with both documents — and the spec round beside
	// it, untouched.
	planFiles, _ := filepath.Glob(filepath.Join(repoDir, "content", "artifacts", "F-0001", "plan-roast-*.md"))
	if len(planFiles) != 1 {
		t.Fatalf("plan rounds persisted = %v, want exactly 1", planFiles)
	}
	data, err := os.ReadFile(planFiles[0])
	if err != nil {
		t.Fatalf("persisted plan roast: %v", err)
	}
	if !strings.Contains(string(data), "Roast do plano") || app.ParseRoastSHA(string(data), "plano") == "" || app.ParseRoastSHA(string(data), "spec") == "" {
		t.Fatalf("plan roast artifact missing header or stamp:\n%s", data)
	}
	specFiles, _ := filepath.Glob(filepath.Join(repoDir, "content", "artifacts", "F-0001", "roast-*.md"))
	if len(specFiles) != 1 {
		t.Fatalf("spec rounds = %v, want the earlier round intact beside the plan one", specFiles)
	}
	spec, err := os.ReadFile(specFiles[0])
	if err != nil || !strings.Contains(string(spec), "Roast da spec") {
		t.Fatalf("plan roast clobbered the spec round (err=%v):\n%s", err, spec)
	}

	var detail struct {
		RoastRounds []struct {
			Name string `json:"name"`
		} `json:"roast_rounds"`
		PlanRoastRounds []struct {
			Name        string `json:"name"`
			SpecChanged bool   `json:"spec_changed"`
			PlanChanged bool   `json:"plan_changed"`
		} `json:"plan_roast_rounds"`
	}
	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	// The two lists stay separate: a plan round never reads as a spec round.
	if len(detail.PlanRoastRounds) != 1 || len(detail.RoastRounds) != 1 {
		t.Fatalf("rounds = spec:%d plan:%d, want 1 each", len(detail.RoastRounds), len(detail.PlanRoastRounds))
	}
	if !strings.HasPrefix(detail.PlanRoastRounds[0].Name, "plan-roast-") {
		t.Errorf("plan round name = %q, want the plan prefix", detail.PlanRoastRounds[0].Name)
	}
	if strings.HasPrefix(detail.RoastRounds[0].Name, "plan-roast-") {
		t.Errorf("spec round list swallowed a plan round: %q", detail.RoastRounds[0].Name)
	}
	if detail.PlanRoastRounds[0].SpecChanged || detail.PlanRoastRounds[0].PlanChanged {
		t.Errorf("round judged the current documents — must not be marked stale")
	}

	// Criterion 4, the stamp's consumer: editing EITHER document flips the
	// badge — the report asserts plan-against-spec conformity, so a spec edit
	// invalidates its verdicts too.
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE z (mudou)."))
	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	if !detail.PlanRoastRounds[0].SpecChanged {
		t.Errorf("spec changed after the round — plan round must be flagged")
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y."))

	// And the plan edit does the same.
	writeDoc(t, repoDir, "F-0001", "plan.md", realDoc("F-0001", "## Sequência de tarefas\n\n1. Outro plano completamente diferente."))
	rec = doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodGet, "/api/features/F-0001", "", &detail)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}
	if !detail.PlanRoastRounds[0].PlanChanged {
		t.Errorf("plan changed after the round — plan round must be flagged")
	}
}

// TestPlanRoastNeedsRealPlan pins F-0275 criterion 1: the trigger is only
// available with a real plan — a missing plan, or the untouched scaffold,
// refuses instead of burning three model calls on boilerplate.
func TestPlanRoastNeedsRealPlan(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "corpo"))

	var errBody struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-0001/roast", planRoastBody, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("missing plan: status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}

	// Scaffold-only plan: doccheck says stub, so the target stays closed.
	writeDoc(t, repoDir, "F-0001", "plan.md", "---\ncerne_id: F-0001\n---\n\n# Título\n\n## Estratégia\n<!-- guia -->\n\n## Sequência de tarefas\n<!-- guia -->\n")
	rec = doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-0001/roast", planRoastBody, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stub plan: status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
}

// TestRoastUnknownTarget: an unrecognized doc target is a client error, not a
// silent fallback to the spec roast — a typo must never quietly judge the
// wrong document.
func TestRoastUnknownTarget(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "corpo"))

	var errBody struct {
		Error string `json:"error"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-x"), http.MethodPost,
		"/api/features/F-0001/roast", `{"doc":"xpto"}`, &errBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}
