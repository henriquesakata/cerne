package domain

import "fmt"

// WorkState is the lifecycle state of a work (agent activity) entity.
type WorkState string

const (
	WorkActive    WorkState = "active"
	WorkDone      WorkState = "done"
	WorkAbandoned WorkState = "abandoned"
)

// ActionAbandon moves an active work to abandoned (F-0130): the agent gave up
// on the activity rather than completing it. ActionComplete (shared with the
// feature FSM) moves it to done.
const ActionAbandon Action = "abandon"

var workTransitions = map[WorkState]map[Action]WorkState{
	WorkActive: {
		ActionComplete: WorkDone,
		ActionAbandon:  WorkAbandoned,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	WorkDone:      {},
	WorkAbandoned: {},
}

// TransitionWork returns the state reached by applying action to from.
func TransitionWork(from WorkState, action Action) (WorkState, error) {
	actions, ok := workTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
