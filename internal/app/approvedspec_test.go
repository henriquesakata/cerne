package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/materialize"
	"gitlab.com/rockt/cerne/internal/store"
)

// approvedSpecA is the spec a test approves: trailing spaces and no final
// newline variation would be lost by any normalizing copy, so a byte-exact
// comparison pins the copy as a copy.
const approvedSpecA = "---\ncerne_id: F-0001\n---\n\n# Versão aprovada\n\nProblema real, com espaço no fim.  \n" + testBarTail

// approveFixture is a backend with F-0001 approved over approvedSpecA.
func approveFixture(t *testing.T) (repoDir, projDir, origin string) {
	t.Helper()
	repoDir, projDir, origin = newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", approvedSpecA)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return repoDir, projDir, origin
}

// TestApproveStoresApprovedSpec (F-0318 crit. 1): the approve keeps a
// byte-exact copy of the spec it approved, committed with the approval — so a
// fresh clone of the origin (another process, a pull, a restarted server)
// still reads the approved version after the live spec moved on.
func TestApproveStoresApprovedSpec(t *testing.T) {
	repoDir, projDir, origin := approveFixture(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nLinha nova depois da aprovação.\n"); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := gitback.Clone(origin, fresh); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	got, err := os.ReadFile(store.ApprovedSpecPath(fresh, "F-0001"))
	if err != nil {
		t.Fatalf("approved copy not in the origin: %v", err)
	}
	if string(got) != approvedSpecA {
		t.Fatalf("approved copy = %q, want the spec as approved %q", got, approvedSpecA)
	}
}

// TestApprovedCopyNotScanned (F-0318, plan task 1): the copy lives outside
// content/features/, so lint, the materializer and the index never read it — a
// broken link or a token that only the copy carries surfaces nowhere.
func TestApprovedCopyNotScanned(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	only := approvedSpecA + "\nzebraquartz [[F-9999]]\n"
	seedCandidateSpec(t, repoDir, "F-0001", only)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}
	if got, err := os.ReadFile(store.ApprovedSpecPath(repoDir, "F-0001")); err != nil || !strings.Contains(string(got), "zebraquartz") {
		t.Fatalf("approved copy = %q, %v — the test needs the copy to carry the token", got, err)
	}

	findings, err := lint.Run(repoDir)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	for _, f := range findings {
		if strings.HasPrefix(f.File, "content/approved/") || strings.Contains(f.Detail, "F-9999") {
			t.Errorf("lint read the approved copy: %+v", f)
		}
	}

	docs := filepath.Join(t.TempDir(), ".docs")
	if _, err := materialize.Materialize(repoDir, docs, "", false); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	_ = filepath.WalkDir(docs, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), "zebraquartz") {
				t.Errorf("materialized %s carries the approved copy", p)
			}
		}
		return nil
	})

	db := filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, db); err != nil {
		t.Fatalf("index: %v", err)
	}
	if hits, err := index.Search(db, "zebraquartz", 10); err != nil || len(hits) != 0 {
		t.Fatalf("index hits = %v, %v — the copy must not be indexed", hits, err)
	}
}

