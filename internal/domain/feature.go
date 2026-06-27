// Package domain holds the cerne feature lifecycle core.
package domain

import (
	"errors"
	"fmt"
)

type FeatureState string

type Action string

const (
	StateCandidate     FeatureState = "candidate"
	StateApproved      FeatureState = "approved"
	StateInDevelopment FeatureState = "in_development"
	StateImplemented   FeatureState = "implemented"
	StateRejected      FeatureState = "rejected"
	StateSuperseded    FeatureState = "superseded"
)

const (
	ActionApprove   Action = "approve"
	ActionStart     Action = "start"
	ActionComplete  Action = "complete"
	ActionReject    Action = "reject"
	ActionSupersede Action = "supersede"
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
		ActionStart:     StateInDevelopment,
		ActionSupersede: StateSuperseded,
	},
	StateInDevelopment: {
		ActionComplete:  StateImplemented,
		ActionSupersede: StateSuperseded,
	},
	StateImplemented: {
		ActionSupersede: StateSuperseded,
	},
	// Terminal states: explicitly present with no actions, so they report
	// ErrInvalidTransition rather than ErrUnknownState.
	StateRejected:   {},
	StateSuperseded: {},
}

// TitleEditable reports whether a feature's title may still be changed: true for
// the live states (candidate/approved/in_development), false for the terminal
// ones (implemented/rejected/superseded), whose records are frozen history.
func TitleEditable(s FeatureState) bool {
	return s != StateImplemented && s != StateRejected && s != StateSuperseded
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
