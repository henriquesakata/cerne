package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// lastIdeaPayload is the payload of id's latest event of the type (idea.*
// events are outside the funnel's ReadEvents).
func lastIdeaPayload(t *testing.T, repoDir, id, typ string) map[string]string {
	t.Helper()
	types, payloads := ideaEvents(t, repoDir, id)
	for k := len(types) - 1; k >= 0; k-- {
		if types[k] == typ {
			return payloads[k]
		}
	}
	t.Fatalf("no %s event for %s", typ, id)
	return nil
}

// F-0419 crit. 1: resolve closes an active idea with closed_at, the optional
// reason and idea.resolved; a closed idea is refused with the drop's sentinel.
func TestResolveIdea(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "já entregue", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveIdea(repoDir, projDir, strings.ToLower(i.ID), " feito na F-0001 ")
	if err != nil {
		t.Fatalf("ResolveIdea: %v", err)
	}
	stored, _ := store.LoadIdea(repoDir, i.ID)
	if got.State != domain.IdeaResolved || stored.State != domain.IdeaResolved || stored.Reason != "feito na F-0001" || stored.ClosedAt == nil {
		t.Fatalf("resolved = %+v, want resolved with reason and closed_at", stored)
	}
	if pl := lastIdeaPayload(t, repoDir, i.ID, "idea.resolved"); pl["reason"] != "feito na F-0001" {
		t.Fatalf("idea.resolved payload = %v, want the reason", pl)
	}
	if _, err := ResolveIdea(repoDir, projDir, i.ID, ""); !errors.Is(err, ErrIdeaNotActive) || !strings.Contains(err.Error(), "resolved") {
		t.Fatalf("second resolve err = %v, want ErrIdeaNotActive naming resolved", err)
	}
	d, _ := CreateIdea(repoDir, projDir, "descartável", "", nil, nil, "")
	if _, err := DropIdea(repoDir, projDir, d.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveIdea(repoDir, projDir, d.ID, ""); !errors.Is(err, ErrIdeaNotActive) {
		t.Fatalf("resolve of a dropped idea err = %v, want ErrIdeaNotActive", err)
	}
	if _, err := ResolveIdea(repoDir, projDir, "I-0099", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

// F-0419 crit. 4: link accepts only a live feature, records via=link and
// points the feature back at the idea; closed features refuse with nothing
// written.
func TestLinkIdeaOnlyToLiveFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	closedStates := []domain.FeatureState{domain.StateImplemented, domain.StateDone, domain.StateRejected, domain.StateSuperseded}
	for i, st := range closedStates {
		seedFeatureInState(t, repoDir, fmt.Sprintf("F-%04d", 10+i), st)
	}
	idea, err := CreateIdea(repoDir, projDir, "ideia a ligar", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	before := backendHead(t, repoDir)
	for i, st := range closedStates {
		id := fmt.Sprintf("F-%04d", 10+i)
		if _, err := LinkIdea(repoDir, projDir, idea.ID, id); !errors.Is(err, ErrIdeaTargetClosed) || !strings.Contains(err.Error(), string(st)) {
			t.Fatalf("link to %s (%s): err = %v, want ErrIdeaTargetClosed naming the state", id, st, err)
		}
	}
	if _, err := LinkIdea(repoDir, projDir, idea.ID, "F-0999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("link to an unknown feature: err = %v, want ErrNotFound", err)
	}
	if _, err := LinkIdea(repoDir, projDir, idea.ID, "xx"); err == nil {
		t.Fatal("link to a non-F id was accepted")
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("refused links moved HEAD %s -> %s", before, after)
	}

	for i, st := range []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment} {
		id := fmt.Sprintf("F-%04d", 20+i)
		seedFeatureInState(t, repoDir, id, st)
		q, _ := CreateIdea(repoDir, projDir, fmt.Sprintf("ideia %d", i), "", nil, nil, "")
		got, err := LinkIdea(repoDir, projDir, strings.ToLower(q.ID), strings.ToLower(id))
		if err != nil {
			t.Fatalf("link to %s (%s): %v", id, st, err)
		}
		if got.State != domain.IdeaPromoted || got.PromotedTo != id || got.ClosedAt == nil {
			t.Fatalf("linked = %+v, want promoted to %s", got, id)
		}
		if pl := lastIdeaPayload(t, repoDir, q.ID, "idea.promoted"); pl["promoted_to"] != id || pl["via"] != "link" {
			t.Fatalf("idea.promoted payload = %v, want promoted_to %s via link", pl, id)
		}
		if f, _ := store.LoadFeature(repoDir, id); f.FromIdea != q.ID {
			t.Fatalf("%s.from_idea = %q, want %s", id, f.FromIdea, q.ID)
		}
	}
	// Not a raw FSM error: the sentinel every manual move of a closed idea uses (critic F-0419 #6).
	if _, err := LinkIdea(repoDir, projDir, "I-0002", "F-0020"); !errors.Is(err, ErrIdeaNotActive) {
		t.Fatalf("link of an already promoted idea: err = %v, want ErrIdeaNotActive", err)
	}
}

// F-0419 crit. 4: unlink hands a promoted idea back to active, clears
// promoted_to and the feature's back reference; an idea whose feature is
// implemented refuses, and a manual resolve/drop of a linked idea is refused.
func TestUnlinkIdeaReopens(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateApproved)
	i, _ := CreateIdea(repoDir, projDir, "ligada e desligada", "", nil, nil, "")
	if _, err := LinkIdea(repoDir, projDir, i.ID, "F-0010"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveIdea(repoDir, projDir, i.ID, ""); !errors.Is(err, ErrIdeaNotActive) {
		t.Fatalf("resolve of a linked idea: err = %v, want ErrIdeaNotActive", err)
	}
	got, err := UnlinkIdea(repoDir, projDir, i.ID)
	if err != nil {
		t.Fatalf("UnlinkIdea: %v", err)
	}
	if got.State != domain.IdeaActive || got.PromotedTo != "" || got.ClosedAt != nil {
		t.Fatalf("unlinked = %+v, want active, no promoted_to, no closed_at", got)
	}
	if pl := lastIdeaPayload(t, repoDir, i.ID, "idea.reopened"); pl["feature"] != "F-0010" || pl["via"] != "unlink" {
		t.Fatalf("idea.reopened payload = %v, want feature F-0010 via unlink", pl)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0010"); f.FromIdea != "" {
		t.Fatalf("F-0010.from_idea = %q after unlink, want empty", f.FromIdea)
	}
	if _, err := UnlinkIdea(repoDir, projDir, i.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("unlink of an active idea: err = %v, want ErrInvalidTransition", err)
	}

	seedFeatureInState(t, repoDir, "F-0011", domain.StateImplemented)
	old, _ := CreateIdea(repoDir, projDir, "promovida e entregue", "", nil, nil, "")
	old.State, old.PromotedTo = domain.IdeaPromoted, "F-0011"
	if err := store.SaveIdea(repoDir, old); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed promoted "+old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlinkIdea(repoDir, projDir, old.ID); !errors.Is(err, ErrIdeaLinked) || !strings.Contains(err.Error(), "F-0011") {
		t.Fatalf("unlink to an implemented feature: err = %v, want ErrIdeaLinked naming F-0011", err)
	}
}
