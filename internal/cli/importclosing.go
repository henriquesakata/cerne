package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/henriquesakata/cerne/internal/guide"
)

// closingKinds is the order the closing lists kinds in.
var closingKinds = []string{"feature", "decision", "reference", "runbook", "note"}

// importClosing is what an applied import hands the shared closing: the
// counts by kind and by state and the titles that came in.
type importClosing struct {
	counts      map[string]map[string]int // kind -> state -> count
	features    []string                  // feature titles, in import order
	others      []string                  // every other title, in import order
	historicals int
}

func newImportClosing() *importClosing {
	return &importClosing{counts: map[string]map[string]int{}}
}

func (c *importClosing) add(kind, state, title string) {
	if c.counts[kind] == nil {
		c.counts[kind] = map[string]int{}
	}
	c.counts[kind][state]++
	if kind == "feature" {
		c.features = append(c.features, title)
		return
	}
	c.others = append(c.others, title)
}

// searchTerm is the first word of at least four letters in the imported
// titles, so the example search is a real hit; a placeholder when none.
func (c *importClosing) searchTerm() string {
	for _, t := range append(c.features, c.others...) {
		for _, w := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if len([]rune(w)) >= 4 {
				return w
			}
		}
	}
	return "<a word from your docs>"
}

// print writes the closing shared by both apply paths: what came in by kind
// and state, then the next steps and the first-week guide.
func (c *importClosing) print(w io.Writer) {
	fmt.Fprintln(w, "What came in:")
	for _, kind := range closingKinds {
		states := c.counts[kind]
		if n := kindTotal(states); n > 0 {
			fmt.Fprintf(w, "  %ss %d%s\n", kind, n, closingBreakdown(states))
		}
	}
	if c.historicals > 0 {
		fmt.Fprintf(w, "  historicals %d\n", c.historicals)
	}
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  1. Search what came in:  cerne search \"%s\"\n", c.searchTerm())
	fmt.Fprintln(w, "  2. Ask your agent for the first feature:")
	fmt.Fprintf(w, "     \"%s\"\n", guide.FirstFeatureAsk)
	fmt.Fprintf(w, "First-week guide: %s\n", guide.FirstWeekURL)
}

// closingBreakdown renders ": 9 implemented, 3 candidate" with the largest
// count first (ties alphabetical); "" when there are no states.
func closingBreakdown(states map[string]int) string {
	var names []string
	for s := range states {
		names = append(names, s)
	}
	sort.Slice(names, func(i, j int) bool {
		if states[names[i]] != states[names[j]] {
			return states[names[i]] > states[names[j]]
		}
		return names[i] < names[j]
	})
	var parts []string
	for _, s := range names {
		parts = append(parts, fmt.Sprintf("%d %s", states[s], s))
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}
