package importer

import (
	"errors"
	"fmt"
)

// ErrInvalidDeliveredState is the sentinel for a delivered-state choice other
// than "implemented" or "done".
var ErrInvalidDeliveredState = errors.New("invalid delivered state")

// ResolveDeliveredState validates the state a feature imported as delivered
// enters with; no choice ("") means "implemented".
func ResolveDeliveredState(s string) (string, error) {
	switch s {
	case "":
		return "implemented", nil
	case "implemented", "done":
		return s, nil
	}
	return "", fmt.Errorf("%w %q: use implemented or done", ErrInvalidDeliveredState, s)
}

// SetDelivered rewrites, in place, the state of every feature candidate mapped
// as delivered ("implemented") to state.
func SetDelivered(cands []Candidate, state string) {
	for i := range cands {
		if cands[i].Kind == "feature" && cands[i].State == "implemented" {
			cands[i].State = state
		}
	}
}

// SetPlanDelivered is SetDelivered for agent plan items.
func SetPlanDelivered(items []PlanItem, state string) {
	for i := range items {
		if items[i].Kind == "feature" && items[i].State == "implemented" {
			items[i].State = state
		}
	}
}
