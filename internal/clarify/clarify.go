// Package clarify parses the [ESCLARECER] ambiguity markers that doc
// templates instruct agents to leave in specs, plans, results, decisions and
// runbooks (see internal/spectmpl.AmbiguityMarker). It is pure — no I/O — so
// lint rules, lifecycle gates and CLI surfaces can share one definition of
// what counts as an open clarification.
//
// Recognized forms, always single-line:
//
//	[ESCLARECER]                              open, no question
//	[ESCLARECER: question]                    open
//	[ESCLARECER: question | option | option]  open, with proposed options
//	[«ESCLARECER»: question]                  open (legacy guillemet form)
//	[ESCLARECIDO: question → answer]          resolved (written by the UI)
package clarify

import (
	"regexp"
	"strings"
)

// Clarification is one marker occurrence inside a document.
type Clarification struct {
	Question string   // empty on the bare form
	Options  []string // proposed answers, empty when the marker has no pipes
	Resolved bool     // true for [ESCLARECIDO: …]
	Answer   string   // set only when Resolved
	Offset   int      // byte offset of Raw within the parsed content
	Raw      string   // the marker exactly as it appears in the text
}

// markerRe matches both marker words with an optional legacy guillemet pair
// and an optional single-line body. The body must not cross lines nor contain
// a closing bracket, which also keeps prefix words ([ESCLARECEDOR]) out.
var markerRe = regexp.MustCompile(`\[«?(ESCLARECER|ESCLARECIDO)»?(?::[ \t]*([^\]\n]*))?\]`)

// Citation contexts: a marker inside any of these is the doc *talking about*
// the convention, not an authored clarification. HTML comments carry the
// templates' authoring guidance (an unclosed one runs to EOF, like in HTML);
// fenced blocks and inline code are how prose cites the syntax — the same
// contexts the UI's markdown render leaves untouched.
var (
	commentRe    = regexp.MustCompile(`<!--([\s\S]*?)(-->|$)`)
	fencedRe     = regexp.MustCompile("(?m)^```[^\n]*\n[\\s\\S]*?^```")
	inlineCodeRe = regexp.MustCompile("`[^`\n]+`")
)

// citationRanges returns the [start, end) intervals of every citation context
// in content, in no particular order.
func citationRanges(content string) [][]int {
	var ranges [][]int
	for _, re := range []*regexp.Regexp{commentRe, fencedRe, inlineCodeRe} {
		ranges = append(ranges, re.FindAllStringIndex(content, -1)...)
	}
	return ranges
}

// Parse returns every marker in content, in document order, with exact
// offsets so callers can rewrite one occurrence without touching duplicates.
// Markers inside citation contexts (HTML comments, fenced or inline code)
// are skipped.
func Parse(content string) []Clarification {
	matches := markerRe.FindAllStringSubmatchIndex(content, -1)
	if matches == nil {
		return nil
	}
	citations := citationRanges(content)
	inCitation := func(off int) bool {
		for _, c := range citations {
			if off >= c[0] && off < c[1] {
				return true
			}
		}
		return false
	}
	out := make([]Clarification, 0, len(matches))
	for _, m := range matches {
		if inCitation(m[0]) {
			continue
		}
		c := Clarification{Offset: m[0], Raw: content[m[0]:m[1]]}
		word := content[m[2]:m[3]]
		body := ""
		if m[4] >= 0 {
			body = strings.TrimSpace(content[m[4]:m[5]])
		}
		if word == "ESCLARECIDO" {
			c.Resolved = true
			if q, a, ok := strings.Cut(body, "→"); ok {
				c.Question = strings.TrimSpace(q)
				c.Answer = strings.TrimSpace(a)
			} else {
				c.Answer = body
			}
		} else {
			parts := strings.Split(body, "|")
			c.Question = strings.TrimSpace(parts[0])
			for _, p := range parts[1:] {
				if opt := strings.TrimSpace(p); opt != "" {
					c.Options = append(c.Options, opt)
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// Open returns only the unresolved markers in content.
func Open(content string) []Clarification {
	var out []Clarification
	for _, c := range Parse(content) {
		if !c.Resolved {
			out = append(out, c)
		}
	}
	return out
}
