package lint

import (
	"fmt"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/dupmatch"
	"gitlab.com/rockt/cerne/internal/store"
)

// dupItem is one live entity to compare: its id and the text the type's
// duplicate criterion looks at (title, or URL for a reference).
type dupItem struct{ id, key string }

// duplicateLiveFindings (F-0324) flags live entities of the SAME type that the
// registration guard would have refused — a similar title for notes and
// decisions, an equal normalized URL for references — registered more than
// once. One finding per group, listing every id. A group where any member's
// creation event carries guard=forced is a conscious override and is silent.
// The pre-existing backlog is only made visible: nothing is retired or merged.
func duplicateLiveFindings(notes []store.Note, decisions []store.Decision, references []store.Reference, events []domain.Event) []Finding {
	forced := map[string]bool{}
	for _, ev := range events {
		if strings.HasSuffix(ev.Type, ".created") && ev.Payload["guard"] == "forced" {
			forced[ev.EntityID] = true
		}
	}
	var out []Finding
	report := func(noun, retire string, items []dupItem, same func(a, b string) bool) {
		for _, g := range dupGroups(items, same) {
			ack := false
			for _, id := range g {
				ack = ack || forced[id]
			}
			if ack {
				continue
			}
			out = append(out, Finding{
				Rule:   RuleDuplicateLive,
				Entity: g[0],
				Detail: fmt.Sprintf("live %ss register the same thing: %s — keep one and retire the others ('%s <id>'), or re-create with --force to acknowledge", noun, strings.Join(g, ", "), retire),
			})
		}
	}
	var ns, ds, rs []dupItem
	for _, n := range notes {
		if n.State == domain.NoteActive {
			ns = append(ns, dupItem{n.ID, n.Title})
		}
	}
	for _, d := range decisions {
		if d.State == domain.DecisionActive {
			ds = append(ds, dupItem{d.ID, d.Title})
		}
	}
	for _, r := range references {
		if r.State == domain.ReferenceCurrent && r.URL != "" {
			rs = append(rs, dupItem{r.ID, dupmatch.NormalizeURL(r.URL)})
		}
	}
	report("note", "cerne note retire", ns, dupmatch.SimilarTitles)
	report("decision", "cerne decision substituir --by", ds, dupmatch.SimilarTitles)
	report("reference", "cerne reference retag", rs, func(a, b string) bool { return a == b })
	return out
}

// dupGroups returns the connected components (size >= 2) of items under same,
// ids in input order. ponytail: O(n^2) pairwise scan, fine for a backend of
// hundreds of live entities; index by key if it ever reaches tens of thousands.
func dupGroups(items []dupItem, same func(a, b string) bool) [][]string {
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if same(items[i].key, items[j].key) {
				parent[find(j)] = find(i)
			}
		}
	}
	byRoot := map[int][]string{}
	var roots []int
	for i, it := range items {
		r := find(i)
		if _, ok := byRoot[r]; !ok {
			roots = append(roots, r)
		}
		byRoot[r] = append(byRoot[r], it.id)
	}
	var out [][]string
	for _, r := range roots {
		if len(byRoot[r]) >= 2 {
			out = append(out, byRoot[r])
		}
	}
	return out
}
