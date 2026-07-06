package app

import (
	"fmt"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0123: starting a child feature pulls its ready ancestor epics into
// development in the same commit. These tests pin every acceptance criterion of
// the spec, tracked by number.

const realDocBody = "---\ncerne_id: %s\n---\n\n# Doc real\n\nConteúdo real o bastante para o doccheck contar como preenchido.\n"

// seedApproved creates a feature under parent (empty for none) and drives it to
// approved with a real plan, so a start gate would open. It does not start it.
func seedApproved(t *testing.T, repoDir, projDir, title, parent string) store.Feature {
	t.Helper()
	f, err := CreateFeature(repoDir, projDir, title, "", parent, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature %q: %v", title, err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, f.ID, "spec", realDoc(f.ID)); err != nil {
		t.Fatalf("save spec %s: %v", f.ID, err)
	}
	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionApprove, "", ""); err != nil {
		t.Fatalf("approve %s: %v", f.ID, err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, f.ID, "plan", realDoc(f.ID)); err != nil {
		t.Fatalf("save plan %s: %v", f.ID, err)
	}
	return reload(t, repoDir, f.ID)
}

func realDoc(id string) string { return fmt.Sprintf(realDocBody, id) }

func reload(t *testing.T, repoDir, id string) store.Feature {
	t.Helper()
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature %s: %v", id, err)
	}
	return f
}

func mustStart(t *testing.T, repoDir, projDir, id string) TransitionOutcome {
	t.Helper()
	out, err := TransitionFeature(repoDir, projDir, id, domain.ActionStart, "", "")
	if err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return out
}

// Crit.1 + Crit.7: a started child pulls an approved+planned parent into
// development in the same transition, and reports it in Outcome.Propagated with
// a feature.started event carrying the child as `via`.
func TestPropagateStartsReadyParent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)

	out := mustStart(t, repoDir, projDir, child.ID)

	if got := reload(t, repoDir, child.ID).State; got != domain.StateInDevelopment {
		t.Fatalf("child state = %s, want in_development", got)
	}
	if got := reload(t, repoDir, epic.ID).State; got != domain.StateInDevelopment {
		t.Fatalf("epic state = %s, want in_development (auto-started)", got)
	}
	if len(out.Propagated) != 1 || out.Propagated[0] != epic.ID {
		t.Fatalf("Propagated = %v, want [%s]", out.Propagated, epic.ID)
	}
	if !hasStartedVia(t, repoDir, epic.ID, child.ID) {
		t.Fatalf("no feature.started event for %s via %s", epic.ID, child.ID)
	}
}

// Crit.2: propagation walks the whole ancestor chain, nearest-first.
func TestPropagateWalksChain(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	grand := seedApproved(t, repoDir, projDir, "Épico avô", "")
	parent := seedApproved(t, repoDir, projDir, "Épico pai", grand.ID)
	child := seedApproved(t, repoDir, projDir, "Fase 1", parent.ID)

	out := mustStart(t, repoDir, projDir, child.ID)

	if got := reload(t, repoDir, parent.ID).State; got != domain.StateInDevelopment {
		t.Fatalf("parent state = %s, want in_development", got)
	}
	if got := reload(t, repoDir, grand.ID).State; got != domain.StateInDevelopment {
		t.Fatalf("grandparent state = %s, want in_development", got)
	}
	if len(out.Propagated) != 2 || out.Propagated[0] != parent.ID || out.Propagated[1] != grand.ID {
		t.Fatalf("Propagated = %v, want [%s %s] (nearest-first)", out.Propagated, parent.ID, grand.ID)
	}
}

// Crit.3: an approved epic without a real plan is NOT forced into development;
// the child start still succeeds and lint keeps flagging the epic.
func TestPropagateSkipsParentWithoutPlan(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	// Epic approved but no plan: create, spec, approve — stop before plan.
	epic, err := CreateFeature(repoDir, projDir, "Épico sem plano", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, epic.ID, "spec", realDoc(epic.ID)); err != nil {
		t.Fatalf("save spec: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, epic.ID, domain.ActionApprove, "", ""); err != nil {
		t.Fatalf("approve epic: %v", err)
	}
	child := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)

	out := mustStart(t, repoDir, projDir, child.ID)

	if got := reload(t, repoDir, epic.ID).State; got != domain.StateApproved {
		t.Fatalf("epic state = %s, want unchanged approved (no plan → not started)", got)
	}
	if len(out.Propagated) != 0 {
		t.Fatalf("Propagated = %v, want empty", out.Propagated)
	}
}

