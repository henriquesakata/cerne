package domain

import "fmt"

type RunbookState string

const (
	RunbookActive     RunbookState = "active"
	RunbookSuperseded RunbookState = "superseded"
)

var runbookTransitions = map[RunbookState]map[Action]RunbookState{
	RunbookActive: {
		ActionSupersede: RunbookSuperseded,
	},
	// Terminal state: explicitly present with no actions, so it reports
	// ErrInvalidTransition rather than ErrUnknownState.
	RunbookSuperseded: {},
}

// TransitionRunbook returns the state reached by applying action to from.
func TransitionRunbook(from RunbookState, action Action) (RunbookState, error) {
	actions, ok := runbookTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
