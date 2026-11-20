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

// missingRelease says why the list does not cover tag, "" when it does.
func missingRelease(c Contents, tag string) string {
	reason, ok := c.Tags[tag]
	switch {
	case !ok:
		return "known_contents.json has no entry for " + tag + ": run `go run ./tools/knowncontents`"
	case reason != "":
		return tag + " is in known_contents.json as skipped: " + reason
	}
	return ""
}
