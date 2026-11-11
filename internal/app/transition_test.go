package app

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedPaid registers an open pending and points it at featureID directly in
// the store — the state a link or promote leaves — committed and pushed so the
// transition's critical section reads it.
func seedPaid(t *testing.T, repoDir, projDir, title, from, featureID string) store.Pending {
	t.Helper()
	p := mustPending(t, repoDir, projDir, title, "", from)
	p.State, p.PromotedTo = domain.PendingPromoted, featureID
	if err := store.SavePending(repoDir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed paid "+p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

// lastCommitFiles lists the files the backend's last commit touched.
func lastCommitFiles(t *testing.T, repoDir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "log", "-1", "--name-only", "--format=").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.Fields(string(out))
}

func pendingState(t *testing.T, repoDir, id string) store.Pending {
	t.Helper()
	p, err := store.LoadPending(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// F-0885 crit. 5: the accept resolves every pending the feature pays, with
// pending.resolved carrying the feature, in the same backend commit as the
// accept; a pending paid by another feature is untouched. The auto-accept of
// a complete ([gates] accept = "auto") resolves them too.
func TestAcceptResolvesPaidPendings(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateImplemented)
	seedFeatureInState(t, repoDir, "F-0011", domain.StateApproved)
	p1 := seedPaid(t, repoDir, projDir, "Primeiro débito pago", origin.ID, "F-0010")
	p2 := seedPaid(t, repoDir, projDir, "Segundo assunto pago", origin.ID, "F-0010")
	other := seedPaid(t, repoDir, projDir, "Débito de outra feature", origin.ID, "F-0011")
	// A pending already closed that still names F-0010 is not paid by it: the
	// accept leaves it alone instead of failing on it.
	closed := seedPaid(t, repoDir, projDir, "Débito já fechado à mão", origin.ID, "F-0010")
	closed.State = domain.PendingResolved
	if err := store.SavePending(repoDir, closed); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed closed "+closed.ID); err != nil {
		t.Fatal(err)
	}

	out, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionAccept, "", "", nil)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	for _, p := range []store.Pending{p1, p2} {
		got := pendingState(t, repoDir, p.ID)
		if got.State != domain.PendingResolved || got.PromotedTo != "F-0010" || got.ClosedAt == nil {
			t.Fatalf("%s = %+v, want resolved, still pointing at F-0010", p.ID, got)
		}
		if pl := lastPayload(t, repoDir, p.ID, "pending.resolved"); pl["feature"] != "F-0010" {
			t.Fatalf("pending.resolved payload = %v, want feature F-0010", pl)
		}
	}
	files := lastCommitFiles(t, repoDir)
	for _, want := range []string{"entities/features/F-0010.json", "entities/pendings/" + p1.ID + ".json", "entities/pendings/" + p2.ID + ".json"} {
		if !slices.Contains(files, want) {
			t.Fatalf("the accept's commit lacks %s: %v", want, files)
		}
	}
	if !slices.Equal(out.Pendings, []string{p1.ID + ": promoted -> resolved", p2.ID + ": promoted -> resolved"}) {
		t.Fatalf("outcome pendings = %v, want both resolved", out.Pendings)
	}
	if got := pendingState(t, repoDir, other.ID); got.State != domain.PendingPromoted || got.PromotedTo != "F-0011" {
		t.Fatalf("pending of another feature = %+v, want untouched", got)
	}

	cfg := project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Accept: project.GateAuto}}
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	seedFeatureInState(t, repoDir, "F-0012", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	auto := seedPaid(t, repoDir, projDir, "Débito do auto-accept", origin.ID, "F-0012")
	citeInResult(t, repoDir, "F-0012", auto.ID)
	if _, err := TransitionFeature(repoDir, projDir, "F-0012", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete with auto accept: %v", err)
	}
	if got := pendingState(t, repoDir, auto.ID); got.State != domain.PendingResolved {
		t.Fatalf("auto-accept left %s %s, want resolved", auto.ID, got.State)
	}
}

