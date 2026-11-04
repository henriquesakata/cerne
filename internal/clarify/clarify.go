// Package clarify parses the [ESCLARECER] ambiguity markers that doc
// templates instruct agents to leave in specs, plans, results, decisions and
// runbooks (see internal/spectmpl.AmbiguityMarker). It is pure — no I/O — so
// lint rules, lifecycle gates and CLI surfaces can share one definition of
// what counts as an open clarification.
//
// Recognized forms. A body may cross soft line breaks inside one paragraph:
// each break and the blanks around it read as one space, and a blank line
// stops the match (F-0317). The marker word comes from a closed per-language
// catalog — pt ESCLARECER/ESCLARECIDO, en CLARIFY/CLARIFIED — so a doc
// authored in English gates exactly like one in Portuguese (F-0182):
//
//	[ESCLARECER]                              open, no question
//	[ESCLARECER: question]                    open
//	[ESCLARECER: question | option | option]  open, with proposed options
//	[ESCLARECER: question \| option]          the same: an escaped pipe separates
//	                                          options exactly like a bare one,
//	                                          everywhere — the escape exists so a
//	                                          marker inside a GFM table cell is
//	                                          not split by the cell separator.
//	                                          A LITERAL pipe in an open marker is
//	                                          therefore not expressible; only the
//	                                          resolved form carries one.
//	[«ESCLARECER»: question]                  open (legacy guillemet form)
//	[ESCLARECIDO: question → answer]          resolved (written by the UI)
//	[CLARIFY: question] / [CLARIFIED: q → a]  the same, en vocabulary
package clarify

import (
	"regexp"
	"sort"
	"strings"
)

// Clarification is one marker occurrence inside a document.
type Clarification struct {
	Question string   // empty on the bare form
	Options  []string // proposed answers, empty when the marker has no pipes
	Resolved bool     // true for [ESCLARECIDO: …]
	Answer   string   // the CURRENT answer; set only when Resolved
	Answers  []string // the answer trail in chronological order (F-0197); the
	// last one is Answer. A marker written before corrections
	// existed is a trail of one, so nothing needs migrating.
	Offset int    // byte offset of Raw within the parsed content
	Raw    string // the marker exactly as it appears in the text
}

// vocab is one language's marker word pair. The catalog is a closed set in
// code, like the facet list: an open-ended regex would swallow prose between
// brackets. Adding a language = one row here, its mirror in ui/src/clarify.ts
// and a fixture in testdata/corpus.json (the parity corpus fails whichever
// side lags behind).
type vocab struct{ Open, Resolved string }

var vocabs = []vocab{
	{Open: "ESCLARECER", Resolved: "ESCLARECIDO"},
	{Open: "CLARIFY", Resolved: "CLARIFIED"},
}

// markerWords returns the regex alternation of every catalog word, longest
// first so a word that prefixes another (CLARIFY / CLARIFIED) never shadows
// the longer form.
func markerWords() string {
	var words []string
	for _, v := range vocabs {
		words = append(words, v.Open, v.Resolved)
	}
	sort.Slice(words, func(i, j int) bool { return len(words[i]) > len(words[j]) })
	return strings.Join(words, "|")
}

// resolvedWords maps each Resolved word of the catalog, so parsing can tell
// the state from the matched word alone — no language detection on read.
var resolvedWords = func() map[string]bool {
	m := make(map[string]bool, len(vocabs))
	for _, v := range vocabs {
		m[v.Resolved] = true
	}
	return m
}()

// markerRe matches any catalog marker word with an optional legacy guillemet
// pair and an optional body. The body is a first line plus continuation lines
// (a soft line break renders as a space in markdown, F-0317); each
// continuation must carry, after its indentation, a non-blank char other than
// a bracket, so a blank line (paragraph end) stops the match, and no
// continuation line contains an opening bracket, so a marker that starts on a
// LATER line is never absorbed. The body never contains a closing bracket,
// which also keeps prefix words ([ESCLARECEDOR], [CLARIFIER]) out. Explicit
// classes, never \s: RE2 and JS disagree on it and the parity corpus locks the
// two together.
// ponytail: a multi-line marker whose continuation line contains "[" is not
// recognized (stays invisible, as before F-0317); an unclosed opener still
// absorbs a marker that starts later on its OWN line, exactly as before (the
// first-line class allows "["); markdown block boundaries (list item,
// heading, table row, quote) are not detected, so an UNCLOSED marker absorbs the following bracket-free lines of its paragraph; upgrade =
// span via mdast (a real markdown parse).
var markerRe = regexp.MustCompile(`\[«?(` + markerWords() + `)»?(?::[ \t]*([^\]\n]*(?:\n[ \t]*[^\[\] \t\r\n][^\[\]\n]*)*))?\]`)

// lineBreakRe is a soft line break with the indentation around it: markdown
// renders it as one space, and so does the parsed body.
var lineBreakRe = regexp.MustCompile(`[ \t]*\r?\n[ \t]*(?:>[ \t]*)*`)

// Citation contexts: a marker inside any of these is the doc *talking about*
// the convention, not an authored clarification. HTML comments carry the
// templates' authoring guidance (an unclosed one runs to EOF, like in HTML);
// fenced blocks and inline code are how prose cites the syntax — the same
// contexts the UI's markdown render leaves untouched.
var (
	commentRe = regexp.MustCompile(`<!--([\s\S]*?)(-->|$)`)
	fencedRe  = regexp.MustCompile("(?m)^```[^\n]*\n[\\s\\S]*?^```")
	// ponytail: inline code that wraps a line is not a citation, so a marker
	// quoted in it reads as a marker (visible false positive, accepted
	// 2026-11-04); upgrade = span via mdast, never a wider regex (that hid real
	// markers, F-0317 round 2).
	inlineCodeRe = regexp.MustCompile("`[^`\n]+`")
)