// approvedWithPlan is approveFixture with a real plan, so only the stale
// approval can stop a start.
func approvedWithPlan(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = approveFixture(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "plan", "# Plano\n\n## Tarefas\n\nPlano real, não um stub.\n"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	return repoDir, projDir
}

// TestStartRefusesStaleApproval (F-0318 crit. 6, 3): a start over a spec that
// differs from the approved one is refused before anything is written — no
// state change, no work opened — naming the reason and the re-approve command.
// A spec edited back to the approved text (A→B→A) starts.
func TestStartRefusesStaleApproval(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if !errors.Is(err, ErrApprovalStale) {
		t.Fatalf("start err = %v, want ErrApprovalStale", err)
	}
	for _, want := range []string{"spec changed since approval", "cerne feature approve F-0001"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved {
		t.Fatalf("state = %s after a refused start, want approved", f.State)
	}
	if works, _ := store.LoadAllWorks(repoDir); len(works) != 0 {
		t.Fatalf("a refused start opened works: %+v", works)
	}

	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA); err != nil {
		t.Fatalf("edit back: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("A→B→A start: %v", err)
	}
}

// TestStartPropagationSkipsStaleEpic (F-0318 crit. 6): a child's start pulls
// its ready epic along (propagateStartToAncestors) — never one whose approval
// went stale, or the propagation would be a way around the start gate.
func TestStartPropagationSkipsStaleEpic(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Filha", State: domain.StateApproved, Parent: "F-0001"}); err != nil {
		t.Fatal(err)
	}
	writeFeatureDoc(t, repoDir, "F-0002", "plan.md", "# Plano\n\n## Tarefas\n\nPlano real da filha.\n")
	commitBackend(t, repoDir)

	out, err := TransitionFeature(repoDir, projDir, "F-0002", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("child start: %v", err)
	}
	if len(out.Propagated) != 0 {
		t.Fatalf("propagated = %v, want the stale epic left approved", out.Propagated)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved {
		t.Fatalf("epic state = %s, want approved", f.State)
	}
}

// TestReapproveRefreshesApprovedSpec (F-0318 crit. 7): approving an approved
// feature whose spec changed promotes the current spec to the new approved
// version — no revert — which clears the stale approval and reopens the start.
func TestReapproveRefreshesApprovedSpec(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	specB := approvedSpecA + "\nCritério novo.\n"
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", specB); err != nil {
		t.Fatalf("edit: %v", err)
	}

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("re-approve: %v", err)
	}
	if out.From != domain.StateApproved || out.To != domain.StateApproved || out.Baseline {
		t.Fatalf("outcome = %+v, want approved → approved, a renewal (not a baseline)", out)
	}
	if got, _ := os.ReadFile(store.ApprovedSpecPath(repoDir, "F-0001")); string(got) != specB {
		t.Fatalf("approved copy = %q, want the re-approved spec", got)
	}
	f, _ := store.LoadFeature(repoDir, "F-0001")
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
		t.Fatal("still stale after the re-approval")
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start after re-approval: %v", err)
	}
}

// TestReapproveRefusedWhenNotStale (F-0318 crit. 7): there is nothing to
// re-approve while the spec is the approved version. The readiness the approve
// --check and the page read refuses alike, as an invalid transition, so every
// "nothing to approve here" caller keeps reading it as before.
func TestReapproveRefusedWhenNotStale(t *testing.T) {
	repoDir, projDir, _ := approveFixture(t)
	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if !errors.Is(err, ErrNothingToReapprove) || !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("re-approve err = %v, want ErrNothingToReapprove (an invalid transition)", err)
	}
	if _, err := ApproveReadiness(repoDir, "F-0001"); !errors.Is(err, ErrNothingToReapprove) {
		t.Fatalf("readiness err = %v, want ErrNothingToReapprove", err)
	}
}

// TestApproveRecordsLegacyBaseline (F-0318 crit. 14): a feature approved
// before F-0318 has no approved copy — never stale, never blocked — and its
// next approve, on the approved feature itself, records the current spec as
// the baseline instead of being refused: the readiness reads it as
// approvable, the outcome says it was a baseline, and from then on the
// comparison holds (nothing to re-approve until the spec changes).
func TestApproveRecordsLegacyBaseline(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md") // legacy: no copy
	if !ApprovalBaselineMissing(repoDir, store.Feature{ID: "F-0001", State: domain.StateApproved}) {
		t.Fatal("legacy approval not reported as missing its baseline")
	}
	if imps, err := ApproveReadiness(repoDir, "F-0001"); err != nil || len(imps) != 0 {
		t.Fatalf("readiness = %v, %v, want approvable", imps, err)
	}

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("baseline approve: %v", err)
	}
	if out.From != domain.StateApproved || out.To != domain.StateApproved || !out.Baseline {
		t.Fatalf("outcome = %+v, want approved → approved, baseline", out)
	}
	spec, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if got, err := os.ReadFile(store.ApprovedSpecPath(repoDir, "F-0001")); err != nil || string(got) != string(spec) {
		t.Fatalf("baseline copy = %q, %v, want the current spec", got, err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); !errors.Is(err, ErrNothingToReapprove) {
		t.Fatalf("approve after the baseline err = %v, want ErrNothingToReapprove", err)
	}
}

// TestChatUpdatesApprovedSpec (F-0318 crit. 5): the Backlog chat refines and
// rewrites an approved feature's spec — F-0262's chat restriction revoked for
// approved only — and the write follows criteria 2 and 3: a different spec
// stales the approval, the approved text written back does not.
func TestChatUpdatesApprovedSpec(t *testing.T) {
	repoDir, projDir, _ := approveFixture(t)
	block, err := BuildRefineBlock(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("refine turn on approved: %v", err)
	}
	if !strings.Contains(block, "reaprovar") {
		t.Errorf("refine block does not warn that a new spec stales the approval:\n%s", block)
	}

	f, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", "## Problema\n\nVersão do chat.\n"+testBarTail)
	if err != nil {
		t.Fatalf("chat write on approved: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved unchanged", f.State)
	}
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); !stale {
		t.Fatal("a different spec from the chat did not stale the approval")
	}

	body := strings.TrimPrefix(approvedSpecA, "---\ncerne_id: F-0001\n---\n\n")
	if f, err = UpdateCandidateSpec(repoDir, projDir, "F-0001", body); err != nil {
		t.Fatalf("chat write back: %v", err)
	}
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
		t.Fatal("the approved text written back by the chat is still stale")
	}
}

