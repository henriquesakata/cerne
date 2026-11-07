package app

import (
	"os"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0214: completing the last alive child of an epic scaffolds the epic's
// result with a roster and reports the next step, without completing the
// epic itself — the close-side counterpart of propagateStartToAncestors.
// These tests pin every acceptance criterion of the spec, tracked by number.

func mustComplete(t *testing.T, repoDir, projDir, id string) TransitionOutcome {
	t.Helper()
	if err := SaveFeatureDoc(repoDir, projDir, id, "result", realDoc(id)+testGauntletTail); err != nil {
		t.Fatalf("save result %s: %v", id, err)
	}
	out, err := TransitionFeature(repoDir, projDir, id, domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
	return out
}

func epicResultBody(t *testing.T, repoDir, id string) string {
	t.Helper()
	data, err := readFeatureDoc(repoDir, id, "result.md")
	if err != nil {
		t.Fatalf("read result.md %s: %v", id, err)
	}
	return string(data)
}

// Crit.1: the last alive child's complete scaffolds the epic result with the
// children roster, reports the epic in Outcome, and (crit.3) leaves the epic
// in_development — the scaffold never completes it.
func TestEpicCloseScaffoldsOnLastAliveChildComplete(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child1 := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
	child2 := seedApproved(t, repoDir, projDir, "Fase 2", epic.ID)
	mustStart(t, repoDir, projDir, child1.ID) // propagates the epic into development
	mustStart(t, repoDir, projDir, child2.ID)

	// Crit.4: a live sibling still pending → no-op.
	out1 := mustComplete(t, repoDir, projDir, child1.ID)
	if out1.EpicReadyToClose != "" {
		t.Fatalf("EpicReadyToClose = %q on first complete, want empty (child2 still alive)", out1.EpicReadyToClose)
	}

	out2 := mustComplete(t, repoDir, projDir, child2.ID)
	if out2.EpicReadyToClose != epic.ID {
		t.Fatalf("EpicReadyToClose = %q, want %s", out2.EpicReadyToClose, epic.ID)
	}
	if got := reload(t, repoDir, epic.ID).State; got != domain.StateInDevelopment {
		t.Fatalf("epic state = %s, want unchanged in_development (scaffold never completes it)", got)
	}
	body := epicResultBody(t, repoDir, epic.ID)
	if !strings.Contains(body, child1.ID) || !strings.Contains(body, child2.ID) {
		t.Fatalf("epic result.md roster missing children:\n%s", body)
	}
	// The roster must stay scaffold (HTML comment): it must not open the
	// complete gate on its own (crit.3's invariant).
	data, err := readFeatureDoc(repoDir, epic.ID, "result.md")
	if err != nil {
		t.Fatalf("read result.md: %v", err)
	}
	if doccheck.HasRealContent(data) {
		t.Fatalf("epic result.md counts as real content after scaffold; roster must stay inside a comment")
	}
}

// Crit.1: a rejected sibling does not count as alive — its rejection frees the
// remaining child's complete to close the epic.
func TestEpicCloseTreatsRejectedChildAsNotAlive(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child1 := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
	mustStart(t, repoDir, projDir, child1.ID)

	child2, err := CreateFeature(repoDir, projDir, "Fase 2 (abandonada)", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child2: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, child2.ID, domain.ActionReject, "", "não necessária", nil); err != nil {
		t.Fatalf("reject child2: %v", err)
	}

	out := mustComplete(t, repoDir, projDir, child1.ID)
	if out.EpicReadyToClose != epic.ID {
		t.Fatalf("EpicReadyToClose = %q, want %s (rejected sibling is not alive)", out.EpicReadyToClose, epic.ID)
	}
}

// Crit.5: a result already filled in for the epic is never overwritten by the
// scaffold — the same no-op protection as the plan scaffold at approve.
func TestEpicCloseNeverOverwritesRealResult(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
	mustStart(t, repoDir, projDir, child.ID)

	real := realDoc(epic.ID) + "Conteúdo já escrito pelo autor.\n"
	if err := SaveFeatureDoc(repoDir, projDir, epic.ID, "result", real); err != nil {
		t.Fatalf("save real epic result: %v", err)
	}

	mustComplete(t, repoDir, projDir, child.ID)

	if got := epicResultBody(t, repoDir, epic.ID); got != real {
		t.Fatalf("epic result.md was overwritten:\ngot:  %q\nwant: %q", got, real)
	}
}

// Crit.4 (no parent): completing a feature with no parent never touches
// anything epic-shaped.
func TestEpicCloseNoopWithoutParent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	solo := seedApproved(t, repoDir, projDir, "Solta", "")
	mustStart(t, repoDir, projDir, solo.ID)

	out := mustComplete(t, repoDir, projDir, solo.ID)
	if out.EpicReadyToClose != "" {
		t.Fatalf("EpicReadyToClose = %q, want empty (no parent)", out.EpicReadyToClose)
	}
}

// readFeatureDoc reads a lifecycle document straight from the backend clone
// for assertions — a thin wrapper over FeatureDocPath.
func readFeatureDoc(repoDir, id, filename string) ([]byte, error) {
	return os.ReadFile(FeatureDocPath(repoDir, id, filename))
}

// F-0326: children whose stored parent is a legacy lowercase id are still the
// epic's children, so completing one while a sibling is live never declares
// the epic ready to close, and completing the last one does.
func TestEpicCloseSeesLegacyLowercaseChildren(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child1 := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
	child2 := seedApproved(t, repoDir, projDir, "Fase 2", epic.ID)
	mustStart(t, repoDir, projDir, child1.ID)
	mustStart(t, repoDir, projDir, child2.ID)
	for _, id := range []string{child1.ID, child2.ID} {
		f := reload(t, repoDir, id)
		f.Parent = strings.ToLower(epic.ID)
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", id, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "legacy lowercase parents"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	if out := mustComplete(t, repoDir, projDir, child1.ID); out.EpicReadyToClose != "" {
		t.Fatalf("EpicReadyToClose = %q with a live sibling, want empty", out.EpicReadyToClose)
	}
	if out := mustComplete(t, repoDir, projDir, child2.ID); out.EpicReadyToClose != epic.ID {
		t.Fatalf("EpicReadyToClose = %q on the last child, want %s", out.EpicReadyToClose, epic.ID)
	}
	body := epicResultBody(t, repoDir, epic.ID)
	if !strings.Contains(body, child1.ID) || !strings.Contains(body, child2.ID) {
		t.Fatalf("epic result.md roster misses the legacy children:\n%s", body)
	}
}
