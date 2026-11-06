package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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
	htmlCommentRe = regexp.MustCompile("(?s)(^|[^`])<!--.*?-->")
	// A template placeholder left unfilled, e.g. "<referência + acesso>".
	placeholderRe = regexp.MustCompile(`^<[^<>]*>$`)
	// An exemption whose parenthesized justification is still a placeholder.
	placeholderParenRe = regexp.MustCompile(`\(\s*<[^<>()]*>\s*\)`)
	nextHeadingRe      = regexp.MustCompile(`(?m)^\s*##\s`)
	// Leading markdown markers of a line: heading, list, quote, table, "N."/"N)"
	// and emphasis delimiters (NBSP counts as space).
	lineMarkersRe = regexp.MustCompile(`^(?:[\s\x{00A0}#>*_|+-]|\d+[.)]\s)*`)
	roundAnchorRe = regexp.MustCompile(`^Rodada[\s\x{00A0}]+(\d+)\b`)
	barLineRe     = regexp.MustCompile(`(?i)^Barra[*_]*(?:[ \t(][^:\n]{0,39})?:[\s\x{00A0}*_]*([^\s\x{00A0}*_].*)$`)
	headerLineRe  = regexp.MustCompile(`(?i)^(?:M(?:é|e\x{0301}|e)todo|Sa(?:í|i\x{0301}|i)da)[*_]*:`)
	criticLineRe  = regexp.MustCompile(`(?i)^Achados? do cr(?:í|i\x{0301}|i)tico`)
	headingRe     = regexp.MustCompile(`^\s*#{1,6}(\s|$)`)
	listItemRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)
	codeSpanRe    = regexp.MustCompile("`[^`\n]*`")
	verdictLitRe  = regexp.MustCompile(`(?i:veredito):\s*(?:A|B|EMPATE)\b`)
	quoteRe       = regexp.MustCompile(`["“”][^"“”]*[^\s"“”][^"“”]*["“”]|«[^»]*[^\s»][^»]*»`)
	// A cited verdict: vitória/derrota/empate (NFC or NFD accents), which the
	// record format puts on a "Rodada N — <veredito>: \"<citação>\"" line.
	verdictRe = regexp.MustCompile(`(?i)\b(vit(?:ó|o\x{0301}|o)ria|derrota|empate)\b`)
	// Recorded exemptions count as a complete record: no rounds exist to cite.
	exemptionRe = regexp.MustCompile(`(?i)Barra:\s*(n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel|dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana)\s*\(`)
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

// stripHTMLComments drops HTML comments (template scaffold, never record).
func stripHTMLComments(b []byte) []byte {
	return htmlCommentRe.ReplaceAll(b, []byte("${1}"))
}

// legacyGauntletRecorded is the pre-F-0304 per-line predicate, kept verbatim
// for features completed before resultGauntletStrictSince. It reports whether the section carries at least one cited
// verdict — the verdict word and the quote on the same line, so a stray quote
// elsewhere in the section cannot vouch for an unquoted verdict — or a
// recorded exemption.
func legacyGauntletRecorded(section string) bool {
	if exemptionRe.MatchString(section) {
		return true
	}
	for _, line := range strings.Split(section, "\n") {
		if verdictRe.MatchString(line) && strings.ContainsAny(line, `"“”`) {
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
		out = append(out, gauntletFinding(repoDir, f, legacy)...)
	}
	return out
}

// gauntletFinding is the per-feature result-missing-gauntlet check, shared by
// the lint loop and CheckResult. legacy lets the pre-F-0304 predicate vouch.
func gauntletFinding(repoDir string, f store.Feature, legacy bool) []Finding {
	resultPath := filepath.Join("content", "features", f.ID, "result.md")
	result, err := os.ReadFile(filepath.Join(repoDir, resultPath))
	if err != nil {
		result = nil // absence of the doc is missing-gate-doc's job; here absence = no record
	}
	// Every section is read and one complete record is enough: a template
	// copy or a narrative section above the record must not hide it. With none
	// complete, the last section's problem is told — the template puts the
	// record last, and copies or narrative sit above it.
	detail := "sem seção \"## Gauntlet\""
	// The errata entries' Gauntlet sections count like the result's once the
	// feature is closed (F-0305); an entry ends at the next "## errata" heading.
	// They can only vouch: the finding's File is result.md, so the detail is
	// always the result's own problem.
	own := gauntletSections(stripHTMLComments(result))
	for i, section := range append(own, gauntletSections(errataText(repoDir, f))...) {
		p := parseGauntlet(section).problem()
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
		Detail: fmt.Sprintf("result.md %s (%s) — feature %s: %s. Forma: Barra + \"Rodada N — <veredito>: \\\"<citação do juiz>\\\"\" + disposição por achado do crítico, ou a dispensa literal (skill cerne-authoring)", detail, f.Title, f.State, resultFix(f, "dentro de uma seção \"## Gauntlet\"")),
	}}
}

