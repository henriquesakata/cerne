package restmpl

import (
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

var (
	bulletRe = regexp.MustCompile(`^(?:[-*+]|\d+[.)])(?:[ \t]+(.*))?$`)
	// nothingRe is a text that only says there is nothing to record.
	nothingRe = regexp.MustCompile(`(?i)^(?:n/a|nenhum|nenhuma|nada|none|nothing|-)\.?$`)
)

// LearningItems reads the items of a result's learnings section (F-0401): each
// first-level list item with its sub-items, a paragraph before the list as an
// item of its own, or, when the section has no list, its prose as one item. The template comment and a nothing-marker (N/A, None,
// Nenhum, ...) are not items.
func LearningItems(result string) []string {
	var lines []string
	in := false
	for _, l := range strings.Split(commentRe.ReplaceAllString(result, ""), "\n") {
		if h, ok := strings.CutPrefix(l, "## "); ok {
			in = doclang.IsLearningsSection(h)
			continue
		}
		if in {
			lines = append(lines, strings.TrimRight(l, " \t\r"))
		}
	}
	var items, pre []string
	var cur []string
	started, fenced := false, false
	flush := func() {
		if started {
			items = append(items, strings.TrimSpace(strings.Join(cur, "\n")))
		}
		cur = nil
	}
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
		}
		if m := bulletRe.FindStringSubmatch(l); m != nil && !fenced {
			flush()
			if !started {
				items = append(items, strings.TrimSpace(strings.Join(pre, "\n"))) // the paragraph before the list
			}
			started = true
			cur = []string{m[1]}
			continue
		}
		if started {
			cur = append(cur, l)
		} else {
			pre = append(pre, l)
		}
	}
	flush()
	if !started {
		items = []string{strings.TrimSpace(strings.Join(pre, "\n"))}
	}
	var out []string
	for _, it := range items {
		if it != "" && !nothingRe.MatchString(strings.Trim(it, "*_ \t")) {
			out = append(out, it)
		}
	}
	return out
}
