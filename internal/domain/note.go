package domain

import (
	"errors"
	"fmt"
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
)

type NoteState string

const (
	NoteActive     NoteState = "active"
	NoteSuperseded NoteState = "superseded"
)

var noteTransitions = map[NoteState]map[Action]NoteState{
	NoteActive: {
		ActionSupersede: NoteSuperseded,
	},
	// Terminal state: explicitly present with no actions, so it reports
	// ErrInvalidTransition rather than ErrUnknownState.
	NoteSuperseded: {},
}

// TransitionNote returns the state reached by applying action to from.
func TransitionNote(from NoteState, action Action) (NoteState, error) {
	actions, ok := noteTransitions[from]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownState, from)
	}
	to, ok := actions[action]
	if !ok {
		return "", fmt.Errorf("%w: cannot %s from %s", ErrInvalidTransition, action, from)
	}
	return to, nil
}

// ValidFacets is the closed set of note facet values. The facet axis is fixed by
// decision (D-0002), not extensible at runtime — a value outside it is rejected.
var ValidFacets = []string{"infra", "code", "data", "process", "auth"}

// ConventionComponent is the reserved component slug that marks a note as a
// prescriptive convention (how to build) rather than descriptive knowledge
// (what was discovered) — F-0165. It rides the open component axis, so the
// closed facet set (D-0002) stays untouched.
const ConventionComponent = "convention"

// AtaComponent is the reserved component slug that marks a note as the
// governed projection of a war room's closing minutes (F-0238) — it rides
// the open component axis, same as ConventionComponent.
const AtaComponent = "ata"

// ErrUnknownFacet is returned by NormalizeFacets for a value not in ValidFacets.
var ErrUnknownFacet = errors.New("facet desconhecida")

func validFacet(f string) bool {
	for _, v := range ValidFacets {
		if v == f {
			return true
		}
	}
	return false
}

// NormalizeFacets lowercases and trims each facet, drops blanks, deduplicates
// (order-preserving) and validates against the closed ValidFacets set. A value
// outside the set yields ErrUnknownFacet (naming the offender) and no partial
// result, so an invalid tag set never half-applies.
func NormalizeFacets(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		f := strings.ToLower(strings.TrimSpace(raw))
		if f == "" {
			continue
		}
		if !validFacet(f) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownFacet, f)
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out, nil
}

// NormalizeComponents slugifies each component (lower, diacritics folded, kept to
// [a-z0-9-]), drops entries that slug to empty, and deduplicates (order-
// preserving). The component axis is curated but open (D-0002): values are
// normalized to kill the Gitea/gitea sprawl, never rejected.
func NormalizeComponents(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		c := slug.Make(raw)
		if c == "" {
			continue
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}