type gauntletRound struct {
	n       int  // 0 = implicit round (section without a "Rodada N" anchor)
	verdict bool // VEREDITO: A|B|EMPATE, or an outcome word
	quote   bool // a quoted span with content inside the round's block
	defeat  bool // the first outcome word on a "Rodada N" line is derrota
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
	var defeats []bool
	var cur *strings.Builder // nil = outside any round block
	implicit := &strings.Builder{}
	critic, criticList, criticIndent := false, false, 0
	emphasis := strings.NewReplacer("*", "", "_", "", "\u00a0", " ")
	for _, line := range strings.Split(section, "\n") {
		if headingRe.MatchString(line) {
			critic = false
		}
		s := lineMarkersRe.ReplaceAllString(line, "")
		// Bar and header lines are consumed first, even inside a critic block
		// and even when they cite a verdict (accepted limit).
		if m := barLineRe.FindStringSubmatch(s); m != nil {
			value := strings.TrimSpace(emphasis.Replace(m[1]))
			if rec.bar == "" && !placeholderRe.MatchString(value) {
				rec.bar = m[1]
			}
			rec.exempt = rec.exempt || (exemptionRe.MatchString("Barra: "+value) && !placeholderParenRe.MatchString(value))
			continue
		}
		if headerLineRe.MatchString(s) {
			continue
		}
		list := listItemRe.MatchString(line)
		if critic { // Achados block: excluded from every round until it ends
			m := roundAnchorRe.FindStringSubmatch(s)
			newRound := false
			if m != nil {
				n, _ := strconv.Atoi(m[1])
				newRound = !slices.Contains(nums, n)
			}
			switch {
			case m != nil && !list,
				newRound && criticList && len(line)-len(strings.TrimLeft(line, " \t")) <= criticIndent:
				critic = false
			default:
				continue
			}
		}
		if criticLineRe.MatchString(s) {
			critic, criticList = true, list
			criticIndent = len(line) - len(strings.TrimLeft(line, " \t"))
			continue
		}
		if m := roundAnchorRe.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			cur = nil
			i := slices.Index(nums, n) // same number = same round
			if i >= 0 {
				cur = blocks[i]
			} else {
				cur = &strings.Builder{}
				blocks, nums, defeats = append(blocks, cur), append(nums, n), append(defeats, false)
				i = len(nums) - 1
			}
			// The group is vitória, derrota or empate: only derrota starts with d.
			v := verdictRe.FindStringSubmatch(s)
			defeats[i] = defeats[i] || (v != nil && strings.EqualFold(v[1][:1], "d"))
		}
		if cur != nil {
			cur.WriteString(line + "\n")
		} else {
			implicit.WriteString(line + "\n")
		}
	}
	if len(blocks) == 0 { // no anchor: the section itself is the one round
		blocks, nums, defeats = []*strings.Builder{implicit}, []int{0}, []bool{false}
	}
	for i, b := range blocks {
		text := b.String()
		rec.rounds = append(rec.rounds, gauntletRound{n: nums[i], verdict: gauntletVerdict(text), quote: gauntletQuote(text), defeat: defeats[i]})
	}
	return rec
}

// GauntletRounds reads a result's own "## Gauntlet" record for the process
// panel's returns guard (F-0321), with the same parser as
// result-missing-gauntlet: hasGauntlet is false with no section or a recorded
// exemption (out of the guard, never zero returns); derrotas counts the
// distinct rounds whose "Rodada N" line opens with a derrota verdict.
func GauntletRounds(result []byte) (hasGauntlet bool, derrotas int) {
	sections := gauntletSections(stripHTMLComments(result))
	defeated := map[int]bool{}
	for _, section := range sections {
		rec := parseGauntlet(section)
		if rec.exempt {
			return false, 0
		}
		for _, r := range rec.rounds {
			if r.defeat {
				defeated[r.n] = true
			}
		}
	}
	return len(sections) > 0, len(defeated)
}

func gauntletVerdict(text string) bool {
	return verdictLitRe.MatchString(text) || verdictRe.MatchString(text)
}

// gauntletQuote reports a quoted span with content; a span that only holds a
// template placeholder ("<citação>") is not a citation.
func gauntletQuote(text string) bool {
	for _, q := range quoteRe.FindAllString(codeSpanRe.ReplaceAllString(text, ""), -1) {
		inner := strings.TrimSpace(strings.Trim(q, "\"“”«»"))
		if !placeholderRe.MatchString(inner) {
			return true
		}
	}
	return false
}

// problem returns "" for a complete record, else the first missing piece:
// the Barra line, then the first incomplete round (citation before verdict).
func (r gauntletRecord) problem() string {
	if r.exempt {
		return ""
	}
	if r.bar == "" {
		return `sem linha "Barra:"`
	}
	for _, rd := range r.rounds {
		name := "rodada"
		if rd.n > 0 {
			name = fmt.Sprintf("rodada %d", rd.n)
		}
		if !rd.quote {
			return name + " sem citação do juiz"
		}
		if !rd.verdict {
			return name + " sem veredito"
		}
	}
	return ""
}
