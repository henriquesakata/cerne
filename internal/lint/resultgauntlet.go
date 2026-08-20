package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// resultGauntletSince is the rule's adoption cutoff (F-0545): only features
// completed after it are checked — the sibling result-missing-evidence rule
// (F-0272) set the pattern, and flagging pre-convention history is noise.
var resultGauntletSince = time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

var (
	gauntletHeadingRe = regexp.MustCompile(`(?m)^\s*##\s+Gauntlet\s*$`)
	nextHeadingRe     = regexp.MustCompile(`(?m)^\s*##\s`)
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

// gauntletRecorded reports whether the section carries at least one cited
// verdict — the verdict word and the quote on the same line, so a stray quote
// elsewhere in the section cannot vouch for an unquoted verdict — or a
// recorded exemption.
func gauntletRecorded(section string) bool {
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
		if has && gauntletRecorded(section) {
			continue
		}
		detail := "result.md sem seção \"## Gauntlet\""
		if has {
			detail = "result.md com \"## Gauntlet\" sem veredito citado"
		}
		out = append(out, Finding{
			Rule:   RuleResultMissingGauntlet,
			Entity: f.ID,
			File:   filepath.ToSlash(resultPath),
			Detail: fmt.Sprintf("%s (%s) — registre o loop: Barra + \"Rodada N — <veredito>: \\\"<citação do juiz>\\\"\" + disposição por achado do crítico, ou a dispensa literal (skill cerne-authoring)", detail, f.Title),
		})
	}
	return out
}
