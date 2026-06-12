package domain

import "fmt"

type DecisionState string

const (
	DecisionActive     DecisionState = "active"
	DecisionSuperseded DecisionState = "superseded"
)

var decisionTransitions = map[DecisionState]map[Action]DecisionState{
	DecisionActive: {
		ActionSupersede: DecisionSuperseded,
	},
	// Terminal state: explicitly present with no actions, so it reports
	// ErrInvalidTransition rather than ErrUnknownState.
	DecisionSuperseded: {},
}

// TransitionDecision returns the state reached by applying action to from.
func TransitionDecision(from DecisionState, action Action) (DecisionState, error) {
	actions, ok := decisionTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
