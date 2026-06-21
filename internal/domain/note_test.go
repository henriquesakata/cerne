package domain

import (
	"errors"
	"testing"
)

func TestNoteTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    NoteState
		action  Action
		wantTo  NoteState
		wantErr bool
	}{
		{"supersede active", NoteActive, ActionSupersede, NoteSuperseded, false},
		{"superseded is terminal", NoteSuperseded, ActionSupersede, "", true},
		{"cannot approve active", NoteActive, ActionApprove, "", true},
		{"cannot obsolete active", NoteActive, ActionObsolete, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := TransitionNote(c.from, c.action)
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

func TestTransitionNoteUnknownState(t *testing.T) {
	_, err := TransitionNote("bogus", ActionSupersede)
	if !errors.Is(err, ErrUnknownState) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownState", err)
	}
	if errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("err = %v, must not be ErrInvalidTransition", err)
	}
}
