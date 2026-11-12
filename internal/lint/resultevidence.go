package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// resultEvidenceSince is the rule's adoption cutoff (F-0272): only features
// completed after it are checked — earlier results were written before the
// evidence convention existed and flagging them would be pure retroactive noise.
var resultEvidenceSince = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

// criteriaSection is the canonical spec heading whose numbered items are the
// acceptance contract this rule enforces. Pinned by test against
// spectmpl.Sections so a template rename cannot silently detach the rule.
const criteriaSection = "Objetivo e critérios de aceitação"

// criteriaSectionIndex is that section's position in the spec template; the
// rule reads it under either language's heading (F-0359).
const criteriaSectionIndex = 1

// maxCriteriaRange caps how wide a cited "critérios N-M" range may expand.
const maxCriteriaRange = 100

var (
	// Top-of-line only: indented numbered lines are sub-items of a criterion,
	// not criteria themselves.
	numberedItemRe = regexp.MustCompile(`^(\d+)[.)]\s`)
	// "critério 2", "critérios 1-3", "critérios 2 a 4", "critérios: 1, 3 e 5" —
	// case-insensitive, tolerating NFC/NFD accents (darwin paste) and a
	// colon/nº after the keyword. A bare digit never matches. Ranges and lists
	// only expand for the plural keyword, so prose like "critério 2 a 3 horas"
	// cannot vouch for criterion 3.
	mentionRe      = regexp.MustCompile(`(?i)crit(?:é|e\x{0301}|e)rio(s?)\s*(?:[:#]|n[ºo°]\.?)?\s*(\d+(?:\s*(?:[-–—]|\ba\b|,|\be\b|\bou\b)\s*\d+)*)`)
	// The English citation (F-0359): "criterion 2", "criteria 1-3", "criteria 2
	// to 4", "criteria: 1, 3 and 5". Group 1 is "a" for the plural.
	mentionENRe    = regexp.MustCompile(`(?i)\bcriteri(?:on|(a))\s*(?:[:#]|no\.?)?\s*(\d+(?:\s*(?:[-–—]|\bto\b|,|\band\b|\bor\b)\s*\d+)*)`)
	mentionNumsRe  = regexp.MustCompile(`\d+`)
	mentionRangeRe = regexp.MustCompile(`(\d+)\s*(?:[-–—]|\ba\b|\bto\b)\s*(\d+)`)
)

// criterionLine is one line of the acceptance-criteria section: N is its
// 1-based line in the spec file, Text the line as written.
type criterionLine struct {
	N    int
	Text string
}

// criteriaLines returns the lines of the acceptance-criteria section of a
// spec, heading excluded — the one cut of "where the criteria are" shared by
// result-missing-evidence and spec-cli-literal-pt (F-0353).
func criteriaLines(spec []byte) []criterionLine {
	var out []criterionLine
	inSection := false
	for i, line := range strings.Split(string(spec), "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			inSection = doclang.IsSection(doclang.Spec, criteriaSectionIndex, h)
			continue
		}
		if inSection {
			out = append(out, criterionLine{i + 1, line})
		}
	}
	return out
}

