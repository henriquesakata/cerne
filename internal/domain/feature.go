// Package domain holds the cerne feature lifecycle core.
package domain

import (
	"errors"
	"fmt"
	"slices"
)

type FeatureState string

type Action string

const (
	StateCandidate     FeatureState = "candidate"
	StateApproved      FeatureState = "approved"
	StateInDevelopment FeatureState = "in_development"
	StateImplemented   FeatureState = "implemented"
	// StateDone is the post-implementation human acceptance (F-0131): the UI
	// shows implemented as "Ready" (awaiting validation) and done as "Done".
	StateDone       FeatureState = "done"
	StateRejected   FeatureState = "rejected"
	StateSuperseded FeatureState = "superseded"
)

const (
	ActionApprove   Action = "approve"
	ActionStart     Action = "start"
	ActionComplete  Action = "complete"
	// ActionAccept is the human validation gate (F-0131): implemented → done.
	ActionAccept    Action = "accept"
	ActionReject    Action = "reject"
	ActionSupersede Action = "supersede"
	// ActionRevert walks one step back in the approval flow (F-0169). v1 covers
	// only approved → candidate; future reverse steps reuse this action.
	ActionRevert Action = "revert"
)

// ErrInvalidTransition is the sentinel for a disallowed action on a known state.
var ErrInvalidTransition = errors.New("invalid transition")

// ErrUnknownState is the sentinel for a state absent from the transition table.
var ErrUnknownState = errors.New("unknown state")

var transitions = map[FeatureState]map[Action]FeatureState{
	StateCandidate: {
		ActionApprove:   StateApproved,
		ActionReject:    StateRejected,
		ActionSupersede: StateSuperseded,
	},
	StateApproved: {
		// Re-approval (F-0318): renews an approval whose spec changed; the
		// core refuses it while there is nothing to renew.
		ActionApprove:   StateApproved,
		ActionStart:     StateInDevelopment,
		ActionSupersede: StateSuperseded,
		ActionRevert:    StateCandidate,
	},
	StateInDevelopment: {
		ActionComplete:  StateImplemented,
		ActionSupersede: StateSuperseded,
	},
	StateImplemented: {
		ActionAccept:    StateDone,
		ActionSupersede: StateSuperseded,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	StateDone:       {},
	StateRejected:   {},
	StateSuperseded: {},
}

// FeatureActions returns every action the lifecycle FSM accepts, sorted and
// deduplicated. It exists so a caller can be exhaustive over the lifecycle
// without restating it: the agent's write boundary (F-0269) derives its refusal
// list from here, and its test fails the moment a new transition is added
// without a matching refusal — the alternative, a hand-kept list, is exactly
// what would silently hand the agent a human gate.
func FeatureActions() []Action {
	seen := map[Action]bool{}
	for _, actions := range transitions {
		for a := range actions {
			seen[a] = true
		}
	}
	out := make([]Action, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// TitleEditable reports whether a feature's title may still be changed: true for
// the live states (candidate/approved/in_development), false for the terminal
// ones (implemented/done/rejected/superseded), whose records are frozen history.
func TitleEditable(s FeatureState) bool {
	return s != StateImplemented && s != StateDone && s != StateRejected && s != StateSuperseded
}

// Transition returns the state reached by applying action to from.
func Transition(from FeatureState, action Action) (FeatureState, error) {
	actions, ok := transitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
