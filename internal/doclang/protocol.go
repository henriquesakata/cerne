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
	joiners := `[-–—]|\b` + in(RangeTo, l) + `\b|,|\b` + in(ListAnd, l) + `\b|\b` + in(ListOr, l) + `\b`
	numbers := `\s*(?:[:#]|n[ºo°]\.?)?\s*(\d+(?:\s*(?:` + joiners + `)\s*\d+)*)`
	keyword := `(?:(` + in(Criteria, l) + `)|` + in(Criterion, l) + `)`
	m := matchers{
		notApplic:    regexp.MustCompile(`(?i)^` + in(NotApplicable, l) + `\s*\(([^)]*)`),
		waived:       regexp.MustCompile(`(?i)^` + in(Waived, l) + `\s*\(([^)]*)`),
		notApplicAt:  regexp.MustCompile(`(?i)^` + in(NotApplicable, l)),
		waivedAt:     regexp.MustCompile(`(?i)^` + in(WaivedOpen, l)),
		mentionRange: regexp.MustCompile(`(\d+)\s*(?:[-–—]|\b` + in(RangeTo, l) + `\b)\s*(\d+)`),
	}
	if layouts[l] == loose {
		m.bar = regexp.MustCompile(`(?i)^` + lineMarkers + in(BarLabel, l) + `[*_]*(?:[ \t(][^:\n]{0,39})?:[*_]*(.*)$`)
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
	// or a YAML "bar: 42").
	m.bar = regexp.MustCompile(`^` + lineMarkers + in(BarLabel, l) + `[*_]*(?:[ \t]*\([^:\n)]{0,39}\))?:[*_]*(.*)$`)
	m.header = regexp.MustCompile(`^(?:` + words(l, MethodLabel, ExitLabel) + `)[*_]*:`)
	m.critic = regexp.MustCompile(`(?i)^` + in(CriticFindings, l) + `\b`)
	// The word, the number, then nothing or punctuation ("Round 2 — …",
	// never "Round 2 of benchmarking was skipped").
	m.round = regexp.MustCompile(`^` + in(Round, l) + `[\s\x{00A0}]+(\d+)[ \t\x{00A0}]*(?:$|[—–:(.,;-])`)
	m.outcome = regexp.MustCompile(`(?i)(?:^|` + wordEdge + `)(` + outcomes + `)(?:$|` + wordEdge + `)`)
	// On its own line the word is followed by nothing or punctuation
	// ("Loss: …", never "Loss of precision was noted").
	m.outcomeLine = regexp.MustCompile(`(?i)^(?:` + in(OutcomeLabel, l) + `[*_]*[ \t]*:[ \t]*[*_]*[ \t]*)?(` + outcomes + `)[*_]*[ \t]*(?:$|[.:;,!(—–]|-\s)`)
	// The letter, then nothing or punctuation ("VERDICT: A", never
	// "Verdict: A clear improvement").
	m.verdictLit = regexp.MustCompile(`(?m)(?i:` + in(VerdictLabel, l) + `):\s*(?:A|B|` + in(TieLetter, l) + `)[ \t]*(?:$|[^\pL\pN_ \t])`)
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
// the language the label was written in.
func BarLine(line string) (value string, l Lang, ok bool) {
	for _, l := range langs {
		if m := byLang[l].bar.FindStringSubmatch(line); m != nil {
			return strings.TrimSpace(m[1]), l, true
		}
	}
	return "", "", false
}

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

// RoundAnchor reads s as the line that opens a round: its number.
func RoundAnchor(s string) (n int, ok bool) {
	for _, l := range langs {
		if m := byLang[l].round.FindStringSubmatch(s); m != nil {
			n, _ = strconv.Atoi(m[1])
			return n, true
		}
	}
	return 0, false
}

// Outcome returns the first outcome word in s, in any language, or "".
// Classify it with Is(Loss, word).
func Outcome(s string) string {
	word, at := "", -1
	for _, l := range langs {
		if m := byLang[l].outcome.FindStringSubmatchIndex(s); m != nil && (at < 0 || m[2] < at) {
			word, at = s[m[2]:m[3]], m[2]
		}
	}
	return word
}

// OutcomeLine returns the outcome a line states on its own ("Veredito:
// derrota", "Loss."), or "".
func OutcomeLine(s string) string {
	for _, l := range langs {
		if m := byLang[l].outcomeLine.FindStringSubmatch(s); m != nil {
			return m[1]
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
			parts = append(parts, in(BarLabel, l)+`:\s*(?:`+words(l, NotApplicable, Waived)+`)\s*\(`)
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
