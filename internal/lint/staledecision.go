package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// RuleStaleDecision flags an active decision that shows a sign of abandonment
// (F-0385). Advisory: it never fails the lint and never moves a state — the
// human answers with keep, retire or substituir.
const RuleStaleDecision = "stale-decision"

// defaultStaleDecisionDays is N, the review window of signal 1, when the
// caller passes none.
const defaultStaleDecisionDays = 180

// decisionMentionRx finds decision ids in a doc: the same word-boundary id
// match ref-to-superseded uses (wordRx), for every decision in one pass.
var decisionMentionRx = regexp.MustCompile(`(?i)\bD-\d+\b`)

// staleDecisionFindings flags an active decision on one of two signals of the
// entity graph, never on age alone (a founding decision is the most durable):
//
//  1. it is cited only by closed features — at least one, and by no live
//     feature, active note (body or --informs), active runbook or other
//     active decision — and has not been reviewed for more than windowDays;
//  2. every one of its components is on a newer active decision created
//     after its last review (D-0002 axis) — a possible successor.
//
// The review is ReviewedAt ('cerne decision keep') or, unreviewed, CreatedAt:
// a keep holds until a new signal — windowDays more without a live citation,
// or an overlapping decision created after it. windowDays<=0 is the 180-day
// default; now is injected so the rule is deterministic under test.
func staleDecisionFindings(repoDir string, features []store.Feature, decisions []store.Decision, notes []store.Note, runbooks []store.Runbook, windowDays int, now time.Time) ([]Finding, error) {
	if windowDays <= 0 {
		windowDays = defaultStaleDecisionDays
	}
	active := map[string]bool{}
	for _, d := range decisions {
		if d.State == domain.DecisionActive {
			active[d.ID] = true
		}
	}
	if len(active) == 0 {
		return nil, nil
	}

	liveCited := map[string]bool{}
	closedBy := map[string][]string{} // decision → "F-0001 implemented", in feature order
	// mentions returns the active decisions the docs at rels cite, minus self.
	mentions := func(self string, rels ...string) ([]string, error) {
		var ids []string
		for _, rel := range rels {
			data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, m := range decisionMentionRx.FindAllString(string(data), -1) {
				id := strings.ToUpper(m)
				if active[id] && id != self && !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
		return ids, nil
	}
	for _, f := range features {
		entries, err := os.ReadDir(filepath.Join(repoDir, "content", "features", f.ID))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var rels []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				rels = append(rels, "content/features/"+f.ID+"/"+e.Name())
			}
		}
		ids, err := mentions("", rels...)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if liveFeatureStates[f.State] {
				liveCited[id] = true
			} else {
				closedBy[id] = append(closedBy[id], f.ID+" "+string(f.State))
			}
		}
	}
	markLive := func(self, rel string) error {
		ids, err := mentions(self, rel)
		for _, id := range ids {
			liveCited[id] = true
		}
		return err
	}
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		for _, id := range n.Informs {
			liveCited[id] = true
		}
		if err := markLive("", "content/notes/"+n.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, rb := range runbooks {
		if rb.State == domain.RunbookActive {
			if err := markLive("", "content/runbooks/"+rb.ID+".md"); err != nil {
				return nil, err
			}
		}
	}
	for _, d := range decisions {
		if d.State == domain.DecisionActive {
			if err := markLive(d.ID, "content/decisions/"+d.ID+".md"); err != nil {
				return nil, err
			}
		}
	}

	window := time.Duration(windowDays) * 24 * time.Hour
	var findings []Finding
	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		base := d.CreatedAt
		if d.ReviewedAt != nil && d.ReviewedAt.After(base) {
			base = *d.ReviewedAt
		}
		var reasons []string
		if age := now.Sub(base); !liveCited[d.ID] && len(closedBy[d.ID]) > 0 && age > window {
			reasons = append(reasons, fmt.Sprintf("cited only by closed features (%s) and by no live entity, unreviewed for %d days", strings.Join(closedBy[d.ID], ", "), int(age.Hours()/24)))
		}
		var newer []string
		if len(d.Components) > 0 {
			for _, e := range decisions {
				if e.State == domain.DecisionActive && e.ID != d.ID && e.CreatedAt.After(base) && containsAll(e.Components, d.Components) {
					newer = append(newer, e.ID)
				}
			}
		}
		by := "D-…"
		if len(newer) > 0 {
			by = newer[0]
			reasons = append(reasons, fmt.Sprintf("every component (%s) is also on the newer active %s — a possible successor", strings.Join(d.Components, ", "), strings.Join(newer, ", ")))
		}
		if len(reasons) == 0 {
			continue
		}
		findings = append(findings, Finding{
			Rule:   RuleStaleDecision,
			Entity: d.ID,
			Detail: fmt.Sprintf("%s — confirm it with 'cerne decision keep %[2]s', retire it with 'cerne decision retire %[2]s --reason \"…\"' or supersede it with 'cerne decision substituir %[2]s --by %[3]s --reason \"…\"'", strings.Join(reasons, "; "), d.ID, by),
		})
	}
	return findings, nil
}

// containsAll reports whether every element of sub is in set.
func containsAll(set, sub []string) bool {
	for _, s := range sub {
		if !slices.Contains(set, s) {
			return false
		}
	}
	return true
}
