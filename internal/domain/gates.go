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
// feature (F-), a declared epic (E-), a task (T-) or a bug report (B-, T-0006).
// All four share the
// feature FSM, store and documents (F-0203).
func IsFeatureID(id string) bool {
	for _, p := range featurePrefixes {
		if strings.HasPrefix(id, p+"-") {
			return true
		}
	}
	return false
}

// featurePrefixes is the one list of the feature family's id prefixes (B-0021).
// Every other place that recognizes feature ids asks IsFeatureID or builds its
// pattern from FeatureIDPrefix; a test fails on a hand-written copy.
var featurePrefixes = []string{"F", "E", "T", "B"}

// FeatureIDPrefix is a regexp group matching one feature-family prefix letter
// ("(?:F|E|T|B)"), derived from the same list as IsFeatureID. Callers add the
// dash, digits and boundaries (and (?i) when they need case-insensitivity).
func FeatureIDPrefix() string {
	return "(?:" + strings.Join(featurePrefixes, "|") + ")"
}

// FeaturePrefixesText lists the feature-family prefixes — every kind of card
// the board shows — for a message or help text: "F-, E-, T-, B-".
func FeaturePrefixesText() string {
	return strings.Join(featurePrefixes, "-, ") + "-"
}

// IsEpicID reports whether id names an epic (E-): a container of features, not
// a unit of work, so cost attribution does not take a mention of one as other
// work.
func IsEpicID(id string) bool {
	return strings.HasPrefix(id, "E-")
}

// CheckGate verifies the document required by action exists in docs.
//
// Gates are per-action: each action checks only its own required document.
// Cumulative document state is enforced by the state machine itself —
// reaching complete requires having passed the approve and start gates —
// not by CheckGate.
//
// The only id-dependent rule is the T-/B- light gate (F-0203, T-0006): a task's or bug's start
// needs no plan of its own — its why lives in the parent PRD. Nothing else
// ever derives from the prefix; epic behavior stays structural.
func CheckGate(id string, action Action, docs DocSet) error {
	switch action {
	case ActionApprove:
		if !docs.HasSpec {
			return ErrGateSpecRequired
		}
	case ActionStart:
		if !docs.HasPlan && !strings.HasPrefix(id, "T-") && !strings.HasPrefix(id, "B-") {
			return ErrGatePlanRequired
		}
	case ActionComplete:
		if !docs.HasResult {
			return ErrGateResultRequired
		}
	}
	return nil
}
