package lint

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// resultGauntletSince is the rule's adoption cutoff (F-0545): only features
// completed after it are checked — the sibling result-missing-evidence rule
// (F-0272) set the pattern, and flagging pre-convention history is noise.
var resultGauntletSince = time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

// resultGauntletStrictSince is the cutoff of the content reader (F-0304), in
// UTC: features completed before it also pass on the legacy per-line
// predicate, since barra and a quote in every round are stricter than what it
// demanded.
var resultGauntletStrictSince = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

var (
	gauntletHeadingRe = regexp.MustCompile(`(?m)^\s*##\s+Gauntlet\s*$`)
	// HTML comments are template scaffold (doccheck reads them so), never
	// record: the result template's Gauntlet guide carries the literal forms.
	// An opener written as code ("`<!--`") is not one, so a backtick before
	// it disqualifies the match (the preceding byte is kept by the replace).
	nextHeadingRe = regexp.MustCompile(`(?m)^\s*##\s`)
	// Leading markdown markers of a line: heading, list, quote, table, "N."/"N)"
	// and emphasis delimiters (NBSP counts as space).
	lineMarkersRe = regexp.MustCompile(`^` + doclang.LineMarkers)
	headingRe     = regexp.MustCompile(`^\s*#{1,6}(\s|$)`)
	listItemRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)
	codeSpanRe    = regexp.MustCompile("`[^`\n]*`")
	quoteRe       = regexp.MustCompile(`["“”][^"“”]*[^\s"“”][^"“”]*["“”]|«[^»]*[^\s»][^»]*»`)
	// The words of the record (bar, round, outcome, verdict, critic) and the
	// patterns that recognize them live in internal/doclang, per language.
)

// gauntletSections returns the body of every "## Gauntlet" section, in order.
func gauntletSections(result []byte) []string {
	var out []string
	for _, loc := range gauntletHeadingRe.FindAllIndex(result, -1) {
		body := result[loc[1]:]
		if next := nextHeadingRe.FindIndex(body); next != nil {
			body = body[:next[0]]
		}
		out = append(out, string(body))
	}
	return out
}

// StripHTMLComments drops HTML comments (template scaffold, never record).
func StripHTMLComments(b []byte) []byte { return doccheck.StripHTMLComments(b) }

// legacyGauntletRecorded is the pre-F-0304 per-line predicate, kept verbatim
// for features completed before resultGauntletStrictSince. It reports whether the section carries at least one cited
// verdict — the verdict word and the quote on the same line, so a stray quote
// elsewhere in the section cannot vouch for an unquoted verdict — or a
// recorded exemption.
func legacyGauntletRecorded(section string) bool {
	if doclang.LegacyExemption(section) {
		return true
	}
	for _, line := range strings.Split(section, "\n") {
		if doclang.LooseOutcome(line) && strings.ContainsAny(line, `"“”`) {
			return true
		}
	}
	return false
}

// resultGauntletFindings flags implemented/done features (completed after the
// cutoff) whose result.md has no complete "## Gauntlet" record: the section
// with a Barra line (or a recorded exemption, F-0545 crit. 2) and, in every
// round, a verdict and a quoted citation of the judge (F-0304). Before
// resultGauntletStrictSince the legacy per-line predicate also vouches. The
// sibling of result-missing-evidence, charging the loop instead of the
// criteria.
func resultGauntletFindings(repoDir string, features []store.Feature, events []domain.Event) []Finding {
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateImplemented && f.State != domain.StateDone {
			continue
		}
		if !completedAfter(events, f.ID, resultGauntletSince) {
			continue
		}
		legacy := !completedAfter(events, f.ID, resultGauntletStrictSince)
		out = append(out, gauntletFinding(repoDir, f, features, legacy)...)
	}
	return out
}

// specOf is feature id's spec in the backend; nil when unreadable.
func specOf(repoDir, id string) []byte {
	spec, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	return spec
}

