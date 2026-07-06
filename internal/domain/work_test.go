package domain

import (
	"errors"
	"testing"
)

func TestTransitionWork(t *testing.T) {
	cases := []struct {
		name    string
		from    WorkState
		action  Action
		want    WorkState
		wantErr error
	}{
		{"start done", WorkActive, ActionComplete, WorkDone, nil},
		{"start abandon", WorkActive, ActionAbandon, WorkAbandoned, nil},
		{"done terminal", WorkDone, ActionComplete, "", ErrInvalidTransition},
		{"done terminal abandon", WorkDone, ActionAbandon, "", ErrInvalidTransition},
		{"abandoned terminal", WorkAbandoned, ActionComplete, "", ErrInvalidTransition},
		{"unknown state", WorkState("bogus"), ActionComplete, "", ErrUnknownState},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TransitionWork(tc.from, tc.action)
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

func TestValidStatesWork(t *testing.T) {
	states, ok := ValidStates("work")
	if !ok {
		t.Fatal("ValidStates(\"work\") ok = false, want true")
	}
	want := []string{string(WorkActive), string(WorkDone), string(WorkAbandoned)}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i, s := range want {
		if states[i] != s {
			t.Fatalf("states[%d] = %s, want %s", i, states[i], s)
		}
	}
}
