package domain

import "fmt"

// HandoffState is the lifecycle state of a handoff (HO-, F-0368): a baton
// passed from one work to whoever takes it.
type HandoffState string

const (
	HandoffPending    HandoffState = "pending"
	HandoffTaken      HandoffState = "taken"
	HandoffSuperseded HandoffState = "superseded"
	HandoffCancelled  HandoffState = "cancelled"
)

// Handoff actions (F-0368): take is the receiver confirming the baton; cancel
// withdraws it (a reason is required at the app layer). A newer handoff of the
// same work supersedes the pending one (ActionSupersede).
const (
	ActionTake   Action = "take"
	ActionCancel Action = "cancel"
)

var handoffTransitions = map[HandoffState]map[Action]HandoffState{
	HandoffPending: {
		ActionTake:      HandoffTaken,
		ActionSupersede: HandoffSuperseded,
		ActionCancel:    HandoffCancelled,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	HandoffTaken:      {},
	HandoffSuperseded: {},
	HandoffCancelled:  {},
}

// TransitionHandoff returns the state reached by applying action to from.
func TransitionHandoff(from HandoffState, action Action) (HandoffState, error) {
	actions, ok := handoffTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
