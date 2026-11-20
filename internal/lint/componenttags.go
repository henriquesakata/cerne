package lint

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// The tag-cleanup rules (E-0005 criterion 18): advisory, each reported as one
// line with the count, for whoever curates the vocabulary. cerne never merges
// or renames a tag itself: that changes the meaning someone gave it.
const (
	// RuleComponentSingleUse: components carried by a single entity.
	RuleComponentSingleUse = "component-single-use"
	// RuleComponentSameRoot: groups of components of the same root
	// (domain.SameComponentRoot), e.g. testing and tests.
	RuleComponentSameRoot = "component-same-root"
	// RuleComponentNoteOnly: components on active notes and on no feature, so
	// a feature's components never reach those notes.
	RuleComponentNoteOnly = "component-note-only"
)

// componentFix is the command every tag-cleanup finding points at.
const componentFix = "cerne tags"

// shownComponents caps the names one finding line lists.
const shownComponents = 10

// componentUse is where one component is used across the backend.
type componentUse struct {
	ids        []string // every entity carrying it, any kind and state
	onFeature  bool     // carried by a feature (F-/E-/T-/B-), any state
	onLiveNote bool     // carried by an active note
}

// componentUses indexes the components of every entity, any kind and state,
// but ideas: raw material feeds no rule (see RunConfig).
func componentUses(features []store.Feature, notes []store.Note, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, pendings []store.Pending) map[string]*componentUse {
	use := map[string]*componentUse{}
	add := func(id string, comps []string) {
		for _, c := range comps {
			if use[c] == nil {
				use[c] = &componentUse{}
			}
			use[c].ids = append(use[c].ids, id)
		}
	}
	for _, f := range features {
		add(f.ID, f.Components)
		for _, c := range f.Components {
			use[c].onFeature = true
		}
	}
	for _, n := range notes {
		add(n.ID, n.Components)
		if n.State == domain.NoteActive {
			for _, c := range n.Components {
				use[c].onLiveNote = true
			}
		}
	}
	for _, d := range decisions {
		add(d.ID, d.Components)
	}
	for _, r := range references {
		add(r.ID, r.Components)
	}
	for _, rb := range runbooks {
		add(rb.ID, rb.Components)
	}
	for _, p := range pendings {
		add(p.ID, p.Components)
	}
	return use
}

// componentTagFindings is the one line per tag-cleanup rule that applies.
func componentTagFindings(use map[string]*componentUse) []Finding {
	names := slices.Sorted(maps.Keys(use))
	var single, noteOnly []string
	for _, c := range names {
		if len(use[c].ids) == 1 {
			single = append(single, fmt.Sprintf("%s (%s)", c, use[c].ids[0]))
		}
		if use[c].onLiveNote && !use[c].onFeature {
			noteOnly = append(noteOnly, c)
		}
	}
	// ponytail: O(n²) over the vocabulary (hundreds of names); a root index
	// if a backend ever holds tens of thousands of components.
	var groups []string
	grouped := map[string]bool{}
	for i, a := range names {
		if grouped[a] {
			continue
		}
		g := []string{a}
		for _, b := range names[i+1:] {
			if !grouped[b] && domain.SameComponentRoot(a, b) {
				g = append(g, b)
				grouped[b] = true
			}
		}
		if len(g) > 1 {
			groups = append(groups, strings.Join(g, "/"))
		}
	}
	const retag = "'cerne <kind> retag <id> --component <c>'"
	var out []Finding
	if len(single) > 0 {
		out = append(out, Finding{Rule: RuleComponentSingleUse, Fix: componentFix, Detail: fmt.Sprintf(
			"%d components are carried by a single entity: %s — compare with 'cerne tags' and move each to an existing one with %s, or keep it",
			len(single), capNames(single), retag)})
	}
	if len(groups) > 0 {
		out = append(out, Finding{Rule: RuleComponentSameRoot, Fix: componentFix, Detail: fmt.Sprintf(
			"%d groups of components share a root: %s — pick one name per group ('cerne tags' counts them) and retag the rest with %s",
			len(groups), capNames(groups), retag)})
	}
	if len(noteOnly) > 0 {
		out = append(out, Finding{Rule: RuleComponentNoteOnly, Fix: componentFix, Detail: fmt.Sprintf(
			"%d components are on active notes and on no feature: %s — a feature's components never reach these notes; see 'cerne tags' and add a feature component with 'cerne note retag <id> --component <c>'",
			len(noteOnly), capNames(noteOnly))})
	}
	return out
}

// capNames joins up to shownComponents names and counts the rest.
func capNames(names []string) string {
	if len(names) <= shownComponents {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:shownComponents], ", "), len(names)-shownComponents)
}
