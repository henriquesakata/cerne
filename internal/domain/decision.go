package domain

import (
	"errors"
	"fmt"
	"strings"
)

type DecisionState string

const (
	DecisionActive     DecisionState = "active"
	DecisionSuperseded DecisionState = "superseded"
	// DecisionRetired: left the current state with no successor (F-0385) —
	// history, like superseded.
	DecisionRetired DecisionState = "retired"
)

// ActionRetire moves a decision to history with no successor (F-0385).
const ActionRetire Action = "retire"

// ErrRetireReasonRequired is the sentinel for a retire without a reason.
var ErrRetireReasonRequired = errors.New("retire requires a non-empty reason")

var decisionTransitions = map[DecisionState]map[Action]DecisionState{
	DecisionActive: {
		ActionSupersede: DecisionSuperseded,
		ActionRetire:    DecisionRetired,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	DecisionSuperseded: {},
	DecisionRetired:    {},
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

// RetireDecision returns the state reached by retiring a decision in from. A
// blank reason is ErrRetireReasonRequired: the event carries why it left.
func RetireDecision(from DecisionState, reason string) (DecisionState, error) {
	if strings.TrimSpace(reason) == "" {
		return "", ErrRetireReasonRequired
	}
	return TransitionDecision(from, ActionRetire)
}
