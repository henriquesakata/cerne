package app

import (
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// WorkSessions attributes session bindings to the active works holding them
// (F-0361): a W- binding goes to that work, an F- binding to the active
// auto-opened work that references the feature. Each list is newest signal
// first. Works that are not active never appear.
func WorkSessions(works []store.Work, signals []SessionSignal) map[string][]SessionSignal {
	active := map[string]bool{}
	autoByFeature := map[string][]string{}
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		active[w.ID] = true
		if w.Auto {
			for _, r := range w.Refs {
				autoByFeature[r] = append(autoByFeature[r], w.ID)
			}
		}
	}
	out := map[string][]SessionSignal{}
	for _, s := range signals {
		ids := autoByFeature[s.Entity]
		if strings.HasPrefix(s.Entity, "W-") {
			ids = []string{s.Entity}
		}
		for _, id := range ids {
			if active[id] {
				out[id] = append(out[id], s)
			}
		}
	}
	for _, l := range out {
		sort.SliceStable(l, func(i, j int) bool { return l[i].LastSignal.After(l[j].LastSignal) })
	}
	return out
}

// MachineActor is this machine's actor pseudonym (F-0442), what a work started
// here by the same user carries.
func MachineActor() string { return defaultWorkActor() }
