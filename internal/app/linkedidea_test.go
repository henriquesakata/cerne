package app

import (
	"errors"
	"os"
	"slices"
	"strings"
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
	if got := loadIdea(t, repoDir, closed.ID); got.State != domain.IdeaResolved || got.UpdatedAt != closed.UpdatedAt {
		t.Fatalf("already-closed idea = %+v, want untouched", got) // critic F-0419 #8
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

// Critic F-0419 #1: an idea promoted before F-0419 never saw its feature's
// accept or reject. The migration reconciles it: done resolves it, rejected
// reopens it; a live feature keeps it promoted. A second run moves nothing.
func TestMigrateFollowsTheFeatureOfAStrandedIdea(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateDone)
	seedFeatureInState(t, repoDir, "F-0011", domain.StateRejected)
	seedFeatureInState(t, repoDir, "F-0012", domain.StateApproved)
	done := seedLinkedIdea(t, repoDir, projDir, "virou feature aceita", "F-0010")
	rejected := seedLinkedIdea(t, repoDir, projDir, "virou feature rejeitada", "F-0011")
	live := seedLinkedIdea(t, repoDir, projDir, "virou feature viva", "F-0012")

	ids, err := MigrateIdeas(repoDir, projDir, true)
	if err != nil || !slices.Equal(ids, []string{done.ID, rejected.ID}) {
		t.Fatalf("dry run = %v, %v; want [%s %s]", ids, err, done.ID, rejected.ID)
	}
	if got := loadIdea(t, repoDir, done.ID); got.State != domain.IdeaPromoted {
		t.Fatalf("dry run wrote: %+v", got)
	}
	if ids, err = MigrateIdeas(repoDir, projDir, false); err != nil || !slices.Equal(ids, []string{done.ID, rejected.ID}) {
		t.Fatalf("migrate = %v, %v", ids, err)
	}
	if got := loadIdea(t, repoDir, done.ID); got.State != domain.IdeaResolved || got.ClosedAt == nil {
		t.Fatalf("idea of a done feature = %+v, want resolved", got)
	}
	if got := loadIdea(t, repoDir, rejected.ID); got.State != domain.IdeaActive || got.PromotedTo != "" {
		t.Fatalf("idea of a rejected feature = %+v, want active again", got)
	}
	if got := loadIdea(t, repoDir, live.ID); got.State != domain.IdeaPromoted || got.PromotedTo != "F-0012" {
		t.Fatalf("idea of a live feature = %+v, want untouched", got)
	}
	if ids, err = MigrateIdeas(repoDir, projDir, false); err != nil || len(ids) != 0 {
		t.Fatalf("second run = %v, %v; want nothing", ids, err)
	}
}

// Critic F-0419 #1: a done feature already resolved the idea it carried (or
// will, on migrate); unlinking it would reopen accepted work.
func TestUnlinkIdeaRefusesADoneFeature(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateDone)
	i := seedLinkedIdea(t, repoDir, projDir, "ideia de feature aceita", "F-0010")
	if _, err := UnlinkIdea(repoDir, projDir, i.ID); !errors.Is(err, ErrIdeaLinked) {
		t.Fatalf("unlink from a done feature = %v, want ErrIdeaLinked", err)
	}
}

// Critic F-0419 #6: link refuses a closed idea with the same sentinel as
// resolve, drop and promote — not a raw FSM error naming "promote".
func TestLinkIdeaRefusesAClosedIdeaWithErrIdeaNotActive(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateApproved)
	i, err := CreateIdea(repoDir, projDir, "uma ideia", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveIdea(repoDir, projDir, i.ID, "feita"); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkIdea(repoDir, projDir, i.ID, "F-0010"); !errors.Is(err, ErrIdeaNotActive) {
		t.Fatalf("link a resolved idea = %v, want ErrIdeaNotActive", err)
	}
}

// Critic F-0419 #8: a linked idea is not dropped by hand either — it follows
// its feature.
func TestDropRefusesALinkedIdea(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0010", domain.StateApproved)
	i := seedLinkedIdea(t, repoDir, projDir, "ideia vinculada", "F-0010")
	if _, err := DropIdea(repoDir, projDir, i.ID, "x"); !errors.Is(err, ErrIdeaNotActive) {
		t.Fatalf("drop a linked idea = %v, want ErrIdeaNotActive", err)
	}
}

// Critic F-0419 #2: promote --title keeps the idea's own title in the
// candidate's context, so the sentence the idea said is not lost.
func TestPromoteIdeaWithATitleKeepsTheIdeaTitle(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	i, err := CreateIdea(repoDir, projDir, "Exportar o kanban", "em CSV", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	_, f, err := PromoteIdea(repoDir, projDir, i.ID, "Exportar o kanban v2", "", false)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(spec), "Exportar o kanban\n") || !strings.Contains(string(spec), "em CSV") {
		t.Fatalf("spec context lost the idea's title or body:\n%s", spec)
	}
}
