package lint

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// RuleHookCitesMissingRule flags a project hook that cites a note that does
// not exist, is not active, or has fewer numbered rules than the one cited
// (F-0340). It fails the lint: a citation with no source is an instruction
// the agent obeys every session while nobody reviews it.
const RuleHookCitesMissingRule = "hook-cites-missing-rule"

// HookText is the text of one project hook: Path is where it lives (relative
// to the project root, or a settings.json locator), Text what the agent reads.
type HookText struct {
	Path string
	Text string
}

// HookCitation is one reference to a note in a hook text. Rules is empty for
// a bare mention of the note.
type HookCitation struct {
	Note  string
	Rules []int
	Line  int
}

// The grammar is a CLOSED list (F-0340 crit. 1): an open pattern ("any number
// near an N-") would flag "N-0600 tem 14 regras". It grows by test.
const (
	hookNum  = `\d+(?:\s*(?:,|e|and)\s*\d+)*`
	hookNote = `N-\d+`
)

var (
	hookRuleOfNote = regexp.MustCompile(`(?i)\b(?:regras?|rules?)\s+(` + hookNum + `)\s+(?:da|de|do|of)\s+\b(` + hookNote + `)\b`)
	hookNoteRule   = regexp.MustCompile(`(?i)\b(` + hookNote + `)\b\s*,?\s*(?:regras?|rules?)\s+(` + hookNum + `)\b`)
	hookBareNote   = regexp.MustCompile(`(?i)\b` + hookNote + `\b`)
	hookNumber     = regexp.MustCompile(`\d+`)
	noteRuleItem   = regexp.MustCompile(`(?m)^(\d+)\.(?:\s|$)`)
)

// NoteRuleCount is the number of rules of a note body (F-0340 crit. 2): the
// highest number among the unindented numbered-list items; 0 with no list.
func NoteRuleCount(body string) int {
	max := 0
	for _, m := range noteRuleItem.FindAllStringSubmatch(body, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > max {
			max = n
		}
	}
	return max
}

// HookCitations lists the note citations of a hook text, line by line, in
// reading order. A bare mention counts only where no form with a rule matched.
func HookCitations(text string) []HookCitation {
	var out []HookCitation
	for i, line := range strings.Split(text, "\n") {
		type hit struct {
			start int
			c     HookCitation
		}
		var hits []hit
		var spans [][2]int
		add := func(m []int, note string, nums string) {
			var rules []int
			for _, d := range hookNumber.FindAllString(nums, -1) {
				if n, err := strconv.Atoi(d); err == nil {
					rules = append(rules, n)
				}
			}
			hits = append(hits, hit{m[0], HookCitation{Note: strings.ToUpper(note), Rules: rules, Line: i + 1}})
			spans = append(spans, [2]int{m[0], m[1]})
		}
		for _, m := range hookRuleOfNote.FindAllStringSubmatchIndex(line, -1) {
			add(m, line[m[4]:m[5]], line[m[2]:m[3]])
		}
		for _, m := range hookNoteRule.FindAllStringSubmatchIndex(line, -1) {
			add(m, line[m[2]:m[3]], line[m[4]:m[5]])
		}
		for _, m := range hookBareNote.FindAllStringIndex(line, -1) {
			covered := false
			for _, s := range spans {
				if m[0] >= s[0] && m[1] <= s[1] {
					covered = true
					break
				}
			}
			if !covered {
				hits = append(hits, hit{m[0], HookCitation{Note: strings.ToUpper(line[m[0]:m[1]]), Line: i + 1}})
			}
		}
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].start < hits[b].start })
		for _, h := range hits {
			out = append(out, h.c)
		}
	}
	return out
}

// CheckHookCitations confirms every note citation in the hooks against the
// backend in repoDir (F-0340 crit. 3-5): the note exists, is active, and has
// the rules cited. A valid citation emits nothing.
func CheckHookCitations(repoDir string, hooks []HookText) ([]Finding, error) {
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.Note, len(notes))
	for _, n := range notes {
		byID[n.ID] = n
	}
	total := map[string]int{}
	var findings []Finding
	for _, h := range hooks {
		for _, c := range HookCitations(h.Text) {
			where := fmt.Sprintf("%s:%d", h.Path, c.Line)
			nt, ok := byID[c.Note]
			var problem string
			switch {
			case !ok:
				problem = fmt.Sprintf("o hook cita a %s, que não existe no backend", c.Note)
			case nt.State != domain.NoteActive:
				problem = fmt.Sprintf("o hook cita a %s, que está %s e não ativa", c.Note, nt.State)
			}
			if problem != "" {
				findings = append(findings, Finding{Rule: RuleHookCitesMissingRule, Entity: c.Note, File: where,
					Detail: problem + ": cite uma nota ativa ou corrija o hook"})
				continue
			}
			if len(c.Rules) == 0 {
				continue
			}
			t, seen := total[c.Note]
			if !seen {
				body, err := store.LoadNoteContent(repoDir, c.Note)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, err
				}
				t = NoteRuleCount(body)
				total[c.Note] = t
			}
			for _, k := range c.Rules {
				if k > t {
					findings = append(findings, Finding{Rule: RuleHookCitesMissingRule, Entity: c.Note, File: where,
						Detail: fmt.Sprintf("o hook cita a regra %d da %s, que tem %d regras numeradas: acrescente a regra à nota (cerne push) ou corrija o hook", k, c.Note, t)})
				}
			}
		}
	}
	return findings, nil
}
