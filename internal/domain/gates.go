package domain

import (
	"errors"
	"strings"
)

// DocSet reports which lifecycle documents exist for a feature.
type DocSet struct {
	HasSpec, HasPlan, HasResult bool
}

var (
	ErrGateSpecRequired   = errors.New("spec document required")
	ErrGatePlanRequired   = errors.New("plan document required")
	ErrGateResultRequired = errors.New("result document required")
)

// IsFeatureID reports whether id names a feature-shaped entity: a legacy
// feature (F-), a declared epic (E-) or a task (T-). All three share the
// feature FSM, store and documents (F-0203).
func IsFeatureID(id string) bool {
	return strings.HasPrefix(id, "F-") || strings.HasPrefix(id, "E-") || strings.HasPrefix(id, "T-")
}

// CheckGate verifies the document required by action exists in docs.
//
// Gates are per-action: each action checks only its own required document.
// Cumulative document state is enforced by the state machine itself —
// reaching complete requires having passed the approve and start gates —
// not by CheckGate.
//
// The only id-dependent rule is the T- light gate (F-0203): a task's start
// needs no plan of its own — its why lives in the parent PRD. Nothing else
// ever derives from the prefix; epic behavior stays structural.
func CheckGate(id string, action Action, docs DocSet) error {
	switch action {
	case ActionApprove:
		if !docs.HasSpec {
			return ErrGateSpecRequired
		}
	case ActionStart:
		if !docs.HasPlan && !strings.HasPrefix(id, "T-") {
			return ErrGatePlanRequired
		}
	case ActionComplete:
		if !docs.HasResult {
			return ErrGateResultRequired
		}
	}
	return nil
}
