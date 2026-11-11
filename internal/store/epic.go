package store

import (
	"sort"

	"github.com/henriquesakata/cerne/internal/domain"
)

// ChildrenByParent groups features by their Parent ID. Features without a
// parent are skipped; each child slice is sorted by ID. The result maps an
// epic's ID to the features that decompose it — the inferred epic structure.
// The key is the canonical parent id, so a legacy lowercase parent still
// groups under its epic.
func ChildrenByParent(features []Feature) map[string][]Feature {
	byParent := map[string][]Feature{}
	for _, f := range features {
		p := domain.CanonicalID(f.Parent)
		if p == "" {
			continue
		}
		byParent[p] = append(byParent[p], f)
	}
	for _, children := range byParent {
		sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	}
	return byParent
}

// EpicProgress counts how many of children are concluded — implemented or
// accepted (done, F-0131) — out of the total: the rollup shown for an epic in
// status, CONTEXT.md and the UI.
func EpicProgress(children []Feature) (done, total int) {
	for _, c := range children {
		if c.State == domain.StateImplemented || c.State == domain.StateDone {
			done++
		}
	}
	return done, len(children)
}
