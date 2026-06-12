package domain

import (
	"errors"
	"testing"
)

func TestReferenceTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    ReferenceState
		action  Action
		wantTo  ReferenceState
		wantErr bool
	}{
		{"obsolete current", ReferenceCurrent, ActionObsolete, ReferenceObsolete, false},
		{"obsolete is terminal", ReferenceObsolete, ActionObsolete, "", true},
		{"cannot supersede current", ReferenceCurrent, ActionSupersede, "", true},
		{"cannot approve current", ReferenceCurrent, ActionApprove, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := TransitionReference(c.from, c.action)
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

func TestTransitionReferenceUnknownState(t *testing.T) {
	_, err := TransitionReference("bogus", ActionObsolete)
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownState", err)
	}
	if errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, must not be ErrInvalidTransition", err)
	}
}
