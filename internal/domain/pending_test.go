package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestTransitionPending(t *testing.T) {
	cases := []struct {
		name    string
		from    PendingState
		action  Action
		want    PendingState
		wantErr error
	}{
		{"open resolve", PendingOpen, ActionResolve, PendingResolved, nil},
		{"open drop", PendingOpen, ActionDrop, PendingDropped, nil},
		{"open promote", PendingOpen, ActionPromote, PendingPromoted, nil},
		{"open complete is not a pending action", PendingOpen, ActionComplete, "", ErrInvalidTransition},
		{"resolved terminal", PendingResolved, ActionResolve, "", ErrInvalidTransition},
		{"dropped terminal", PendingDropped, ActionPromote, "", ErrInvalidTransition},
		{"promoted terminal", PendingPromoted, ActionDrop, "", ErrInvalidTransition},
		{"unknown state", PendingState("bogus"), ActionResolve, "", ErrUnknownState},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TransitionPending(tc.from, tc.action)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidStatesPending(t *testing.T) {
	states, ok := ValidStates("pending")
	if !ok {
		t.Fatal(`ValidStates("pending") ok = false, want true`)
	}
	want := []string{"open", "resolved", "dropped", "promoted"}
	if !slices.Equal(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}