// Crit.4: a parent that is not approved stops the walk with no change —
// candidate is not auto-approved, an already-started parent is not touched.
func TestPropagateStopsAtNonApprovedParent(t *testing.T) {
	t.Run("candidate parent", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		epic, err := CreateFeature(repoDir, projDir, "Épico candidato", "", "", nil, nil)
		if err != nil {
			t.Fatalf("CreateFeature: %v", err)
		}
		child := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
		out := mustStart(t, repoDir, projDir, child.ID)
		if got := reload(t, repoDir, epic.ID).State; got != domain.StateCandidate {
			t.Fatalf("epic state = %s, want unchanged candidate", got)
		}
		if len(out.Propagated) != 0 {
			t.Fatalf("Propagated = %v, want empty", out.Propagated)
		}
	})

	t.Run("already in_development parent", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		epic := seedApproved(t, repoDir, projDir, "Épico", "")
		// Start the epic first (it has no parent, so nothing propagates), so it
		// legitimately sits in_development before the child starts.
		mustStart(t, repoDir, projDir, epic.ID)
		child := seedApproved(t, repoDir, projDir, "Fase 2", epic.ID)
		out := mustStart(t, repoDir, projDir, child.ID)
		if got := reload(t, repoDir, epic.ID).State; got != domain.StateInDevelopment {
			t.Fatalf("epic state = %s, want unchanged in_development", got)
		}
		if len(out.Propagated) != 0 {
			t.Fatalf("Propagated = %v, want empty", out.Propagated)
		}
	})

	t.Run("terminal parent", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		epic := seedApproved(t, repoDir, projDir, "Épico", "")
		child := seedApproved(t, repoDir, projDir, "Fase 1", epic.ID)
		// Supersede the epic (terminal) after the child is linked.
		other, err := CreateFeature(repoDir, projDir, "Substituto", "", "", nil, nil)
		if err != nil {
			t.Fatalf("CreateFeature other: %v", err)
		}
		if _, err := TransitionFeature(repoDir, projDir, epic.ID, domain.ActionSupersede, other.ID, "troca"); err != nil {
			t.Fatalf("supersede epic: %v", err)
		}
		out := mustStart(t, repoDir, projDir, child.ID)
		if got := reload(t, repoDir, epic.ID).State; got != domain.StateSuperseded {
			t.Fatalf("epic state = %s, want unchanged superseded", got)
		}
		if len(out.Propagated) != 0 {
			t.Fatalf("Propagated = %v, want empty", out.Propagated)
		}
	})
}

// Crit.5: only start propagates — approve of a child leaves the parent intact.
func TestNonStartActionDoesNotPropagate(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	epic := seedApproved(t, repoDir, projDir, "Épico", "")
	child, err := CreateFeature(repoDir, projDir, "Fase 1", "", epic.ID, nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature child: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, child.ID, "spec", realDoc(child.ID)); err != nil {
		t.Fatalf("save child spec: %v", err)
	}

	out, err := TransitionFeature(repoDir, projDir, child.ID, domain.ActionApprove, "", "")
	if err != nil {
		t.Fatalf("approve child: %v", err)
	}
	if got := reload(t, repoDir, epic.ID).State; got != domain.StateApproved {
		t.Fatalf("epic state = %s, want unchanged approved (approve must not propagate)", got)
	}
	if len(out.Propagated) != 0 {
		t.Fatalf("Propagated = %v, want empty for approve", out.Propagated)
	}
}

// Crit.6: a child with no parent, or a dangling parent link, starts cleanly
// with no propagation and no error.
func TestPropagateNoParentAndDangling(t *testing.T) {
	t.Run("no parent", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		child := seedApproved(t, repoDir, projDir, "Solta", "")
		out := mustStart(t, repoDir, projDir, child.ID)
		if len(out.Propagated) != 0 {
			t.Fatalf("Propagated = %v, want empty", out.Propagated)
		}
	})

	t.Run("dangling parent", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		child := seedApproved(t, repoDir, projDir, "Fase 1", "")
		child.Parent = "F-9999"
		if err := store.SaveFeature(repoDir, child); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		out := mustStart(t, repoDir, projDir, child.ID)
		if len(out.Propagated) != 0 {
			t.Fatalf("Propagated = %v, want empty for dangling parent", out.Propagated)
		}
	})
}

// Crit.8: a cyclic parent link terminates instead of looping forever.
func TestPropagateCycleTerminates(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a := seedApproved(t, repoDir, projDir, "A", "")
	b := seedApproved(t, repoDir, projDir, "B", "")
	// Corrupt the graph into a cycle A→B→A, bypassing the reparent validator.
	a.Parent = b.ID
	b.Parent = a.ID
	if err := store.SaveFeature(repoDir, a); err != nil {
		t.Fatalf("SaveFeature a: %v", err)
	}
	if err := store.SaveFeature(repoDir, b); err != nil {
		t.Fatalf("SaveFeature b: %v", err)
	}

	started, err := propagateStartToAncestors(repoDir, a.ID)
	if err != nil {
		t.Fatalf("propagateStartToAncestors: %v", err)
	}
	// It may start B (a's approved+planned parent) but must stop at the cycle
	// back to A rather than loop.
	if len(started) > 1 {
		t.Fatalf("started = %v, want at most one before the cycle breaks", started)
	}
}

func hasStartedVia(t *testing.T, repoDir, entityID, via string) bool {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, e := range evs {
		if e.EntityID == entityID && e.Type == "feature.started" && e.Payload["via"] == via {
			return true
		}
	}
	return false
}
