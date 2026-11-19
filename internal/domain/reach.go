package domain

import "slices"

// mainPath is the lifecycle's forward path; rejected and superseded sit off it.
var mainPath = []FeatureState{StateCandidate, StateApproved, StateInDevelopment, StateImplemented, StateDone}

// FeatureAtOrPast reports whether a feature in cur has reached target (F-0460):
// on the forward path, any state at or after target; off it, only target itself.
func FeatureAtOrPast(cur, target FeatureState) bool {
	ci, ti := slices.Index(mainPath, cur), slices.Index(mainPath, target)
	if ci < 0 || ti < 0 {
		return cur == target
	}
	return ci >= ti
}

// FeatureReachable reports whether some sequence of lifecycle transitions
// leads from `from` to `to` (F-0460): the FSM itself is the source, so a new
// transition never leaves this behind.
func FeatureReachable(from, to FeatureState) bool {
	seen := map[FeatureState]bool{from: true}
	queue := []FeatureState{from}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if s == to {
			return true
		}
		for _, next := range transitions[s] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}