// F-0885 crit. 6: a reject reopens each pending the feature paid (link and
// closed_at cleared, pending.reopened with the feature and the reason); a
// supersede transfers each to the successor (pending.transferred) — or
// reopens it when the successor is no longer live. Both in the transition's
// own commit.
func TestRejectReopensAndSupersedeTransfersPaidPendings(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)

	seedFeatureInState(t, repoDir, "F-0010", domain.StateCandidate)
	rej := seedPaid(t, repoDir, projDir, "Débito da rejeitada", origin.ID, "F-0010")
	out, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionReject, "", "ninguém vai fazer", nil)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := pendingState(t, repoDir, rej.ID); got.State != domain.PendingOpen || got.PromotedTo != "" || got.ClosedAt != nil {
		t.Fatalf("after reject %+v, want open, unlinked, no closed_at", got)
	}
	if pl := lastPayload(t, repoDir, rej.ID, "pending.reopened"); pl["feature"] != "F-0010" || pl["reason"] != "ninguém vai fazer" {
		t.Fatalf("pending.reopened payload = %v, want feature F-0010 and the reason", pl)
	}
	if !slices.Contains(lastCommitFiles(t, repoDir), "entities/pendings/"+rej.ID+".json") || len(out.Pendings) != 1 {
		t.Fatalf("reject commit %v / outcome %v, want the pending in both", lastCommitFiles(t, repoDir), out.Pendings)
	}

	seedFeatureInState(t, repoDir, "F-0011", domain.StateInDevelopment)
	seedFeatureInState(t, repoDir, "F-0012", domain.StateCandidate)
	tr := seedPaid(t, repoDir, projDir, "Débito transferido", origin.ID, "F-0011")
	if _, err := TransitionFeature(repoDir, projDir, "F-0011", domain.ActionSupersede, "F-0012", "a F-0012 faz melhor", nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if got := pendingState(t, repoDir, tr.ID); got.State != domain.PendingPromoted || got.PromotedTo != "F-0012" {
		t.Fatalf("after supersede %+v, want promoted to F-0012", got)
	}
	if pl := lastPayload(t, repoDir, tr.ID, "pending.transferred"); pl["feature"] != "F-0011" || pl["promoted_to"] != "F-0012" {
		t.Fatalf("pending.transferred payload = %v, want feature F-0011 -> F-0012", pl)
	}
	if files := lastCommitFiles(t, repoDir); !slices.Contains(files, "entities/pendings/"+tr.ID+".json") || !slices.Contains(files, "entities/features/F-0011.json") {
		t.Fatalf("supersede commit %v, want the feature and the pending", files)
	}

	seedFeatureInState(t, repoDir, "F-0013", domain.StateApproved)
	seedFeatureInState(t, repoDir, "F-0014", domain.StateDone)
	back := seedPaid(t, repoDir, projDir, "Débito sem sucessora viva", origin.ID, "F-0013")
	if _, err := TransitionFeature(repoDir, projDir, "F-0013", domain.ActionSupersede, "F-0014", "já entregue", nil); err != nil {
		t.Fatalf("supersede by a done feature: %v", err)
	}
	if got := pendingState(t, repoDir, back.ID); got.State != domain.PendingOpen || got.PromotedTo != "" {
		t.Fatalf("superseded by a done feature: %+v, want open (the debt is never lost)", got)
	}
}

// citeInResult appends a line citing ids to the feature's result.md, committed
// and pushed.
func citeInResult(t *testing.T, repoDir, featureID string, ids ...string) {
	t.Helper()
	path := FeatureDocPath(repoDir, featureID, "result.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nPendências pagas: "+strings.Join(ids, ", ")+"\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "cite "+strings.Join(ids, ",")); err != nil {
		t.Fatal(err)
	}
}

// F-0885 crit. 7: the complete refuses a result that does not cite each
// pending the feature pays, naming the missing ones and both ways out; it
// passes once the result cites them ([[P-…]] or P-…), or once the pending is
// unlinked.
func TestCompleteRefusesUncitedPaidPending(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	cited := seedPaid(t, repoDir, projDir, "Débito citado no result", origin.ID, "F-0010")
	missing := seedPaid(t, repoDir, projDir, "Débito esquecido no result", origin.ID, "F-0010")
	citeInResult(t, repoDir, "F-0010", cited.ID)

	before := backendHead(t, repoDir)
	_, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionComplete, "", "", nil)
	if !errors.Is(err, ErrPaidPendingUncited) {
		t.Fatalf("complete without citing %s: err = %v, want ErrPaidPendingUncited", missing.ID, err)
	}
	for _, want := range []string{missing.ID, "[[" + missing.ID + "]]", "cerne pending unlink " + missing.ID} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "unlink "+cited.ID) {
		t.Fatalf("refusal names the cited %s: %q", cited.ID, err.Error())
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("refused complete moved HEAD %s -> %s", before, after)
	}
	citeInResult(t, repoDir, "F-0010", "[["+missing.ID+"]]")
	if _, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete citing both: %v", err)
	}

	seedFeatureInState(t, repoDir, "F-0011", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	dropped := seedPaid(t, repoDir, projDir, "Débito que a feature não paga", origin.ID, "F-0011")
	if _, err := TransitionFeature(repoDir, projDir, "F-0011", domain.ActionComplete, "", "", nil); !errors.Is(err, ErrPaidPendingUncited) {
		t.Fatalf("complete of F-0011: err = %v, want ErrPaidPendingUncited", err)
	}
	if _, err := UnlinkPending(repoDir, projDir, dropped.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0011", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete after unlink: %v", err)
	}
}

// F-0885 r1 (critic): a citation counts only in the text a reader sees and only
// as the exact id: P-00011, an HTML comment or fenced code is no citation.
func TestCompleteCitationIsVisibleAndExact(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	origin := pendingOrigin(t, repoDir, projDir, "Origem", nil, nil)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	p := seedPaid(t, repoDir, projDir, "Débito pago", origin.ID, "F-0010")
	for _, cite := range []string{p.ID + "1", "<!-- [[" + p.ID + "]] -->", "\n~~~\n" + p.ID + "\n~~~", "\n```text\n" + p.ID + "\n```"} {
		citeInResult(t, repoDir, "F-0010", cite)
		if _, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionComplete, "", "", nil); !errors.Is(err, ErrPaidPendingUncited) {
			t.Fatalf("%q counted as a citation of %s: err = %v", cite, p.ID, err)
		}
	}
	citeInResult(t, repoDir, "F-0010", "[["+p.ID+"]]")
	if _, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete with a visible citation: %v", err)
	}
}
