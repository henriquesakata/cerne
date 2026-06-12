package domain

import "fmt"

type ReferenceState string

const (
	ReferenceCurrent  ReferenceState = "current"
	ReferenceObsolete ReferenceState = "obsolete"
)

// ActionObsolete marks a reference as no longer current.
const ActionObsolete Action = "obsolete"

var referenceTransitions = map[ReferenceState]map[Action]ReferenceState{
	ReferenceCurrent: {
		ActionObsolete: ReferenceObsolete,
	},
	// Terminal state: explicitly present with no actions, so it reports
	// ErrInvalidTransition rather than ErrUnknownState.
	ReferenceObsolete: {},
}

// TransitionReference returns the state reached by applying action to from.
func TransitionReference(from ReferenceState, action Action) (ReferenceState, error) {
	actions, ok := referenceTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
