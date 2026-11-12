package doclang

import (
	"regexp"
	"strconv"
	"strings"
)

// The recognizers of the bar/Gauntlet protocol. A reader hands over a line or
// a block and gets back what it is; which words, in which language, under
// which layout, is decided here and nowhere else.

// lineMarkers is the markdown a protocol line may open with: list, quote,
// heading and table markers, emphasis, "N." — in any layout.
const lineMarkers = `(?:[\s\x{00A0}#>*_|+-]|\d+[.)]\s)*`

// wordEdge is a position an outcome word may touch without being part of a
// larger token: not a letter, digit, underscore or hyphen ("tie", never
// "tie-break").
const wordEdge = `[^\pL\pN_-]`

// matchers is every recognizer of one language, compiled once.
type matchers struct {
	bar          *regexp.Regexp // group 1: the value after the label
	barInProse   bool           // a bar line counts inside a code block too
	notApplic    *regexp.Regexp // the complete form; group 1: the parenthesized text
	waived       *regexp.Regexp // the complete form; group 1: the parenthesized text
	notApplicAt  *regexp.Regexp // a value that opens as that exemption
	waivedAt     *regexp.Regexp
	header       *regexp.Regexp // the Method/Exit lines under the bar
	critic       *regexp.Regexp
	round        *regexp.Regexp // group 1: the round number
	outcome      *regexp.Regexp // group 1: an outcome word inside a line
	outcomeLine  *regexp.Regexp // group 1: the outcome a line states on its own
	verdictLit   *regexp.Regexp // the judge's fixed last line
	anywhere     bool           // an outcome word anywhere in a round is a verdict
	mention      *regexp.Regexp // group 1: the plural keyword or empty; group 2: the numbers
	mentionRange *regexp.Regexp // groups 1, 2: the ends of a range inside the numbers
}

func words(l Lang, ts ...Term) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = in(t, l)
	}
	return strings.Join(parts, `|`)
}

func build(l Lang) matchers {
	outcomes := words(l, Win, Loss, Tie)
	and, or := `\b`+in(ListAnd, l)+`\b`, `\b`+in(ListOr, l)+`\b`
	comma := `,`
	if layouts[l] == strict {
		comma = `,\s*(?:` + and + `|` + or + `)?` // the serial comma: "1, 2, and 3"
	}
	joiners := `[-–—]|\b` + in(RangeTo, l) + `\b|` + comma + `|` + and + `|` + or
	numbers := `\s*(?:[:#]|n[ºo°]\.?)?\s*(\d+(?:\s*(?:` + joiners + `)\s*\d+)*)`
	keyword := `(?:(` + in(Criteria, l) + `)|` + in(Criterion, l) + `)`
	m := matchers{
		notApplic:   regexp.MustCompile(`(?i)^` + in(NotApplicable, l) + `\s*\(([^)]*)`),
		waived:      regexp.MustCompile(`(?i)^` + in(Waived, l) + `\s*\(([^)]*)`),
		notApplicAt: regexp.MustCompile(`(?i)^` + in(NotApplicable, l)),
		waivedAt:    regexp.MustCompile(`(?i)^` + in(WaivedOpen, l)),
	}
	rangeCase := `(?i)` // "Criteria 2 TO 4", as the keyword itself is matched
	if layouts[l] == loose {
		rangeCase = ``
	}
	m.mentionRange = regexp.MustCompile(rangeCase + `(\d+)\s*(?:[-–—]|\b` + in(RangeTo, l) + `\b)\s*(\d+)`)
	if layouts[l] == loose {
		m.bar = regexp.MustCompile(`(?i)^` + lineMarkers + in(BarLabel, l) + `[*_]*(?:[ \t(][^:\n]{0,39})?:[*_]*(.*)$`)
		m.barInProse = true // documents show the forms inside code fences
		m.header = regexp.MustCompile(`(?i)^(?:` + words(l, MethodLabel, ExitLabel) + `)[*_]*:`)
		m.critic = regexp.MustCompile(`(?i)^` + in(CriticFindings, l))
		m.round = regexp.MustCompile(`^` + in(Round, l) + `[\s\x{00A0}]+(\d+)\b`)
		m.outcome = regexp.MustCompile(`(?i)\b(` + outcomes + `)\b`)
		m.outcomeLine = regexp.MustCompile(`(?i)^(?:` + in(OutcomeLabel, l) + `[*_]*[ \t]*:[ \t]*[*_]*[ \t]*)?(` + outcomes + `)\b`)
		m.verdictLit = regexp.MustCompile(`(?i:` + in(VerdictLabel, l) + `):\s*(?:A|B|` + in(TieLetter, l) + `)\b`)
		m.anywhere = true
		m.mention = regexp.MustCompile(`(?i)` + keyword + numbers)
		return m
	}
	// Strict: the label as written, capital included, with only a
	// parenthesized qualifier ("Bar (from the spec):", never "Bar chart: …"
	// or a YAML "bar: 42"), at most three spaces in ("    Bar: 1," is code)
	// and never inside a code block (BarLine's inCode).
	m.bar = regexp.MustCompile(`^ {0,3}(?:(?:[#>*_|+-]+|\d+[.)])[ \t\x{00A0}]+)*[*_]*` + in(BarLabel, l) + `[*_]*(?:[ \t]*\([^:\n)]{0,39}\))?:[*_]*(.*)$`)

	m.header = regexp.MustCompile(`^(?:` + words(l, MethodLabel, ExitLabel) + `)[*_]*:`)
	m.critic = regexp.MustCompile(`(?i)^` + in(CriticFindings, l) + `\b`)
	// The word, the number, then nothing or punctuation ("Round 2 — …",
	// never "Round 2 of benchmarking was skipped").
	// Emphasis and a table cell may close the anchor ("**Round 1** — …",
	// "| Round 1 | loss |"), as they may in a loose language.
	m.round = regexp.MustCompile(`^` + in(Round, l) + `[\s\x{00A0}]+(\d+)[*_]*[ \t\x{00A0}]*(?:$|[—–:(.,;|-])`)
	m.outcome = regexp.MustCompile(`(?i)(?:^|` + wordEdge + `)(` + outcomes + `)(?:$|` + wordEdge + `)`)
	// A line states an outcome when it is the labelled form ("Verdict: loss —
	// B handles the empty case") or the bare word and nothing else ("Loss.").
	// A bare word followed by text is prose or a metric: "Loss of precision
	// was noted", "Loss: 0.31", "Win: 3 of 5 seeds".
	m.outcomeLine = regexp.MustCompile(`(?i)^(?:` + in(OutcomeLabel, l) + `[*_]*[ \t]*:[ \t]*[*_]*[ \t]*(` + outcomes + `)[*_]*[ \t]*(?:$|[.:;,!(—–]|-\s)|[*_]*(` + outcomes + `)[*_]*[ \t]*[.!]?[ \t]*$)`)
	// The letter on the label's own line, then nothing or a mark that closes
	// a sentence or a quote ("VERDICT: A", `"VERDICT: B".`; never "Verdict:
	// A clear improvement" or "Verdict: A/B comparison pending").
	m.verdictLit = regexp.MustCompile(`(?m)(?i:` + in(VerdictLabel, l) + `):[ \t]*(?:A|B|` + in(TieLetter, l) + `)[ \t]*(?:$|["”»'’.!;)\]*_\x60])`)
	m.mention = regexp.MustCompile(`(?i)\b` + keyword + numbers)
	return m
}