// specBarRule is what a feature's own spec decides about its Gauntlet record
// (T-0007). A task answers to its spec's bar line: exempt owes no record, an
// inherited one owes none while its parent names a bar (why says when it does
// not); a named one owes a round — and so does a parent whose tasks inherit
// its bar. strict then means an exemption written in the result does not
// count. Every other feature is read by its result alone, as before.
func specBarRule(repoDir string, f store.Feature, features []store.Feature) (skip, strict bool, why string) {
	spec := specOf(repoDir, f.ID)
	if strings.HasPrefix(f.ID, "T-") {
		if parent, ok := doccheck.BarInherited(spec); ok {
			switch {
			case parent != f.Parent:
				return false, false, fmt.Sprintf("its bar is inherited from %s, which is not its parent", parent)
			case !doccheck.BarNamed(specOf(repoDir, parent)):
				return false, false, fmt.Sprintf("its bar is inherited from %s, whose spec names no bar of its own", parent)
			}
			return true, false, ""
		}
		if _, err := doccheck.SpecBar(spec); err == nil {
			if doccheck.BarExempt(spec) {
				return true, false, ""
			}
			strict = true
		}
	}
	for _, c := range features {
		if c.Parent != f.ID || !strings.HasPrefix(c.ID, "T-") {
			continue // only the children's specs are read
		}
		if c.State == domain.StateRejected || c.State == domain.StateSuperseded {
			continue // an abandoned task inherits nothing
		}
		if parent, ok := doccheck.BarInherited(specOf(repoDir, c.ID)); ok && parent == f.ID {
			if !doccheck.BarNamed(spec) {
				return false, false, fmt.Sprintf("task %s inherits its bar, but its spec names no bar of its own", c.ID)
			}
			strict = true
		}
	}
	return false, strict, ""
}

// gauntletFinding is the per-feature result-missing-gauntlet check, shared by
// the lint loop and CheckResult. legacy lets the pre-F-0304 predicate vouch;
// features is the backend's, for a parent's inheriting tasks (T-0007).
func gauntletFinding(repoDir string, f store.Feature, features []store.Feature, legacy bool) []Finding {
	resultPath := filepath.Join("content", "features", f.ID, "result.md")
	skip, strict, why := specBarRule(repoDir, f, features)
	if skip {
		return nil
	}
	if why != "" {
		return []Finding{{Rule: RuleResultMissingGauntlet, Entity: f.ID, File: filepath.ToSlash(resultPath),
			Detail: fmt.Sprintf("%s (%s) — %s: an inherited bar needs the parent's named bar and its round in the parent's result; declare the task's own bar or name one on the parent", f.ID, f.Title, why)}}
	}
	legacy = legacy && !strict
	result, err := os.ReadFile(filepath.Join(repoDir, resultPath))
	if err != nil {
		result = nil // absence of the doc is missing-gate-doc's job; here absence = no record
	}
	// Every section is read and one complete record is enough: a template
	// copy or a narrative section above the record must not hide it. With none
	// complete, the last section's problem is told — the template puts the
	// record last, and copies or narrative sit above it.
	lang := store.DocsLanguage(repoDir)
	detail := `no "## Gauntlet" section`
	// The errata entries' Gauntlet sections count like the result's once the
	// feature is closed (F-0305); an entry ends at the next "## errata" heading.
	// They can only vouch: the finding's File is result.md, so the detail is
	// always the result's own problem.
	own := gauntletSections(StripHTMLComments(result))
	for i, section := range append(own, gauntletSections(errataText(repoDir, f))...) {
		rec := parseGauntlet(section)
		exemptInResult := strict && rec.exempt
		if exemptInResult {
			rec.exempt = false // the spec names a bar: the round is owed (T-0007)
		}
		p := rec.problem(lang)
		if p != "" && exemptInResult {
			p = "the spec names a bar, so an exemption written in the result does not count — " + p
		}
		// The legacy predicate vouches only for the result's own sections: an
		// erratum is written now and answers to the strict reader.
		if p == "" || (legacy && i < len(own) && legacyGauntletRecorded(section)) {
			return nil
		}
		if i < len(own) {
			detail = p
		}
	}
	return []Finding{{
		Rule:   RuleResultMissingGauntlet,
		Entity: f.ID,
		File:   filepath.ToSlash(resultPath),
		Detail: fmt.Sprintf("result.md has no complete Gauntlet record: %s (%s) — feature %s: %s. Form: %s (cerne-authoring skill)", detail, f.Title, f.State, resultFix(f, `inside a "## Gauntlet" section`), gauntletForm(lang)),
	}}
}

