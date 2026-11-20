package refresh

import (
	"slices"
	"sort"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// State is what the rule says about one agent's managed files.
type State int

const (
	// UpToDate: nothing to do (also an agent with no managed file at all).
	UpToDate State = iota
	// Renewable: every item is current or one some release wrote, and some
	// differ or are missing: the set can be renewed without losing an edit.
	Renewable
	// Unrecognized: an item has content no release wrote, which a person may
	// have edited or a newer cerne written: nothing of the agent is touched.
	Unrecognized
)

// Verdict is the rule's answer for one agent; Unknown names the items (paths)
// that made it Unrecognized.
type Verdict struct {
	State   State
	Unknown []string
}

// Connected is the agents of a project: Claude Code always, plus every
// provider whose folder exists.
func Connected(projDir string) []hooks.Provider {
	return append([]hooks.Provider{hooks.ProviderClaude}, hooks.DetectProviders(projDir)...)
}

// Classify reads the project's managed items of agent p against what this
// build writes and the list of what releases wrote. It only reads files.
func Classify(projDir string, p hooks.Provider) Verdict {
	disk, want := Collect(projDir), Current()
	present := false
	var unknown []string
	renew := false
	for slot, sha := range disk {
		if agentOf(slot) != p {
			continue
		}
		cur, expected := want[slot]
		_, tuple := cutHash(slot)
		// A file nobody expects and no release wrote is the person's own.
		if !expected && !tuple && !knownSlot(slot) {
			continue
		}
		present = true
		switch {
		case expected && cur == sha:
		case Known(slot, sha):
			renew = renew || expected
		default:
			unknown = append(unknown, pathOf(slot))
		}
	}
	if !present {
		return Verdict{}
	}
	for slot := range want {
		if _, ok := disk[slot]; !ok && agentOf(slot) == p {
			renew = true
		}
	}
	sort.Strings(unknown)
	unknown = slices.Compact(unknown)
	switch {
	case len(unknown) > 0:
		return Verdict{State: Unrecognized, Unknown: unknown}
	case renew:
		return Verdict{State: Renewable}
	}
	return Verdict{}
}