var byLang = func() map[Lang]matchers {
	m := make(map[Lang]matchers, len(langs))
	for _, l := range langs {
		m[l] = build(l)
	}
	return m
}()

// BarLine reads line as the bar line: the value after the label, trimmed, and
// the language the label was written in. inCode says the line sits inside a
// fenced code block, where only a loose language still reads it: a code
// sample ("Bar: 1,") is not a bar.
func BarLine(line string, inCode bool) (value string, l Lang, ok bool) {
	for _, l := range langs {
		m := byLang[l]
		if inCode && !m.barInProse {
			continue
		}
		if sub := m.bar.FindStringSubmatch(line); sub != nil {
			return strings.TrimSpace(sub[1]), l, true
		}
	}
	return "", "", false
}

var fenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// Fence reports whether line opens or closes a fenced code block. A reader
// walking a document flips its inCode on it.
func Fence(line string) bool { return fenceRe.MatchString(line) }

// Exemption reads a bar value as one of the two exemption forms. ok reports
// that the value opens as an exemption (so it is not a named reference); t is
// which one (NotApplicable or Waived) and l its language; inner is the
// parenthesized text, and complete reports that the form reached its
// parentheses at all.
func Exemption(value string) (t Term, l Lang, inner string, complete, ok bool) {
	for _, l := range langs {
		m := byLang[l]
		for _, c := range []struct {
			t        Term
			at, full *regexp.Regexp
		}{{NotApplicable, m.notApplicAt, m.notApplic}, {Waived, m.waivedAt, m.waived}} {
			if !c.at.MatchString(value) {
				continue
			}
			if sub := c.full.FindStringSubmatch(value); sub != nil {
				return c.t, l, sub[1], true, true
			}
			return c.t, l, "", false, true
		}
	}
	return 0, "", "", false, false
}

// HeaderLine reports a Method or Exit line: part of the bar's header, never
// of a round.
func HeaderLine(s string) bool {
	for _, l := range langs {
		if byLang[l].header.MatchString(s) {
			return true
		}
	}
	return false
}

// CriticLine reports the line that opens the critic's findings.
func CriticLine(s string) bool {
	for _, l := range langs {
		if byLang[l].critic.MatchString(s) {
			return true
		}
	}
	return false
}

// RoundAnchor reads s as the line that opens a round: its number and the
// language the anchor was written in.
func RoundAnchor(s string) (n int, l Lang, ok bool) {
	for _, l := range langs {
		if m := byLang[l].round.FindStringSubmatch(s); m != nil {
			n, _ = strconv.Atoi(m[1])
			return n, l, true
		}
	}
	return 0, "", false
}