type gauntletRound struct {
	n       int    // 0 = implicit round (section without a "Rodada N" anchor)
	verdict bool   // VEREDITO: A|B|EMPATE, or an outcome word
	quote   bool   // a quoted span with content inside the round's block
	outcome int8   // 0 unread, 1 vitória/empate, 2 derrota: the anchor line's first outcome word before its citation, else the round's first outcome line
	word    string // the lower-cased outcome word that set outcome ("" when unread)
}

type gauntletRecord struct {
	exempt bool   // one of the two literal exemptions
	bar    string // value of the Barra: line, "" when missing
	rounds []gauntletRound
}

// parseGauntlet reads the body of ONE "## Gauntlet" section by content: the
// Barra line and, per round, a verdict and a quoted citation, whatever the
// markdown layout (heading, list, bold, table row).
func parseGauntlet(section string) gauntletRecord {
	var rec gauntletRecord
	var blocks []*strings.Builder
	var nums []int
	var outcomes []int8 // aligned with blocks
	var words []string  // aligned with blocks: the word behind each outcome
	var implicitOutcome int8
	var implicitWord string
	// raise records the outcome of an outcome-word match; derrota outranks the rest.
	raise := func(o *int8, w *string, word string) {
		if v := outcomeOf(word); v > *o {
			*o, *w = v, strings.ToLower(word)
		}
	}
	cur, curIdx := (*strings.Builder)(nil), -1 // the open round; nil/-1 = outside any
	implicit := &strings.Builder{}
	critic, criticList, criticIndent := false, false, 0
	for _, line := range strings.Split(section, "\n") {
		if headingRe.MatchString(line) {
			critic = false
		}
		s := lineMarkersRe.ReplaceAllString(line, "")
		// Bar and header lines are consumed first, even inside a critic block
		// and even when they cite a verdict (accepted limit).
		// The approve gate's rule (F-0327): a template slot or nothing
		// written is no bar, and only a written exemption is one. A malformed
		// exemption is still written text, so it stays a bar needing rounds.
		if bar, ok := doccheck.BarLine(line); ok {
			if rec.bar == "" && !doccheck.Unwritten(bar) {
				rec.bar = bar
			}
			exempt, err := doccheck.BarValue(bar)
			rec.exempt = rec.exempt || (exempt && err == nil)
			continue
		}
		if doclang.HeaderLine(s) {
			continue
		}
		list := listItemRe.MatchString(line)
		if critic { // Achados block: excluded from every round until it ends
			n, _, anchored := doclang.RoundAnchor(s)
			newRound := anchored && !slices.Contains(nums, n)
			switch {
			case anchored && !list,
				newRound && criticList && len(line)-len(strings.TrimLeft(line, " \t")) <= criticIndent:
				critic = false
			default:
				continue
			}
		}
		if doclang.CriticLine(s) {
			critic, criticList = true, list
			criticIndent = len(line) - len(strings.TrimLeft(line, " \t"))
			continue
		}
		if n, anchorLang, anchored := doclang.RoundAnchor(s); anchored {
			i := slices.Index(nums, n) // same number = same round
			if i < 0 {
				blocks, nums, outcomes, words = append(blocks, &strings.Builder{}), append(nums, n), append(outcomes, 0), append(words, "")
				i = len(nums) - 1
			}
			cur, curIdx = blocks[i], i
			// The outcome is the line's head, before the judge's citation opens:
			// a quote or a note after it ("uma vitória e uma derrota") is not it.
			head := s
			if q := strings.IndexAny(s, "\"“«"); q >= 0 {
				head = s[:q]
			}
			raise(&outcomes[i], &words[i], doclang.Outcome(head, anchorLang))
		} else if word := doclang.OutcomeLine(s); word != "" {
			target, w := &implicitOutcome, &implicitWord
			if curIdx >= 0 {
				target, w = &outcomes[curIdx], &words[curIdx]
			}
			if *target == 0 { // the round's first outcome line decides
				raise(target, w, word)
			}
		}
		if cur != nil {
			cur.WriteString(line + "\n")
		} else {
			implicit.WriteString(line + "\n")
		}
	}
	if len(blocks) == 0 { // no anchor: the section itself is the one round
		blocks, nums, outcomes, words = []*strings.Builder{implicit}, []int{0}, []int8{implicitOutcome}, []string{implicitWord}
	}
	for i, b := range blocks {
		text := b.String()
		// A stated outcome (the anchor's or an outcome line's) is a verdict;
		// otherwise the catalog says whether the round's text carries one.
		rec.rounds = append(rec.rounds, gauntletRound{n: nums[i], verdict: outcomes[i] != 0 || doclang.VerdictIn(text), quote: gauntletQuote(text), outcome: outcomes[i], word: words[i]})
	}
	return rec
}

