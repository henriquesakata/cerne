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
