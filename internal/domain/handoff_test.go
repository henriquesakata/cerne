package domain

import (
	"errors"
	"testing"
)

// F-0368: a handoff is pending until it is taken, superseded or cancelled —
// three terminal states that answer "was the baton received".
func TestHandoffTransitions(t *testing.T) {
	cases := []struct {
		from   HandoffState
		action Action
		want   HandoffState
	}{
		{HandoffPending, ActionTake, HandoffTaken},
		{HandoffPending, ActionSupersede, HandoffSuperseded},
		{HandoffPending, ActionCancel, HandoffCancelled},
	}
	for _, c := range cases {
		if got, err := TransitionHandoff(c.from, c.action); err != nil || got != c.want {
			t.Fatalf("%s/%s = %s, %v; want %s", c.from, c.action, got, err, c.want)
		}
	}
	for _, from := range []HandoffState{HandoffTaken, HandoffSuperseded, HandoffCancelled} {
		for _, a := range []Action{ActionTake, ActionSupersede, ActionCancel} {
			if _, err := TransitionHandoff(from, a); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("%s/%s err = %v, want ErrInvalidTransition", from, a, err)
			}
		}
	}
	if _, err := TransitionHandoff("bogus", ActionTake); !errors.Is(err, ErrUnknownState) {
		t.Fatalf("unknown state err = %v", err)
	}
}