// Inside a GFM table an unescaped pipe is a cell separator: the parser slices
// a marker with options across several cells before any chip logic sees it,
// leaving raw text in the cell with no way to answer it (F-0282). The fix is
// markdown's own backslash escape — inside a table the marker's option pipes
// are written `\|`, which keeps the cell whole. So the escaped form separates
// options exactly like the bare one, and the backslash is dropped on read.
//
// In the RESOLVED form there are no options — the body splits on the arrow
// alone — so there `\|` is a literal pipe and `\\` a literal backslash: Parse
// unescapes both, and Resolve and Correct escape on write, which is what keeps
// an answer typed with a pipe (or with a backslash before one) from corrupting
// the table it lives in.
//
// unescapeSeparator turns the escaped separator back into a plain pipe, and
// stops there: it is all the OPEN form needs, because nothing ever writes an
// escape into an open marker. Reading a doubled backslash as an escape here
// would silently rewrite prose that was already authored — a Windows path, a
// regex — for no gain.
func unescapeSeparator(s string) string { return strings.ReplaceAll(s, `\|`, "|") }

// unescapeResolved reads the RESOLVED form, which Resolve and Correct wrote:
// there the backslash escapes itself as well as the pipe, so both come back.
func unescapeResolved(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '|' || s[i+1] == '\\') {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// The backslash escapes itself as well as the pipe, in one pass: without that,
// an answer typed as a backslash followed by a pipe would be written with the
// backslash escaping the backslash, and GFM would read the pipe that follows as
// a cell separator — the very corruption the escape exists to prevent.
var pipeEscaper = strings.NewReplacer(`\`, `\\`, "|", `\|`)

func escapePipe(s string) string { return pipeEscaper.Replace(s) }

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
			body = strings.TrimSpace(lineBreakRe.ReplaceAllString(content[m[4]:m[5]], " "))
		}
		if resolvedWords[word] {
			c.Resolved = true
			// The question is what precedes the FIRST arrow; everything after
			// it is the answer trail (F-0197), each correction appended with
			// the same arrow. A marker with a single answer parses as a trail
			// of one, which is why no legacy document needs migrating.
			rest := body
			if q, a, ok := strings.Cut(body, "→"); ok {
				c.Question = unescapeResolved(strings.TrimSpace(q))
				rest = a
			}
			for _, s := range strings.Split(rest, "→") {
				c.Answers = append(c.Answers, unescapeResolved(strings.TrimSpace(s)))
			}
			c.Answer = c.Answers[len(c.Answers)-1]
		} else {
			parts := strings.Split(unescapeSeparator(body), "|")
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

// Resolve returns the resolved marker form for the open clarification c, in
// the same vocabulary as the original word ([CLARIFY] resolves to [CLARIFIED],
// never [ESCLARECIDO]) — the mirror of ui/src/clarify.ts resolveMarker, kept
// in step by the corpus' resolve cases. It reports false when c is already
// resolved or its raw form is not a catalog marker; legacy guillemets resolve
// to the plain form.
func Resolve(c Clarification, answer string) (string, bool) {
	if c.Resolved {
		return "", false
	}
	m := markerRe.FindStringSubmatchIndex(c.Raw)
	if m == nil || m[0] != 0 || m[1] != len(c.Raw) {
		return "", false
	}
	word := c.Raw[m[2]:m[3]]
	for _, v := range vocabs {
		if v.Open != word {
			continue
		}
		if c.Question != "" {
			return "[" + v.Resolved + ": " + escapePipe(c.Question) + " → " + escapePipe(answer) + "]", true
		}
		return "[" + v.Resolved + ": " + escapePipe(answer) + "]", true
	}
	return "", false
}

// Correct returns the marker form for the resolved clarification c with answer
// appended to its trail — the inverse of Resolve, and the reason a correction
// does not erase what it corrected (F-0197): the document carries the old
// answer next to the new one, current last. The vocabulary of the original is
// preserved ([CLARIFIED] stays [CLARIFIED]), like in Resolve. It reports false
// when c is still open — correcting an open marker is answering it — or when
// its raw form is not a catalog marker.
func Correct(c Clarification, answer string) (string, bool) {
	if !c.Resolved {
		return "", false
	}
	m := markerRe.FindStringSubmatchIndex(c.Raw)
	if m == nil || m[0] != 0 || m[1] != len(c.Raw) {
		return "", false
	}
	word := c.Raw[m[2]:m[3]]
	if !resolvedWords[word] {
		return "", false
	}
	escaped := make([]string, 0, len(c.Answers)+1)
	for _, a := range append(append([]string{}, c.Answers...), answer) {
		escaped = append(escaped, escapePipe(a))
	}
	trail := strings.Join(escaped, " → ")
	if c.Question != "" {
		return "[" + word + ": " + escapePipe(c.Question) + " → " + trail + "]", true
	}
	// No question: the trail still needs its leading arrow, otherwise
	// [CLARIFIED: a1 → a2] would reparse as question "a1" with answer "a2".
	// The empty slot before the first arrow is what says "there was no
	// question" — and [CLARIFIED: a] (no arrow at all) keeps parsing as the
	// one-answer legacy form.
	return "[" + word + ": → " + trail + "]", true
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
