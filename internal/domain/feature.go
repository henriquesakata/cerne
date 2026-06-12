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

// ErrInvalidTransition is the sentinel for any disallowed state/action pair.
var ErrInvalidTransition = errors.New("invalid transition")

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
}

// Transition returns the state reached by applying action to from.
func Transition(from FeatureState, action Action) (FeatureState, error) {
	to, ok := transitions[from][action]
	if !ok {
		return "", fmt.Errorf("%w: %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}
