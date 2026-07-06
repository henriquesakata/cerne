package domain

import (
	"errors"
	"testing"
)

// TestAcceptTransition pins the done state (F-0131): accept moves implemented
// to done (the human validation), every other origin refuses, and done is
// terminal — nothing leaves it.
func TestAcceptTransition(t *testing.T) {
	cases := []struct {
		name    string
		from    FeatureState
		action  Action
		wantTo  FeatureState
		wantErr bool
	}{
		{"accept implemented", StateImplemented, ActionAccept, StateDone, false},
		{"cannot accept candidate", StateCandidate, ActionAccept, "", true},
		{"cannot accept approved", StateApproved, ActionAccept, "", true},
		{"cannot accept in-dev", StateInDevelopment, ActionAccept, "", true},
		{"cannot accept rejected", StateRejected, ActionAccept, "", true},
		{"cannot accept superseded", StateSuperseded, ActionAccept, "", true},
		{"done is terminal (accept)", StateDone, ActionAccept, "", true},
		{"done is terminal (supersede)", StateDone, ActionSupersede, "", true},
		{"done is terminal (approve)", StateDone, ActionApprove, "", true},
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

// Done records are frozen history like the other terminal states.
func TestTitleEditableDone(t *testing.T) {
	if TitleEditable(StateDone) {
		t.Fatal("TitleEditable(done) = true, want false")
	}
}

// The importer's state authority must accept done as a legal feature state.
func TestValidStatesIncludeDone(t *testing.T) {
	states, ok := ValidStates("feature")
	if !ok {
		t.Fatal("feature kind unknown")
	}
	for _, s := range states {
		if s == string(StateDone) {
			return
		}
	}
	t.Fatalf("done missing from feature states: %v", states)
}
