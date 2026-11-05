package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeApprovedCopy writes the spec copy feature id's last approve kept (F-0318).
func writeApprovedCopy(t *testing.T, repoDir, id, content string) {
	t.Helper()
	path := store.ApprovedSpecPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFeatureDetailSpecChangedAfterApproval pins F-0262 as F-0318 redefined
// it: the feature page warns that the approval is stale when the live spec
// differs from the approved copy — same derivation the lint rule uses.
func TestFeatureDetailSpecChangedAfterApproval(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Editada após aprovar", domain.StateApproved)
	seedFeature(t, repoDir, "F-0002", "Aprovada intacta", domain.StateApproved)
	writeApprovedCopy(t, repoDir, "F-0001", "A\n")
	writeDoc(t, repoDir, "F-0001", "spec.md", "A\nB\n")
	writeApprovedCopy(t, repoDir, "F-0002", "A\n")
	writeDoc(t, repoDir, "F-0002", "spec.md", "A\n")

	var got struct {
		SpecChanged bool `json:"spec_changed_after_approval"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK || !got.SpecChanged {
		t.Fatalf("F-0001: status %d, spec_changed_after_approval = %v, want true", rec.Code, got.SpecChanged)
	}
	got.SpecChanged = false
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &got)
	if rec.Code != http.StatusOK || got.SpecChanged {
		t.Fatalf("F-0002: status %d, spec_changed_after_approval = %v, want false", rec.Code, got.SpecChanged)
	}
}

// TestStaleApprovalOneSourceOfTruth (F-0318 crit. 4, 3): lint, the feature
// payload, the board payload and the start gate read one fact. Approved over A, edited to B, all
// three accuse it; edited back to A — with edit events after the approval in
// the log — none does.
func TestStaleApprovalOneSourceOfTruth(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	specA := realDoc("F-0001", "Problema real, versão aprovada.")
	writeDoc(t, repoDir, "F-0001", "spec.md", specA)
	commitBackend(t, repoDir)
	if _, err := app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "plan", realDoc("F-0001", "Plano real.")); err != nil {
		t.Fatalf("plan: %v", err)
	}

	surfaces := func() (finding, payload bool) {
		t.Helper()
		findings, err := lint.Run(repoDir)
		if err != nil {
			t.Fatalf("lint: %v", err)
		}
		for _, f := range findings {
			finding = finding || (f.Rule == lint.RuleSpecChangedAfterApproval && f.Entity == "F-0001")
		}
		var got struct {
			SpecChanged bool `json:"spec_changed_after_approval"`
		}
		if rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got); rec.Code != http.StatusOK {
			t.Fatalf("detail status %d", rec.Code)
		}
		var state struct {
			StaleApprovals []string `json:"stale_approvals"`
		}
		doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &state)
		if board := slices.Contains(state.StaleApprovals, "F-0001"); board != got.SpecChanged {
			t.Fatalf("board stale %v disagrees with the feature payload %v", board, got.SpecChanged)
		}
		return finding, got.SpecChanged
	}

	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", realDoc("F-0001", "Problema real, com um critério novo.")); err != nil {
		t.Fatalf("A→B: %v", err)
	}
	finding, payload := surfaces()
	_, startErr := app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if !finding || !payload || !errors.Is(startErr, app.ErrApprovalStale) {
		t.Fatalf("A→B: lint %v, payload %v, start %v — want all three to accuse it", finding, payload, startErr)
	}

	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", specA); err != nil {
		t.Fatalf("A→B→A: %v", err)
	}
	finding, payload = surfaces()
	_, startErr = app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if finding || payload || startErr != nil {
		t.Fatalf("A→B→A: lint %v, payload %v, start %v — want none to accuse it", finding, payload, startErr)
	}
}

// TestFeatureDetailCarriesApprovedSpec (F-0318 crit. 9, 7): a stale approval's
// payload carries the approval delta — the approved and the live spec as the
// comparison read them (bodies, no frontmatter), so the page diffs exactly what
// staled it — and the re-approve's readiness, so the button tells before the
// click. A clean approval carries neither.
func TestFeatureDetailCarriesApprovedSpec(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Editada após aprovar", domain.StateApproved)
	seedFeature(t, repoDir, "F-0002", "Aprovada intacta", domain.StateApproved)
	writeApprovedCopy(t, repoDir, "F-0001", realDoc("F-0001", "Versão aprovada."))
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Versão editada."))
	writeApprovedCopy(t, repoDir, "F-0002", realDoc("F-0002", "Versão aprovada."))
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "Versão aprovada."))

	type delta struct{ Approved, Current string }
	var got struct {
		ApprovalDelta    *delta                  `json:"approval_delta"`
		Bar              string                  `json:"bar"`
		ApproveReadiness []app.ApproveImpediment `json:"approve_readiness"`
	}
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	body := func(doc string) string { return strings.TrimSpace(doc[strings.Index(doc, "# Título"):]) }
	want := delta{body(realDoc("F-0001", "Versão aprovada.")), body(realDoc("F-0001", "Versão editada."))}
	if got.ApprovalDelta == nil || *got.ApprovalDelta != want {
		t.Fatalf("F-0001 approval_delta = %+v, want the two bodies %+v", got.ApprovalDelta, want)
	}
	if got.Bar == "" {
		t.Fatalf("F-0001 bar = %q, want the bar the re-approve would accept", got.Bar)
	}

	got.ApprovalDelta, got.Bar = nil, ""
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &got)
	if got.ApprovalDelta != nil || got.Bar != "" {
		t.Fatalf("F-0002 approval_delta = %v, bar = %q, want neither on a clean approval", got.ApprovalDelta, got.Bar)
	}
}

// TestStateCarriesStaleApprovals (F-0318 crit. 8, 4): the board payload lists
// the features whose approval went stale — the kanban badges them from the
// same function every other surface reads — and is never null.
func TestStateCarriesStaleApprovals(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Editada após aprovar", domain.StateApproved)
	seedFeature(t, repoDir, "F-0002", "Aprovada intacta", domain.StateApproved)
	seedFeature(t, repoDir, "F-0003", "Legada, sem cópia", domain.StateApproved)
	writeApprovedCopy(t, repoDir, "F-0001", "A\n")
	writeDoc(t, repoDir, "F-0001", "spec.md", "A\nB\n")
	writeApprovedCopy(t, repoDir, "F-0002", "A\n")
	writeDoc(t, repoDir, "F-0002", "spec.md", "A\n")
	writeDoc(t, repoDir, "F-0003", "spec.md", "B\n")

	var got struct {
		StaleApprovals []string `json:"stale_approvals"`
	}
	doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/state", "", &got)
	if len(got.StaleApprovals) != 1 || got.StaleApprovals[0] != "F-0001" {
		t.Fatalf("stale_approvals = %v, want [F-0001]", got.StaleApprovals)
	}

	empty, emptyProj := newBackend(t)
	var raw map[string]json.RawMessage
	doJSON(t, New(empty, emptyProj, ""), http.MethodGet, "/api/state", "", &raw)
	if string(raw["stale_approvals"]) != "[]" {
		t.Fatalf("stale_approvals = %s, want [] when none", raw["stale_approvals"])
	}
}