// specCriteria returns the numbers of the numbered list items inside the
// acceptance-criteria section of a spec, in order. No section or no numbered
// items means no contract to enforce (F-0272, criterion 4).
func specCriteria(spec []byte) []int {
	var out []int
	seen := map[int]bool{}
	for _, l := range criteriaLines(spec) {
		if m := numberedItemRe.FindStringSubmatch(l.Text); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// resultMentions returns the criterion numbers a result.md explicitly cites via
// "critério N" (single, range "N-M"/"N a M", or list "N, M e K").
func resultMentions(result []byte) map[int]bool {
	out := map[int]bool{}
	matches := append(mentionRe.FindAllStringSubmatch(string(result), -1), mentionENRe.FindAllStringSubmatch(string(result), -1)...)
	for _, m := range matches {
		plural, nums := m[1] != "", m[2]
		if !plural {
			// singular vouches for exactly one criterion — the first number;
			// whatever follows ("2 a 3 horas") is prose, not a range
			if d := mentionNumsRe.FindString(nums); d != "" {
				n, _ := strconv.Atoi(d)
				out[n] = true
			}
			continue
		}
		for _, r := range mentionRangeRe.FindAllStringSubmatch(nums, -1) {
			lo, _ := strconv.Atoi(r[1])
			hi, _ := strconv.Atoi(r[2])
			if hi-lo > maxCriteriaRange { // prose like "critérios 1 a 999999" must not flood the map
				continue
			}
			for n := lo; n <= hi; n++ {
				out[n] = true
			}
		}
		for _, d := range mentionNumsRe.FindAllString(nums, -1) {
			n, _ := strconv.Atoi(d)
			out[n] = true
		}
	}
	return out
}

// completedAfter reports whether the feature's latest complete event lands
// after the adoption cutoff. No complete event on record means legacy history
// (imported or pre-event-log) — grandfathered.
func completedAfter(events []domain.Event, id string, since time.Time) bool {
	var last time.Time
	for _, e := range events {
		if e.EntityID == id && e.Type == "feature.completed" && e.At.After(last) {
			last = e.At
		}
	}
	return !last.IsZero() && last.After(since)
}

// resultEvidenceFindings flags implemented/done features (completed after the
// cutoff) whose result.md does not cite every numbered acceptance criterion of
// the spec (F-0272). Reads the canonical backend content/ tree — implemented
// prose is history and never materialized under .docs/.
func resultEvidenceFindings(repoDir string, features []store.Feature, events []domain.Event) []Finding {
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateImplemented && f.State != domain.StateDone {
			continue
		}
		if !completedAfter(events, f.ID, resultEvidenceSince) {
			continue
		}
		out = append(out, evidenceFinding(repoDir, f)...)
	}
	return out
}

// evidenceFinding is the per-feature result-missing-evidence check, shared by
// the lint loop and CheckResult.
func evidenceFinding(repoDir string, f store.Feature) []Finding {
	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
	if err != nil {
		return nil
	}
	criteria := specCriteria(spec)
	if len(criteria) == 0 {
		return nil
	}
	resultPath := filepath.Join("content", "features", f.ID, "result.md")
	result, err := os.ReadFile(filepath.Join(repoDir, resultPath))
	if err != nil {
		result = nil // gate doc coverage is missing-gate-doc's job; here absence = zero evidence
	}
	mentions := resultMentions(StripHTMLComments(result))
	for n := range resultMentions(errataText(repoDir, f)) { // the errata completes what the result lacks (F-0305)
		mentions[n] = true
	}
	var missing []string
	for _, n := range criteria {
		if !mentions[n] {
			missing = append(missing, strconv.Itoa(n))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Finding{{
		Rule:   RuleResultMissingEvidence,
		Entity: f.ID,
		File:   filepath.ToSlash(resultPath),
		Detail: fmt.Sprintf("result.md sem evidência do(s) critério(s) %s da spec (%s) — feature %s: %s. Forma: cite \"critério N\" com a prova de cada um", strings.Join(missing, ", "), f.Title, f.State, resultFix(f, "citando \"critério N\"")),
	}}
}

// errataText returns the feature's errata without HTML comments, for the
// result presence rules (F-0305) — only once the feature is past the live
// states: while live the result itself is the document to fix, and the
// complete gate must not be opened by an erratum. Unreadable or absent errata
// is no errata.
func errataText(repoDir string, f store.Feature) []byte {
	if domain.TitleEditable(f.State) {
		return nil
	}
	er, _ := store.LoadErrata(repoDir, f.ID)
	return StripHTMLComments([]byte(er))
}

// CheckResult returns the result-record findings of ONE feature, ignoring the
// adoption cutoffs and its state — except that the errata counts only once the
// feature is closed (errataText): result-missing-evidence and
// result-missing-gauntlet, in RuleOrder. It reads only the strict Gauntlet
// reader (no legacy tolerance) — the gate at `complete` (F-0303).
func CheckResult(repoDir string, f store.Feature) []Finding {
	return append(evidenceFinding(repoDir, f), gauntletFinding(repoDir, f, false)...)
}