// TestChatStillRefusesInDevelopment (F-0318 crit. 5, 12): the chat's reach
// stops at approved — in development the spec is no longer the chat's to
// rewrite, on the turn and on the write.
func TestChatStillRefusesInDevelopment(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	before := readFeatureDocFile(t, repoDir, "F-0001", "spec.md")
	if _, err := BuildRefineBlock(repoDir, "F-0001"); !errors.Is(err, ErrNotCandidate) {
		t.Fatalf("refine err = %v, want ErrNotCandidate", err)
	}
	_, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", "## Problema\n\nBurlado.\n")
	if !errors.Is(err, ErrNotCandidate) || !strings.Contains(err.Error(), "cerne push") {
		t.Fatalf("write err = %v, want ErrNotCandidate naming the push path", err)
	}
	if got := readFeatureDocFile(t, repoDir, "F-0001", "spec.md"); got != before {
		t.Fatalf("spec.md changed on a refused write: %q", got)
	}
}

// assertNotStale fails when feature id reads as a stale approval on the
// function or as a lint finding.
func assertNotStale(t *testing.T, repoDir, id string) {
	t.Helper()
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
		t.Errorf("%s (%s) reads as a stale approval", id, f.State)
	}
	findings, err := lint.Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Rule == lint.RuleSpecChangedAfterApproval {
			t.Errorf("%s (%s): %+v", id, f.State, fd)
		}
	}
}

// TestInDevelopmentSpecWriteNeverStales (F-0318 crit. 12; F-0262 crit. 7): in
// development a spec write is never blocked by the state and never stales
// anything — the approval is spent, the spec is the contract being built.
func TestInDevelopmentSpecWriteNeverStales(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nAjuste durante o desenvolvimento.\n"); err != nil {
		t.Fatalf("spec write in development: %v", err)
	}
	assertNotStale(t, repoDir, "F-0001")
}

// TestDoneFeatureKeepsErrataPath (F-0318 crit. 12, 13, read against the F-0305
// boundary): a closed feature's documents are frozen — implemented corrects
// its result with amend, done only takes errata — and neither ever reads as a
// stale approval, though the approved copy is still in the backend.
func TestDoneFeatureKeepsErrataPath(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "result", "# Resultado\n\nEntregue.\n"+testGauntletTail); err != nil {
		t.Fatalf("result: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// implemented: the spec is frozen; the result's correction is amend.
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nTarde demais.\n"); !errors.Is(err, ErrFeatureFrozen) {
		t.Fatalf("implemented spec write err = %v, want ErrFeatureFrozen", err)
	}
	if _, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody, "evidência faltando"); err != nil {
		t.Fatalf("amend on implemented: %v", err)
	}
	assertNotStale(t, repoDir, "F-0001")

	// done: amend refuses with the errata path; the errata is appended.
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "", nil); err != nil {
		t.Fatalf("accept: %v", err)
	}
	_, _, err := AmendFeatureResult(repoDir, projDir, "F-0001", amendedBody+"\nDe novo.\n", "de novo")
	if !errors.Is(err, ErrAmendNotImplemented) || !strings.Contains(err.Error(), "cerne feature errata F-0001") {
		t.Fatalf("amend on done err = %v, want ErrAmendNotImplemented naming the errata", err)
	}
	if _, err := UpdateCandidateSpec(repoDir, projDir, "F-0001", "## Problema\n\nTarde.\n"); !errors.Is(err, ErrNotCandidate) || !strings.Contains(err.Error(), "cerne feature errata F-0001") {
		t.Fatalf("spec verb on done err = %v, want ErrNotCandidate naming the errata", err)
	}
	if _, err := AddErrata(repoDir, projDir, "F-0001", "Correção depois do aceite.", "tester"); err != nil {
		t.Fatalf("errata on done: %v", err)
	}
	assertNotStale(t, repoDir, "F-0001")
	if _, err := os.Stat(store.ApprovedSpecPath(repoDir, "F-0001")); err != nil {
		t.Fatalf("approved copy gone: %v", err)
	}
}
