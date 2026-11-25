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

// IsPlanDelivered reports whether an agent plan item is a delivered feature:
// the agent may map one straight to "done" as well as to "implemented".
func IsPlanDelivered(it PlanItem) bool {
	return it.Kind == "feature" && (it.State == "implemented" || it.State == "done")
}

// SetAnchoredDelivered is SetDelivered for the anchored items of an agent plan.
func SetAnchoredDelivered(items []AnchoredItem, state string) {
	for i := range items {
		if IsPlanDelivered(items[i].PlanItem) {
			items[i].State = state
		}
	}
}
