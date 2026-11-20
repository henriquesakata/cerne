package cli

import "slices"

// rankByComponents is the --component group filter of a listing (E-0005
// criterion 20): it keeps the items carrying any of q, most matches first and,
// on a tie, the one whose rarest matched component is the least used among
// items; the order is stable otherwise. It returns the kept items and, by id,
// the components of q each one matched.
func rankByComponents[T any](items []T, tags func(T) (id string, comps []string), q []string) ([]T, map[string][]string) {
	use := map[string]int{}
	for _, it := range items {
		_, comps := tags(it)
		for _, c := range comps {
			use[c]++
		}
	}
	type hit struct {
		item  T
		match []string
		rare  int
	}
	var hits []hit
	matched := map[string][]string{}
	for _, it := range items {
		id, comps := tags(it)
		h := hit{item: it}
		for _, c := range q {
			if slices.Contains(comps, c) {
				if len(h.match) == 0 || use[c] < h.rare {
					h.rare = use[c]
				}
				h.match = append(h.match, c)
			}
		}
		if len(h.match) > 0 {
			hits = append(hits, h)
			matched[id] = h.match
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if d := len(b.match) - len(a.match); d != 0 {
			return d
		}
		return a.rare - b.rare
	})
	out := make([]T, len(hits))
	for i, h := range hits {
		out[i] = h.item
	}
	return out, matched
}
