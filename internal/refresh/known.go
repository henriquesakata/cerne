package refresh

import (
	_ "embed"
	"encoding/json"
	"slices"
)

//go:embed known_contents.json
var knownJSON []byte

// Contents is the generated list: the releases looked at (a reason when one
// was skipped) and, per slot, every hash some release wrote.
type Contents struct {
	Tags  map[string]string   `json:"tags"`
	Known map[string][]string `json:"known"`
}

var known = func() Contents {
	var c Contents
	if err := json.Unmarshal(knownJSON, &c); err != nil {
		panic("refresh: known_contents.json: " + err.Error())
	}
	return c
}()

// Known reports whether some release wrote sha at slot.
func Known(slot, sha string) bool { return slices.Contains(known.Known[slot], sha) }

// knownSlot reports whether any release wrote the slot.
func knownSlot(slot string) bool { _, ok := known.Known[slot]; return ok }

// missingContent names the slots whose current content the list lacks.
func missingContent(cur map[string]string, c Contents) []string {
	var out []string
	for slot, sha := range cur {
		if !slices.Contains(c.Known[slot], sha) {
			out = append(out, pathOf(slot))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
