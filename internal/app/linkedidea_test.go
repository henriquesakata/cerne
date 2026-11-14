package app

import (
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedLinkedIdea captures an idea and points it at featureID directly in the
// store — the state a link or promote leaves — committed and pushed.
func seedLinkedIdea(t *testing.T, repoDir, projDir, title, featureID string) store.Idea {
	t.Helper()
	i, err := CreateIdea(repoDir, projDir, title, "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	i.State, i.PromotedTo = domain.IdeaPromoted, featureID
	if err := store.SaveIdea(repoDir, i); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed linked "+i.ID); err != nil {
		t.Fatal(err)
	}
	return i
}

func loadIdea(t *testing.T, repoDir, id string) store.Idea {
	t.Helper()
	i, err := store.LoadIdea(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// F-0419 crit. 4: the accept resolves every idea the feature carries, in the
// accept's own commit; an idea of another feature, or already closed, is
// untouched. The auto-accept of a complete resolves them too.
func TestAcceptResolvesLinkedIdeas(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateImplemented)
	seedFeatureInState(t, repoDir, "F-0011", domain.StateApproved)
	i1 := seedLinkedIdea(t, repoDir, projDir, "primeira ideia", "F-0010")
	i2 := seedLinkedIdea(t, repoDir, projDir, "segunda ideia", "F-0010")
	other := seedLinkedIdea(t, repoDir, projDir, "ideia de outra feature", "F-0011")
	closed := seedLinkedIdea(t, repoDir, projDir, "ideia já resolvida", "F-0010")
	closed.State = domain.IdeaResolved
	if err := store.SaveIdea(repoDir, closed); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed closed "+closed.ID); err != nil {
		t.Fatal(err)
	}

	out, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionAccept, "", "", nil)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	for _, i := range []store.Idea{i1, i2} {
		got := loadIdea(t, repoDir, i.ID)
		if got.State != domain.IdeaResolved || got.PromotedTo != "F-0010" || got.ClosedAt == nil {
			t.Fatalf("%s = %+v, want resolved, still pointing at F-0010", i.ID, got)
		}
		if pl := lastIdeaPayload(t, repoDir, i.ID, "idea.resolved"); pl["feature"] != "F-0010" {
			t.Fatalf("idea.resolved payload = %v, want feature F-0010", pl)
		}
	}
	files := lastCommitFiles(t, repoDir)
	for _, want := range []string{"entities/features/F-0010.json", "entities/ideas/" + i1.ID + ".json", "entities/ideas/" + i2.ID + ".json"} {
		if !slices.Contains(files, want) {
			t.Fatalf("the accept's commit lacks %s: %v", want, files)
		}
	}
	if !slices.Equal(out.Ideas, []string{i1.ID + ": promoted -> resolved", i2.ID + ": promoted -> resolved"}) {
		t.Fatalf("outcome ideas = %v, want both resolved", out.Ideas)
	}
	if got := loadIdea(t, repoDir, other.ID); got.State != domain.IdeaPromoted || got.PromotedTo != "F-0011" {
		t.Fatalf("idea of another feature = %+v, want untouched", got)
	}

	cfg := project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Accept: project.GateAuto}}
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	seedFeatureInState(t, repoDir, "F-0012", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	auto := seedLinkedIdea(t, repoDir, projDir, "ideia do auto-accept", "F-0012")
	if _, err := TransitionFeature(repoDir, projDir, "F-0012", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete with auto accept: %v", err)
	}
	if got := loadIdea(t, repoDir, auto.ID); got.State != domain.IdeaResolved {
		t.Fatalf("auto-accept left %s %s, want resolved", auto.ID, got.State)
	}
}

// F-0419 crit. 4: a reject reopens each idea the feature carried (active,
// promoted_to and closed_at cleared, idea.reopened with the feature and the
// reason); a supersede transfers each to the successor — or reopens it when
// the successor is not live. Both in the transition's own commit.
func TestRejectReopensAndSupersedeTransfersLinkedIdeas(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	seedFeatureInState(t, repoDir, "F-0010", domain.StateCandidate)
	rej := seedLinkedIdea(t, repoDir, projDir, "ideia da rejeitada", "F-0010")
	out, err := TransitionFeature(repoDir, projDir, "F-0010", domain.ActionReject, "", "ninguém vai fazer", nil)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := loadIdea(t, repoDir, rej.ID); got.State != domain.IdeaActive || got.PromotedTo != "" || got.ClosedAt != nil {
		t.Fatalf("after reject %+v, want active, unlinked, no closed_at", got)
	}
	if pl := lastIdeaPayload(t, repoDir, rej.ID, "idea.reopened"); pl["feature"] != "F-0010" || pl["reason"] != "ninguém vai fazer" {
		t.Fatalf("idea.reopened payload = %v, want feature F-0010 and the reason", pl)
	}
	if !slices.Contains(lastCommitFiles(t, repoDir), "entities/ideas/"+rej.ID+".json") || len(out.Ideas) != 1 {
		t.Fatalf("reject commit %v / outcome %v, want the idea in both", lastCommitFiles(t, repoDir), out.Ideas)
	}

	seedFeatureInState(t, repoDir, "F-0011", domain.StateInDevelopment)
	seedFeatureInState(t, repoDir, "F-0012", domain.StateCandidate)
	tr := seedLinkedIdea(t, repoDir, projDir, "ideia transferida", "F-0011")
	if _, err := TransitionFeature(repoDir, projDir, "F-0011", domain.ActionSupersede, "F-0012", "a F-0012 faz melhor", nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if got := loadIdea(t, repoDir, tr.ID); got.State != domain.IdeaPromoted || got.PromotedTo != "F-0012" {
		t.Fatalf("after supersede %+v, want promoted to F-0012", got)
	}
	if pl := lastIdeaPayload(t, repoDir, tr.ID, "idea.transferred"); pl["feature"] != "F-0011" || pl["promoted_to"] != "F-0012" {
		t.Fatalf("idea.transferred payload = %v, want feature F-0011 -> F-0012", pl)
	}
	if files := lastCommitFiles(t, repoDir); !slices.Contains(files, "entities/ideas/"+tr.ID+".json") || !slices.Contains(files, "entities/features/F-0011.json") {
		t.Fatalf("supersede commit %v, want the feature and the idea", files)
	}

	seedFeatureInState(t, repoDir, "F-0013", domain.StateApproved)
	seedFeatureInState(t, repoDir, "F-0014", domain.StateDone)
	back := seedLinkedIdea(t, repoDir, projDir, "ideia sem sucessora viva", "F-0013")
	if _, err := TransitionFeature(repoDir, projDir, "F-0013", domain.ActionSupersede, "F-0014", "já entregue", nil); err != nil {
		t.Fatalf("supersede by a done feature: %v", err)
	}
	if got := loadIdea(t, repoDir, back.ID); got.State != domain.IdeaActive || got.PromotedTo != "" {
		t.Fatalf("superseded by a done feature: %+v, want active (the idea is never lost)", got)
	}
}
