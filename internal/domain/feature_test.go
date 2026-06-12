package domain

import (
	"errors"
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
