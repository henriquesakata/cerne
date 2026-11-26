package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gauntlet"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// RiskCheck is what one condition of the risk gate found (F-0516): whether
// it holds and, in one phrase, what was measured.
type RiskCheck struct {
	Holds bool
	Found string
}

// riskArea is the first condition (crit. 3): the delivery touches no
// sensitive area — no file of the diff from the merge-base with the default
// branch to HEAD matches a sensitive path, and no facet of the card is a
// sensitive one. In doubt it does not hold: a diff that cannot be read, an
// empty one (complete ran outside the delivery's branch) and a card with no
// facet all wait for a person. Its own function: a per-card waiver drops only
// this condition.
func riskArea(projDir, pinBranch string, f store.Feature, g project.GatesConfig) (c RiskCheck, paths, facets []string) {
	var why []string
	files, base, err := gauntlet.ChangedFiles(projDir, pinBranch)
	switch {
	case errors.Is(err, gauntlet.ErrNotGitRepo):
		why = append(why, "the diff cannot be read: not a git repository")
	case errors.Is(err, gauntlet.ErrNoBase):
		why = append(why, "the diff cannot be read: no default branch (origin/<pin_branch>, origin/HEAD or main)")
	case err != nil:
		why = append(why, "the diff cannot be read: "+err.Error())
	case len(files) == 0:
		why = append(why, "the diff from "+base+" to HEAD is empty: run complete in the delivery's worktree")
	default:
		paths = sensitivePaths(files, g.RiskHumanPaths())
	}
	if len(paths) > 0 {
		why = append(why, "touches a sensitive path: "+someOf(paths))
	}
	human := g.RiskHumanFacets()
	for _, x := range f.Facets {
		if slices.ContainsFunc(human, func(h string) bool { return strings.EqualFold(h, x) }) {
			facets = append(facets, x)
		}
	}
	switch {
	case len(f.Facets) == 0:
		why = append(why, "the card has no facet (cerne feature retag "+f.ID+" --facet …)")
	case len(facets) > 0:
		why = append(why, "carries a sensitive facet: "+strings.Join(facets, ", "))
	}
	if len(why) > 0 {
		return RiskCheck{Found: strings.Join(why, "; ")}, paths, facets
	}
	return RiskCheck{Holds: true, Found: "touches no sensitive path or facet"}, nil, nil
}

// sensitivePaths are the files a glob matches: a glob with no slash matches
// the file name at any depth, one with a slash starts at the repository root.
func sensitivePaths(files, globs []string) []string {
	var out []string
	for _, g := range globs {
		if !strings.Contains(g, "/") {
			g = "**/" + g
		}
		re, err := domain.GlobRegexp(g)
		if err != nil {
			continue // every glob compiles: GlobRegexp quotes all but * and ?
		}
		for _, f := range files {
			if re.MatchString(f) && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// someOf names up to five items and counts the rest.
func someOf(items []string) string {
	if len(items) <= 5 {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:5], ", "), len(items)-5)
}

// riskGauntlet is the second condition (crit. 4): the last round recorded in
// the result's ## Gauntlet is a win of ours against a real bar. A tie, a
// loss, an exempt bar, no verdict and the Gauntlet off never hold.
func riskGauntlet(result []byte, gauntletOn bool) (c RiskCheck, verdict string) {
	if !gauntletOn {
		return RiskCheck{Found: "the Gauntlet is off in this project (light mode)"}, "off"
	}
	verdict, ok := lint.GauntletVerdict(result)
	switch {
	case !ok:
		return RiskCheck{Found: "the result records no Gauntlet round with a verdict"}, "none"
	case lint.GauntletExempt(result):
		return RiskCheck{Found: "the Gauntlet's bar is exempt (not applicable or waived): no win against a real bar"}, verdict
	case verdict != "win":
		return RiskCheck{Found: "the Gauntlet's last round is a " + verdict + ", not a win"}, verdict
	}
	return RiskCheck{Holds: true, Found: "the Gauntlet's last round is a win"}, verdict
}

// riskTests is the third condition (crit. 5): every numbered criterion has a
// marked test and none is verified by hand. The rule off, no test file found
// or a spec with no numbered criterion never hold: nothing was checked.
func riskTests(repoDir, projDir string, f store.Feature) (c RiskCheck, tested string) {
	if !project.CriterionTestsEnabled(projDir) {
		return RiskCheck{Found: "lint.criterion_tests is off: no criterion is checked against a test"}, ""
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		cfg = project.Config{}
	}
	idx := ScanProjectTests(projDir, cfg)
	criteria, withTest, manual := lint.CriterionCoverage(repoDir, f.ID, idx)
	tested = fmt.Sprintf("%d/%d", len(withTest), len(criteria))
	var why []string
	switch {
	case idx.Files == 0:
		why = append(why, "no test file found: the criteria cannot be checked")
	case len(criteria) == 0:
		why = append(why, "the spec has no numbered criterion")
	}
	for _, n := range criteria {
		switch {
		case slices.Contains(manual, n):
			why = append(why, fmt.Sprintf("criterion %d is verified by hand, not by a test", n))
		case !slices.Contains(withTest, n) && idx.Files > 0:
			why = append(why, fmt.Sprintf("criterion %d has no marked test", n))
		}
	}
	if events, err := store.ReadEvents(repoDir); err == nil {
		for _, fi := range lint.CheckCriterionTests(repoDir, f, events, idx) {
			if fi.Rule == lint.RuleOrphanCriterionTest {
				why = append(why, fi.Detail)
			}
		}
	}
	if len(why) > 0 {
		return RiskCheck{Found: strings.Join(why, "; ")}, tested
	}
	return RiskCheck{Holds: true, Found: tested + " criteria with a test"}, tested
}
