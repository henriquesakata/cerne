package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// resultGauntletStrictSince is the cutoff of the content reader (F-0304):
// features completed before it also pass on the legacy per-line predicate,
// since barra and a quote in every round are stricter than what it demanded.
var resultGauntletStrictSince = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

var (
	gauntletHeadingRe = regexp.MustCompile(`(?m)^\s*##\s+Gauntlet\s*$`)
	nextHeadingRe     = regexp.MustCompile(`(?m)^\s*##\s`)
	// Leading markdown markers of a line: heading, list, quote, table, "N." and
	// emphasis delimiters.
	lineMarkersRe = regexp.MustCompile(`^(?:[\s#>*_|-]|\d+\.\s)*`)
	roundAnchorRe = regexp.MustCompile(`(?i)^Rodada\s+(\d+)\b`)
	barLineRe     = regexp.MustCompile(`(?i)^Barra:[\s*_]*([^\s*_].*)$`)
	criticLineRe  = regexp.MustCompile(`(?i)^Achados do cr(?:í|i\x{0301}|i)tico`)
	verdictLitRe  = regexp.MustCompile(`(?i)VEREDITO:\s*(?:A|B|EMPATE)\b`)
	quoteRe       = regexp.MustCompile(`"[^"]*\S[^"]*"|“[^”]*\S[^”]*”|«[^»]*\S[^»]*»`)
	// A cited verdict: vitória/derrota/empate (NFC or NFD accents), which the
	// record format puts on a "Rodada N — <veredito>: \"<citação>\"" line.
	verdictRe = regexp.MustCompile(`(?i)\b(vit(?:ó|o\x{0301}|o)ria|derrota|empate)\b`)
	// Recorded exemptions count as a complete record: no rounds exist to cite.
	exemptionRe = regexp.MustCompile(`(?i)Barra:\s*(n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel|dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana)\s*\(`)
)

// gauntletSection returns the body of the "## Gauntlet" section, and whether
// the heading exists at all.
func gauntletSection(result []byte) (string, bool) {
	loc := gauntletHeadingRe.FindIndex(result)
	if loc == nil {
		return "", false
	}
	body := result[loc[1]:]
	if next := nextHeadingRe.FindIndex(body); next != nil {
		body = body[:next[0]]
	}
	return string(body), true
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
// cutoff) whose result.md has no "## Gauntlet" record: the section with at
// least one cited verdict, or a recorded exemption (F-0545 crit. 2) — the
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
		resultPath := filepath.Join("content", "features", f.ID, "result.md")
		result, err := os.ReadFile(filepath.Join(repoDir, resultPath))
		if err != nil {
			result = nil // absence of the doc is missing-gate-doc's job; here absence = no record
		}
		section, has := gauntletSection(result)
		detail := "sem seção \"## Gauntlet\""
		if has {
			detail = parseGauntlet(section).problem()
			if detail == "" || (!completedAfter(events, f.ID, resultGauntletStrictSince) && legacyGauntletRecorded(section)) {
				continue
			}
		}
		out = append(out, Finding{
			Rule:   RuleResultMissingGauntlet,
			Entity: f.ID,
			File:   filepath.ToSlash(resultPath),
			Detail: fmt.Sprintf("result.md %s (%s) — registre o loop: Barra + \"Rodada N — <veredito>: \\\"<citação do juiz>\\\"\" + disposição por achado do crítico, ou a dispensa literal (skill cerne-authoring)", detail, f.Title),
		})
	}
	return out
}

type gauntletRound struct {
	n       int  // 0 = implicit round (section without a "Rodada N" anchor)
	verdict bool // VEREDITO: A|B|EMPATE, or an outcome word
	quote   bool // a quoted span with content inside the round's block
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
	blocks := []*strings.Builder{}
	var nums []int
	var cur *strings.Builder // nil = outside any round block
	anchored := false
	implicit := &strings.Builder{}
	inImplicit := true
	for _, line := range strings.Split(section, "\n") {
		s := lineMarkersRe.ReplaceAllString(line, "")
		if m := barLineRe.FindStringSubmatch(s); m != nil && rec.bar == "" {
			rec.bar = m[1]
			rec.exempt = exemptionRe.MatchString("Barra: " + m[1])
			continue
		}
		if criticLineRe.MatchString(s) {
			cur, inImplicit = nil, false
			continue
		}
		if m := roundAnchorRe.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			cur, inImplicit, anchored = &strings.Builder{}, false, true
			blocks, nums = append(blocks, cur), append(nums, n)
		}
		if cur != nil {
			cur.WriteString(line + "\n")
		} else if inImplicit {
			implicit.WriteString(line + "\n")
		}
	}
	if !anchored {
		blocks, nums = []*strings.Builder{implicit}, []int{0}
	}
	for i, b := range blocks {
		text := b.String()
		rec.rounds = append(rec.rounds, gauntletRound{
			n:       nums[i],
			verdict: verdictLitRe.MatchString(text) || verdictRe.MatchString(text),
			quote:   quoteRe.MatchString(text),
		})
	}
	return rec
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
