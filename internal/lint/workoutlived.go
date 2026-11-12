package lint

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// closingEvent names the event that moves a feature into each closed state.
var closingEvent = map[domain.FeatureState]string{
	domain.StateImplemented: "feature.completed",
	domain.StateDone:        "feature.accepted",
	domain.StateRejected:    "feature.rejected",
	domain.StateSuperseded:  "feature.superseded",
}

// workOutlivedFeatureFindings flags an active work whose refs are all
// features already out of the live cycle (F-0341 crit. 8) — the planning
// works opened before the transitions learned to close them, or one another
// session held. Only a work created before each ref's last move into its
// current state outlived it: one opened after (an errata, the QA of an
// accept) is live work the Fix would close, and with no such event logged
// (an imported feature) no work can predate it. Fix carries the closing
// command, the states as its note.
func workOutlivedFeatureFindings(works []store.Work, features []store.Feature, events []domain.Event) []Finding {
	state := make(map[string]domain.FeatureState, len(features))
	for _, f := range features {
		state[f.ID] = f.State
	}
	closedAt := map[string]time.Time{}
	for _, ev := range events {
		if s, ok := state[ev.EntityID]; ok && ev.Type == closingEvent[s] && ev.At.After(closedAt[ev.EntityID]) {
			closedAt[ev.EntityID] = ev.At
		}
	}
	var findings []Finding
	for _, w := range works {
		if w.State != domain.WorkActive || len(w.Refs) == 0 {
			continue
		}
		var states []string
		for _, ref := range w.Refs {
			s, ok := state[ref]
			if !ok || liveFeatureStates[s] || !w.CreatedAt.Before(closedAt[ref]) {
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
