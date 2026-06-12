package domain

import "errors"

// DocSet reports which lifecycle documents exist for a feature.
type DocSet struct {
	HasSpec, HasPlan, HasResult bool
}

var (
	ErrGateSpecRequired   = errors.New("spec document required")
	ErrGatePlanRequired   = errors.New("plan document required")
	ErrGateResultRequired = errors.New("result document required")
)

// CheckGate verifies the document required by action exists in docs.
func CheckGate(action Action, docs DocSet) error {
	switch action {
	case ActionApprove:
		if !docs.HasSpec {
			return ErrGateSpecRequired
		}
	case ActionStart:
		if !docs.HasPlan {
			return ErrGatePlanRequired
		}
	case ActionComplete:
		if !docs.HasResult {
			return ErrGateResultRequired
		}
	}
	return nil
}