// GauntletRounds reads a result's own "## Gauntlet" record for the process
// panel's returns guard (F-0321), with the same parser as
// result-missing-gauntlet. Rounds are the distinct "Rodada N" of every
// section — an exempt bar's alternative-standard rounds included — or, with
// none, the sections' implicit round (never an exemption's own prose).
// derrotas counts the rounds read as derrota; with none read, more than one
// round still means at least one return (derrota abre nova rodada).
// hasGauntlet is false — out of the guard, never zero returns — with no round
// or a lone round whose outcome cannot be read (C6).
func GauntletRounds(result []byte) (hasGauntlet bool, derrotas int) {
	rounds := readRounds(result)
	unread := 0
	for _, r := range rounds {
		switch r.outcome {
		case 2:
			derrotas++
		case 0:
			unread++
		}
	}
	switch {
	case derrotas > 0:
		return true, derrotas
	case unread > 0 && len(rounds) > 1:
		return true, len(rounds) - 1
	case unread > 0 || len(rounds) == 0:
		return false, 0
	}
	return true, 0
}

// outcomeOf maps an outcome word to a round outcome: 2 a loss, 1 a win or a
// tie, 0 no word. The catalog classifies it, in any language.
func outcomeOf(word string) int8 {
	switch {
	case word == "":
		return 0
	case doclang.Is(doclang.Loss, word):
		return 2
	}
	return 1
}

// gauntletQuote reports a quoted span with content; a span that only holds a
// template placeholder ("<citação>") is not a citation.
func gauntletQuote(text string) bool {
	for _, q := range quoteRe.FindAllString(codeSpanRe.ReplaceAllString(text, ""), -1) {
		inner := strings.TrimSpace(strings.Trim(q, "\"“”«»"))
		if !doccheck.Unwritten(inner) {
			return true
		}
	}
	return false
}

// problem returns "" for a complete record, else the first missing piece:
// the bar line, then the first incomplete round (citation before verdict).
// The prose is English; the labels it names are language l's (F-0418).
func (r gauntletRecord) problem(l doclang.Lang) string {
	if r.exempt {
		return ""
	}
	if r.bar == "" {
		return fmt.Sprintf(`no "%s:" line`, doclang.Display(doclang.BarLabel, l))
	}
	for _, rd := range r.rounds {
		name := "the round"
		if rd.n > 0 {
			name = fmt.Sprintf(`"%s %d"`, doclang.Display(doclang.Round, l), rd.n)
		}
		if !rd.quote {
			return name + " has no judge quote"
		}
		if !rd.verdict {
			return name + " has no verdict"
		}
	}
	return ""
}

// gauntletForm is the record result-missing-gauntlet teaches: English prose
// with the literal forms of language l (N-0068, F-0418).
func gauntletForm(l doclang.Lang) string {
	d := func(t doclang.Term) string { return doclang.Display(t, l) }
	bar := d(doclang.BarLabel)
	return fmt.Sprintf(`a "%s:" line + one "%s N — <%s|%s|%s>: \"<judge's quote>\"" line per round + a "%s:" line with a disposition per finding, or the literal exemption "%s: %s (<justification>)"`,
		bar, d(doclang.Round), d(doclang.Win), d(doclang.Loss), d(doclang.Tie), d(doclang.CriticFindings), bar, d(doclang.NotApplicable))
}

