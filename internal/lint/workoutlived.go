package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// workOutlivedFeatureFindings flags an active work whose refs are all
// features already out of the live cycle (F-0341 crit. 8) — the planning
// works opened before the transitions learned to close them, or one another
// session held. Fix carries the closing command, the states as its note.
func workOutlivedFeatureFindings(works []store.Work, features []store.Feature) []Finding {
	state := make(map[string]domain.FeatureState, len(features))
	for _, f := range features {
		state[f.ID] = f.State
	}
	var findings []Finding
	for _, w := range works {
		if w.State != domain.WorkActive || len(w.Refs) == 0 {
			continue
		}
		var states []string
		for _, ref := range w.Refs {
			s, ok := state[ref]
			if !ok || liveFeatureStates[s] {
				states = nil
				break
			}
			if !slices.Contains(states, string(s)) {
				states = append(states, string(s))
			}
		}
		if states == nil {
			continue
		}
		fix := fmt.Sprintf("cerne work done %s --note %q", w.ID, "feature "+strings.Join(states, ", "))
		findings = append(findings, Finding{
			Rule:   RuleWorkOutlivedFeature,
			Entity: w.ID,
			Detail: fmt.Sprintf("active, but every feature it refs is closed (%s: %s) — run '%s'", strings.Join(w.Refs, ", "), strings.Join(states, ", "), fix),
			Fix:    fix,
		})
	}
	return findings
}
