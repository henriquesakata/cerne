package domain

import (
	"errors"
	"testing"
)

func TestDecisionTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    DecisionState
		action  Action
		wantTo  DecisionState
		wantErr bool
	}{
		{"supersede active", DecisionActive, ActionSupersede, DecisionSuperseded, false},
		{"superseded is terminal", DecisionSuperseded, ActionSupersede, "", true},
		{"cannot approve active", DecisionActive, ActionApprove, "", true},
		{"cannot obsolete active", DecisionActive, ActionObsolete, "", true},
		{"retire active", DecisionActive, ActionRetire, DecisionRetired, false},
		{"retired is terminal", DecisionRetired, ActionRetire, "", true},
		{"superseded cannot retire", DecisionSuperseded, ActionRetire, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := TransitionDecision(c.from, c.action)
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

func TestTransitionDecisionUnknownState(t *testing.T) {
	_, err := TransitionDecision("bogus", ActionSupersede)
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownState", err)
	}
	if errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, must not be ErrInvalidTransition", err)
	}
}

// TestRetireDecisionRequiresReason pins F-0385 criteria 5 and 6 at the
// domain: retire moves an active decision to retired, and a blank reason is
// refused with the sentinel before any transition.
func TestRetireDecisionRequiresReason(t *testing.T) {
	for _, reason := range []string{"", "   "} {
		if _, err := RetireDecision(DecisionActive, reason); !errors.Is(err, ErrRetireReasonRequired) {
			t.Fatalf("RetireDecision(active, %q) err = %v, want ErrRetireReasonRequired", reason, err)
		}
	}
	got, err := RetireDecision(DecisionActive, "no longer applies")
	if err != nil || got != DecisionRetired {
		t.Fatalf("RetireDecision(active) = %q, %v; want retired", got, err)
	}
	if _, err := RetireDecision(DecisionSuperseded, "why"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("RetireDecision(superseded) err = %v, want ErrInvalidTransition", err)
	}
}
