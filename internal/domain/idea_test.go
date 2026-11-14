package domain

import (
	"errors"
	"testing"
)

func TestTransitionIdea(t *testing.T) {
	cases := []struct {
		name   string
		from   IdeaState
		action Action
		to     IdeaState
		err    error
	}{
		{"active resolve", IdeaActive, ActionResolve, IdeaResolved, nil},
		{"active drop", IdeaActive, ActionDrop, IdeaDropped, nil},
		{"active promote", IdeaActive, ActionPromote, IdeaPromoted, nil},
		{"active reopen", IdeaActive, ActionReopen, "", ErrInvalidTransition},
		{"promoted resolve", IdeaPromoted, ActionResolve, IdeaResolved, nil},
		{"promoted reopen", IdeaPromoted, ActionReopen, IdeaActive, nil},
		{"promoted transfer", IdeaPromoted, ActionTransfer, IdeaPromoted, nil},
		{"promoted drop", IdeaPromoted, ActionDrop, "", ErrInvalidTransition},
		{"resolved terminal", IdeaResolved, ActionReopen, "", ErrInvalidTransition},
		{"dropped terminal", IdeaDropped, ActionPromote, "", ErrInvalidTransition},
		{"unknown state", IdeaState("x"), ActionDrop, "", ErrUnknownState},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := TransitionIdea(c.from, c.action)
			if !errors.Is(err, c.err) || got != c.to {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, err, c.to, c.err)
			}
		})
	}
}