// Outcome returns the first outcome word in s, or "". The words of language
// prefer are looked for first, and another language's only when it has none:
// in "Rodada 2 — quick win tentado, derrota" the round is a derrota, not a
// win. Classify the word with Is(Loss, word).
func Outcome(s string, prefer Lang) string {
	order := []Lang{prefer}
	for _, l := range langs {
		if l != prefer {
			order = append(order, l)
		}
	}
	for _, l := range order {
		if m, ok := byLang[l]; ok {
			if sub := m.outcome.FindStringSubmatch(s); sub != nil {
				return sub[1]
			}
		}
	}
	return ""
}

// OutcomeLine returns the outcome a line states on its own ("Veredito:
// derrota", "Loss."), or "".
func OutcomeLine(s string) string {
	for _, l := range langs {
		if m := byLang[l].outcomeLine.FindStringSubmatch(s); m != nil {
			for _, word := range m[1:] { // the labelled or the bare group
				if word != "" {
					return word
				}
			}
		}
	}
	return ""
}

// VerdictIn reports that a round's text carries a verdict without a stated
// outcome line: the judge's fixed last line in any language, or — in a loose
// language only — an outcome word anywhere in the text.
func VerdictIn(text string) bool {
	for _, l := range langs {
		m := byLang[l]
		if m.verdictLit.MatchString(text) || (m.anywhere && m.outcome.MatchString(text)) {
			return true
		}
	}
	return false
}

// LooseOutcome reports an outcome word anywhere in s, in the loose languages
// only: the predicate of records older than the strict reader.
func LooseOutcome(s string) bool {
	for _, l := range langs {
		if m := byLang[l]; m.anywhere && m.outcome.MatchString(s) {
			return true
		}
	}
	return false
}

// legacyExemptionRe is the exemption as records older than the strict reader
// wrote it: unanchored, in the loose languages only.
var legacyExemptionRe = func() *regexp.Regexp {
	var parts []string
	for _, l := range langs {
		if layouts[l] == loose {
			// Those records spelled the form with spaces only; the catalog's
			// fragment also takes a hyphen, which this predicate never did.
			spaced := strings.ReplaceAll(words(l, NotApplicable, Waived), `[\s-]+`, `\s+`)
			parts = append(parts, in(BarLabel, l)+`:\s*(?:`+spaced+`)\s*\(`)
		}
	}
	return regexp.MustCompile(`(?i)(?:` + strings.Join(parts, `|`) + `)`)
}()

// LegacyExemption reports an exemption written anywhere in section, by the
// rule of records older than the strict reader.
func LegacyExemption(section string) bool { return legacyExemptionRe.MatchString(section) }

// Citation is one mention of acceptance criteria in a result: "criterion 2"
// or "criteria 1-3, 5". Plural tells the two apart: only a plural keyword
// expands ranges and lists, so prose like "criterion 2 to 3 hours" cannot
// vouch for criterion 3.
type Citation struct {
	Plural  bool
	Numbers []int    // every number written, in order
	Ranges  [][2]int // each "N-M" / "N to M" pair among them
}

var numRe = regexp.MustCompile(`\d+`)

// Citations returns every citation of acceptance criteria in text, in any
// language. Each language joins its numbers with its own words.
func Citations(text string) []Citation {
	var out []Citation
	for _, l := range langs {
		m := byLang[l]
		for _, sub := range m.mention.FindAllStringSubmatch(text, -1) {
			c := Citation{Plural: sub[1] != ""}
			for _, d := range numRe.FindAllString(sub[2], -1) {
				n, _ := strconv.Atoi(d)
				c.Numbers = append(c.Numbers, n)
			}
			for _, r := range m.mentionRange.FindAllStringSubmatch(sub[2], -1) {
				lo, _ := strconv.Atoi(r[1])
				hi, _ := strconv.Atoi(r[2])
				c.Ranges = append(c.Ranges, [2]int{lo, hi})
			}
			out = append(out, c)
		}
	}
	return out
}

// unmetRe matches an acceptance line — a list item or table row naming a
// criterion — that the delivery marked unmet, in the strict languages. The
// loose language's heuristic predates the catalog and stays with its reader.
var unmetRe = func() *regexp.Regexp {
	var parts []string
	for _, l := range langs {
		if layouts[l] == strict {
			parts = append(parts, `(?i:`+words(l, Criterion, Criteria)+`)\b.*\b`+in(Unmet, l)+`\b`)
		}
	}
	if len(parts) == 0 {
		return regexp.MustCompile(`^\b$`) // matches nothing
	}
	return regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)]|\|).*(?:` + strings.Join(parts, `|`) + `)`)
}()

// UnmetCriterion reports an acceptance line that marks a criterion unmet.
func UnmetCriterion(line string) bool { return unmetRe.MatchString(line) }
