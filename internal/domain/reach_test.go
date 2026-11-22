package domain

import "testing"

// TestFeatureReachable (F-0460 criterion 4): a target is reachable when some
// path of the lifecycle FSM leads to it — including the revert back to
// candidate, so an approved feature can still end up rejected.
func TestFeatureReachable(t *testing.T) {
	cases := []struct {
		from, to FeatureState
		want     bool
	}{
		{StateCandidate, StateApproved, true},
		{StateCandidate, StateDone, true},
		{StateApproved, StateRejected, true}, // revert, then reject
		{StateInDevelopment, StateRejected, false},
		{StateInDevelopment, StateApproved, false},
		{StateImplemented, StateSuperseded, true},
		{StateDone, StateSuperseded, true}, // F-0499: revert, then supersede
		{StateDone, StateInDevelopment, true},
		{StateDone, StateRejected, false},
		{StateRejected, StateApproved, false},
		{StateSuperseded, StateDone, false},
		{StateApproved, StateApproved, true},
	}
	for _, c := range cases {
		if got := FeatureReachable(c.from, c.to); got != c.want {
			t.Errorf("FeatureReachable(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

// TestFeatureAtOrPast (F-0460 criterion 2): on the main path, a state at or
// after the target counts as there; off-path targets only by equality.
func TestFeatureAtOrPast(t *testing.T) {
	cases := []struct {
		cur, target FeatureState
		want        bool
	}{
		{StateApproved, StateApproved, true},
		{StateInDevelopment, StateApproved, true},
		{StateDone, StateImplemented, true},
		{StateCandidate, StateApproved, false},
		{StateRejected, StateApproved, false},
		{StateRejected, StateRejected, true},
		{StateDone, StateRejected, false},
		{StateSuperseded, StateSuperseded, true},
	}
	for _, c := range cases {
		if got := FeatureAtOrPast(c.cur, c.target); got != c.want {
			t.Errorf("FeatureAtOrPast(%s, %s) = %v, want %v", c.cur, c.target, got, c.want)
		}
	}
}