// GauntletVerdict reads a result's Gauntlet outcome for the accept queue
// (F-0875) from the rounds parseGauntlet reads for result-missing-gauntlet and
// GauntletRounds, so the queue never contradicts the lint: the outcome (win,
// loss or tie, in English whatever the result's language — B-0073) of the
// highest-numbered "Rodada N", else of the implicit round (a "Veredito:
// derrota" line with no anchor), else "exempt" for a recorded exemption. ok is
// false when no outcome can be read: the queue then says so, never infers.
func GauntletVerdict(result []byte) (verdict string, ok bool) {
	last, implicit, exempt := 0, "", false
	for _, section := range gauntletSections(StripHTMLComments(result)) {
		rec := parseGauntlet(section)
		exempt = exempt || rec.exempt
		for _, r := range rec.rounds {
			switch {
			case r.word == "":
			case r.n != 0 && r.n >= last:
				last, verdict = r.n, r.word
			case r.n == 0 && !rec.exempt && implicit == "":
				implicit = r.word
			}
		}
	}
	switch {
	case verdict != "":
		verdict = outcomeName(verdict)
	case implicit != "":
		verdict = outcomeName(implicit)
	case exempt:
		verdict = "exempt"
	}
	return verdict, verdict != ""
}

// GauntletExempt reports whether a result's Gauntlet record exempts the bar
// (not applicable or waived by human decision), whatever rounds it holds: the
// risk gate counts only a win against a real bar (F-0516).
func GauntletExempt(result []byte) bool {
	for _, section := range gauntletSections(StripHTMLComments(result)) {
		if parseGauntlet(section).exempt {
			return true
		}
	}
	return false
}

// outcomeName is the English outcome of a round's word, in any language:
// loss, tie or win, or "" for no word.
func outcomeName(word string) string {
	switch {
	case word == "":
		return ""
	case doclang.Is(doclang.Loss, word):
		return "loss"
	case doclang.Is(doclang.Tie, word):
		return "tie"
	}
	return "win"
}

// RoundOutcome is one round of a result's Gauntlet record (F-0391): its
// number (0 for the implicit round) and the outcome read — win, loss, tie, or
// "" when none can be read.
type RoundOutcome struct {
	N       int    `json:"n"`
	Outcome string `json:"outcome"`
}

// GauntletRoundList lists the rounds GauntletRounds counts, in number order —
// all but an unread implicit round, which is a section nobody judged.
func GauntletRoundList(result []byte) []RoundOutcome {
	var out []RoundOutcome
	for _, r := range readRounds(result) {
		if r.n == 0 && r.outcome == 0 {
			continue
		}
		out = append(out, RoundOutcome{N: r.n, Outcome: outcomeName(r.word)})
	}
	return out
}

// readRounds is the rounds of every "## Gauntlet" section, in number order:
// the distinct "Rodada N" (an exempt bar's included), each with its worst
// outcome, or — with none — the sections' implicit round (never an
// exemption's own prose).
func readRounds(result []byte) []gauntletRound {
	anchored := map[int]gauntletRound{}
	var implicit *gauntletRound
	for _, section := range gauntletSections(StripHTMLComments(result)) {
		rec := parseGauntlet(section)
		for _, r := range rec.rounds {
			switch {
			case r.n != 0:
				if cur, ok := anchored[r.n]; !ok || r.outcome > cur.outcome {
					anchored[r.n] = r
				}
			case !rec.exempt && (implicit == nil || r.outcome > implicit.outcome):
				implicit = &r
			}
		}
	}
	if len(anchored) == 0 && implicit != nil {
		return []gauntletRound{*implicit}
	}
	return slices.SortedFunc(maps.Values(anchored), func(a, b gauntletRound) int { return a.n - b.n })
}
