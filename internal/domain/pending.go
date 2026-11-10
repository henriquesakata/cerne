package domain

import "fmt"

// PendingState is the lifecycle state of a pending (P-), a known delivery
// debt left by a concrete feature, task or epic (F-0322).
type PendingState string

const (
	PendingOpen     PendingState = "open"
	PendingResolved PendingState = "resolved"
	PendingDropped  PendingState = "dropped"
	PendingPromoted PendingState = "promoted"
)

// Pending actions (F-0322): resolve closes a debt that was paid, drop discards
// it (a reason is required at the app layer), promote turns it into a feature
// or task of the regular funnel. F-0885: a promoted pending follows the feature
// that pays it — reopen hands it back to open (the feature was rejected or the
// link undone), transfer moves it to the feature that superseded its own.
const (
	ActionResolve  Action = "resolve"
	ActionDrop     Action = "drop"
	ActionPromote  Action = "promote"
	ActionReopen   Action = "reopen"
	ActionTransfer Action = "transfer"
)

var pendingTransitions = map[PendingState]map[Action]PendingState{
	PendingOpen: {
		ActionResolve: PendingResolved,
		ActionDrop:    PendingDropped,
		ActionPromote: PendingPromoted,
	},
	// Promoted lives while the paying feature lives (F-0885): it leaves only by
	// the accept (resolve), a reject or unlink (reopen) or a supersede
	// (transfer to the successor).
	PendingPromoted: {
		ActionResolve:  PendingResolved,
		ActionReopen:   PendingOpen,
		ActionTransfer: PendingPromoted,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	PendingResolved: {},
	PendingDropped:  {},
}

// TransitionPending returns the state reached by applying action to from.
func TransitionPending(from PendingState, action Action) (PendingState, error) {
	actions, ok := pendingTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
