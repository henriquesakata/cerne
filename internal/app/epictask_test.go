package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// setFeatureState drives a feature's state directly and commits, so the next
// creation (which starts from a clean HEAD, F-0121) sees it.
func setFeatureState(t *testing.T, repoDir string, f store.Feature, s domain.FeatureState) {
	t.Helper()
	f.State = s
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "set state"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestCreateEpic(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	e, err := CreateEpicWithSpec(repoDir, projDir, "Meu épico", "", "", nil, nil, "", false)
	if err != nil {
		t.Fatalf("CreateEpicWithSpec: %v", err)
	}
	if e.ID != "E-0001" || e.State != domain.StateCandidate {
		t.Fatalf("epic = %+v, want E-0001 candidate", e)
	}
	got, err := store.LoadFeature(repoDir, "E-0001")
	if err != nil || got.State != domain.StateCandidate {
		t.Fatalf("LoadFeature = %+v, %v; want persisted candidate", got, err)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].Type != "feature.created" || evs[0].EntityID != "E-0001" {
		t.Fatalf("events = %+v, want one feature.created for E-0001", evs)
	}
}

// TestCreateTask pins criterion 2 (F-0203): a task is born approved — the
// human gate was already paid on the parent PRD — linked to its parent.
func TestCreateTask(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	prd, err := CreateFeature(repoDir, projDir, "PRD", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	setFeatureState(t, repoDir, prd, domain.StateApproved)

	tk, err := CreateTaskWithSpec(repoDir, projDir, "Primeiro passo", "", prd.ID, nil, nil, "", false)
	if err != nil {
		t.Fatalf("CreateTaskWithSpec: %v", err)
	}
	if tk.ID != "T-0001" || tk.State != domain.StateApproved || tk.Parent != prd.ID {
		t.Fatalf("task = %+v, want T-0001 approved under %s", tk, prd.ID)
	}
	got, err := store.LoadFeature(repoDir, "T-0001")
	if err != nil || got.State != domain.StateApproved || got.Parent != prd.ID {
		t.Fatalf("LoadFeature = %+v, %v; want persisted approved child", got, err)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.created" || last.EntityID != tk.ID || last.Payload["parent"] != prd.ID {
		t.Fatalf("event = %+v, want feature.created carrying parent", last)
	}
}

// TestCreateTaskUnderEpic pins the mixed-prefix link: T- under a declared E-.
func TestCreateTaskUnderEpic(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	e, err := CreateEpicWithSpec(repoDir, projDir, "Épico", "", "", nil, nil, "", false)
	if err != nil {
		t.Fatalf("CreateEpicWithSpec: %v", err)
	}
	setFeatureState(t, repoDir, e, domain.StateInDevelopment)
	tk, err := CreateTaskWithSpec(repoDir, projDir, "Passo", "", e.ID, nil, nil, "", false)
	if err != nil {
		t.Fatalf("CreateTaskWithSpec under epic: %v", err)
	}
	if tk.Parent != e.ID || tk.State != domain.StateApproved {
		t.Fatalf("task = %+v, want approved under %s", tk, e.ID)
	}
}

// TestCreateTaskRefusals pins criterion 3 (F-0203): no parent, unknown parent
// or an uncommitted parent refuses via sentinel with nothing written.
func TestCreateTaskRefusals(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	prd, err := CreateFeature(repoDir, projDir, "PRD candidato", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	rejected, err := CreateFeature(repoDir, projDir, "PRD rejeitado", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	setFeatureState(t, repoDir, rejected, domain.StateRejected)

	cases := []struct {
		name    string
		parent  string
		wantErr error
	}{
		{"no parent", "", ErrParentRequired},
		{"unknown parent", "F-9999", ErrParentNotFound},
		{"candidate parent", prd.ID, ErrParentNotCommitted},
		{"terminal parent", rejected.ID, ErrParentTerminal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CreateTaskWithSpec(repoDir, projDir, "Passo", "", c.parent, nil, nil, "", false)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want errors.Is %v", err, c.wantErr)
			}
			if _, err := store.LoadFeature(repoDir, "T-0001"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("a refused task must write nothing, LoadFeature err = %v", err)
			}
		})
	}
}

// TestTaskStartWithoutPlan pins criterion 4 (F-0203) end to end: a T- starts
// with no plan of its own (gate waived, auto-work opens — the normal F-0146
// flow), but completing with no real result still refuses.
func TestTaskStartWithoutPlan(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "T-0001", domain.StateApproved, "spec.md")

	out, err := TransitionFeature(repoDir, projDir, "T-0001", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("start of a task without plan must pass: %v", err)
	}
	if out.To != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development", out.To)
	}
	ws := loadWorks(t, repoDir)
	if len(ws) != 1 || ws[0].Refs[0] != "T-0001" || !ws[0].Auto {
		t.Fatalf("works = %+v, want one auto work ref T-0001", ws)
	}
	// The result stub scaffolded at start is not real content: complete refuses.
	if _, err := TransitionFeature(repoDir, projDir, "T-0001", domain.ActionComplete, "", "", nil); !errors.Is(err, domain.ErrGateResultRequired) {
		t.Fatalf("complete err = %v, want ErrGateResultRequired", err)
	}
}

// TestWorkRefAcceptsTask pins the prefix routing (F-0203): --ref T-/E- resolve
// as features in work registration.
func TestWorkRefAcceptsTask(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "T-0001", domain.StateApproved)
	seedFeatureInState(t, repoDir, "E-0001", domain.StateApproved)
	w, err := StartWork(repoDir, projDir, "Investigação", []string{"T-0001", "E-0001"}, "", false, nil)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if len(w.Refs) != 2 {
		t.Fatalf("refs = %+v, want both kept", w.Refs)
	}
}

// TestReparentMixedPrefixes pins criterion 7 (F-0203): reparent accepts a T-
// under an F- and under an E- (ValidateParentLink untouched).
func TestReparentMixedPrefixes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved)
	seedFeatureInState(t, repoDir, "E-0001", domain.StateApproved)
	seedFeatureInState(t, repoDir, "T-0001", domain.StateApproved)

	if err := SetFeatureParent(repoDir, projDir, "T-0001", "F-0001"); err != nil {
		t.Fatalf("reparent T- under F-: %v", err)
	}
	if err := SetFeatureParent(repoDir, projDir, "T-0001", "E-0001"); err != nil {
		t.Fatalf("reparent T- under E-: %v", err)
	}
	got, err := store.LoadFeature(repoDir, "T-0001")
	if err != nil || got.Parent != "E-0001" {
		t.Fatalf("parent = %q, %v; want E-0001", got.Parent, err)
	}
}
