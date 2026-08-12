package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestFeatureTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    FeatureState
		action  Action
		wantTo  FeatureState
		wantErr bool
	}{
		{"approve candidate", StateCandidate, ActionApprove, StateApproved, false},
		{"start approved", StateApproved, ActionStart, StateInDevelopment, false},
		{"complete in-dev", StateInDevelopment, ActionComplete, StateImplemented, false},
		{"reject candidate", StateCandidate, ActionReject, StateRejected, false},
		{"supersede implemented (Bills case)", StateImplemented, ActionSupersede, StateSuperseded, false},
		{"cannot approve twice", StateApproved, ActionApprove, "", true},
		{"cannot start candidate", StateCandidate, ActionStart, "", true},
		{"cannot supersede rejected", StateRejected, ActionSupersede, "", true},
		{"cannot reject implemented", StateImplemented, ActionReject, "", true},
		{"supersede candidate", StateCandidate, ActionSupersede, StateSuperseded, false},
		{"supersede approved", StateApproved, ActionSupersede, StateSuperseded, false},
		{"supersede in-dev", StateInDevelopment, ActionSupersede, StateSuperseded, false},
		{"superseded is terminal", StateSuperseded, ActionApprove, "", true},
		{"rejected is terminal", StateRejected, ActionApprove, "", true},
		// F-0169: revert walks an approval back — and only an approval.
		{"revert approved", StateApproved, ActionRevert, StateCandidate, false},
		{"cannot revert candidate", StateCandidate, ActionRevert, "", true},
		{"cannot revert in-dev", StateInDevelopment, ActionRevert, "", true},
		{"cannot revert implemented", StateImplemented, ActionRevert, "", true},
		{"cannot revert done", StateDone, ActionRevert, "", true},
		{"cannot revert rejected", StateRejected, ActionRevert, "", true},
		{"cannot revert superseded", StateSuperseded, ActionRevert, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Transition(c.from, c.action)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr && !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("err = %v, want errors.Is ErrInvalidTransition", err)
			}
			if !c.wantErr && got != c.wantTo {
				t.Fatalf("got %s, want %s", got, c.wantTo)
			}
		})
	}
}

func TestTitleEditable(t *testing.T) {
	cases := []struct {
		state FeatureState
		want  bool
	}{
		{StateCandidate, true},
		{StateApproved, true},
		{StateInDevelopment, true},
		{StateImplemented, false},
		{StateRejected, false},
		{StateSuperseded, false},
	}
	for _, c := range cases {
		if got := TitleEditable(c.state); got != c.want {
			t.Errorf("TitleEditable(%s) = %v, want %v", c.state, got, c.want)
		}
	}
}

func TestTransitionUnknownState(t *testing.T) {
	_, err := Transition("bogus", ActionApprove)
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownState", err)
	}
	if errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, must not be ErrInvalidTransition", err)
	}
}

// TestFeatureActionsCoversFSM (F-0269): the enumerator is only useful if it is
// complete — an action reachable in `transitions` but missing here would be a
// lifecycle verb the agent's refusal list never learns about.
func TestFeatureActionsCoversFSM(t *testing.T) {
	got := FeatureActions()
	for from, actions := range transitions {
		for a := range actions {
			if !slices.Contains(got, a) {
				t.Fatalf("FeatureActions() misses %q (reachable from %s): %v", a, from, got)
			}
		}
	}
	if !slices.IsSorted(got) {
		t.Fatalf("FeatureActions() = %v, want sorted", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Fatalf("FeatureActions() has duplicate %q: %v", got[i], got)
		}
	}
}
