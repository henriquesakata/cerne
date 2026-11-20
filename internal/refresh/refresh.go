package refresh

import (
	"slices"
	"sort"
	"strings"

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

// isGuide says whether a slot is part of the authoring guide: the skill
// copies, Copilot's instructions and the pointer blocks of AGENTS.md/GEMINI.md.
func isGuide(slot string) bool {
	path := pathOf(slot)
	return strings.Contains(path, "/skills/") || strings.HasPrefix(path, ".github/instructions/") || path == "AGENTS.md" || path == "GEMINI.md"
}

// Classify reads the project's managed items of agent p against what this
// build writes and the list of what releases wrote. It only reads files. With
// withGuide false the guide's files are left out of the verdict (sync_skills
// owns them, F-0477). An agent is connected only when one of its expected files
// or one of its hook entries is on disk: a lone leftover file is not.
func Classify(projDir string, p hooks.Provider, withGuide bool) Verdict {
	want := Current()
	if len(want) == 0 { // the scratch install failed: say nothing rather than guess
		return Verdict{}
	}
	disk := Collect(projDir)
	present := false
	var unknown []string
	renew := false
	for slot, sha := range disk {
		if agentOf(slot) != p || (!withGuide && isGuide(slot)) {
			continue
		}
		cur, expected := want[slot]
		_, tuple := cutHash(slot)
		// A file nobody expects is the person's own (or a retired one).
		if !expected && !tuple {
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
		if _, ok := disk[slot]; !ok && agentOf(slot) == p && (withGuide || !isGuide(slot)) {
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
