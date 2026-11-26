package app

import (
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
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

// RiskAssessment is the risk gate's reading of one delivery at complete
// (F-0516): the three conditions and the measurements behind them, recorded
// on the complete event and, when all three hold, on the accept.
type RiskAssessment struct {
	Area, Gauntlet, Tests RiskCheck
	Paths                 []string // the changed files a sensitive glob matched
	Facets                []string // the card's facets on the sensitive list
	Verdict               string   // the Gauntlet's last outcome: win, tie, loss, exempt, off or none
	Round                 int      // the number of the Gauntlet's highest round; 0 with none or an unnumbered one
	Tested                string   // "<criteria with a test>/<criteria>"
	Base, Head            string   // the commits measured: merge-base with the default branch, and HEAD; empty when unread
}

// Low reports a delivery of low risk: all three conditions hold.
func (r RiskAssessment) Low() bool { return r.Area.Holds && r.Gauntlet.Holds && r.Tests.Holds }

// Summary is the three measurements in one line.
func (r RiskAssessment) Summary() string {
	return r.Area.Found + "; " + r.Gauntlet.Found + "; " + r.Tests.Found
}

// Held is what each condition that does not hold found, one line each.
func (r RiskAssessment) Held() []string {
	var out []string
	for _, c := range []RiskCheck{r.Area, r.Gauntlet, r.Tests} {
		if !c.Holds {
			out = append(out, c.Found)
		}
	}
	return out
}

// payload is the record of the measurements on the complete event and on the
// accept of a low-risk delivery: risk is low or held, risk_area,
// risk_gauntlet and risk_tests what each condition found, and the raw values
// behind them (the sensitive paths and facets hit, comma-separated; the
// verdict; the criteria with a test).
func (r RiskAssessment) payload() map[string]string {
	risk := "held"
	if r.Low() {
		risk = "low"
	}
	return map[string]string{
		"risk": risk, "risk_area": r.Area.Found, "risk_gauntlet": r.Gauntlet.Found, "risk_tests": r.Tests.Found,
		"risk_paths": strings.Join(r.Paths, ", "), "risk_facets": strings.Join(r.Facets, ", "),
		"risk_verdict": r.Verdict, "risk_round": strconv.Itoa(r.Round), "risk_criteria_tested": r.Tested,
		"risk_base": r.Base, "risk_head": r.Head,
	}
}

// assessRisk measures the three conditions of the risk gate on feature f,
// whose delivery is the git checkout at projDir.
func assessRisk(repoDir, projDir string, f store.Feature) RiskAssessment {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		cfg = project.Config{}
	}
	var r RiskAssessment
	var d gauntlet.Delivery
	r.Area, r.Paths, r.Facets, d = riskArea(projDir, cfg.PinBranch, f, cfg.Gates)
	r.Base, r.Head = d.Base, d.Head
	result, _ := os.ReadFile(FeatureDocPath(repoDir, f.ID, "result.md"))
	r.Gauntlet, r.Verdict = riskGauntlet(result, cfg.Gates.GauntletEnabled())
	if rounds := lint.GauntletRoundList(result); len(rounds) > 0 {
		r.Round = rounds[len(rounds)-1].N
	}
	if prev, reverted := revertedRiskRound(repoDir, f.ID); r.Gauntlet.Holds && reverted && r.Round <= prev {
		r.Gauntlet = RiskCheck{Found: fmt.Sprintf("reverted after its risk accept on round %d: the win predates the revert, record a new Gauntlet round", prev)}
	}
	r.Tests, r.Tested = riskTests(repoDir, projDir, f)
	return r
}

// revertedRiskRound reports whether feature id was reverted after its last
// accept as low risk and, if so, the round that accept counted; an accept
// that recorded none counts as the highest, so nothing passes it.
func revertedRiskRound(repoDir, id string) (round int, reverted bool) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return math.MaxInt, true // in doubt, hold
	}
	accepted := -1
	for i, ev := range evs {
		if ev.EntityID == id && ev.Type == eventTypes[domain.ActionAccept] && ev.Payload["via"] == project.GateRisk {
			accepted = i
		}
	}
	if accepted < 0 || !slices.ContainsFunc(evs[accepted+1:], func(ev domain.Event) bool {
		return ev.EntityID == id && ev.Type == eventTypes[domain.ActionRevert]
	}) {
		return 0, false
	}
	n, err := strconv.Atoi(evs[accepted].Payload["risk_round"])
	if err != nil {
		return math.MaxInt, true
	}
	return n, true
}

// riskArea is the first condition (crit. 3): the delivery touches no
// sensitive area — no file of the diff from the merge-base with the default
// branch to HEAD matches a sensitive path, and no facet of the card is a
// sensitive one. In doubt it does not hold: a diff that cannot be read, an
// empty one (complete ran outside the delivery's branch) and a card with no
// facet all wait for a person. Its own function: a per-card waiver drops only
// this condition. d is the delivery read, empty when git could not read it.
func riskArea(projDir, pinBranch string, f store.Feature, g project.GatesConfig) (c RiskCheck, paths, facets []string, d gauntlet.Delivery) {
	var why []string
	d, err := gauntlet.ReadDelivery(projDir, pinBranch, f.ID)
	files, base := d.Files, d.Ref
	switch {
	case errors.Is(err, gauntlet.ErrNotGitRepo):
		why = append(why, "the diff cannot be read: not a git repository")
	case errors.Is(err, gauntlet.ErrNoBase):
		why = append(why, "the diff cannot be read: no default branch (origin/<pin_branch>, origin/HEAD or main)")
	case err != nil:
		why = append(why, "the diff cannot be read: "+err.Error())
	case len(files) == 0:
		why = append(why, "the diff from "+base+" to HEAD is empty: run complete in the delivery's worktree")
	case !d.Cited:
		// Another card's checkout (another worktree, the main checkout under
		// cerne ui) would be measured in its place.
		why = append(why, "no commit from "+base+" to HEAD cites "+f.ID+": this checkout is not its delivery — run complete in the delivery's worktree")
		paths = sensitivePaths(files, g.RiskHumanPaths(), d.Prefix)
	default:
		paths = sensitivePaths(files, g.RiskHumanPaths(), d.Prefix)
	}
	if len(paths) > 0 {
		why = append(why, "touches a sensitive path: "+someOf(paths))
	}
	if len(d.Dirty) > 0 {
		why = append(why, "uncommitted changes the diff cannot measure: "+someOf(d.Dirty))
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
		return RiskCheck{Found: strings.Join(why, "; ")}, paths, facets, d
	}
	return RiskCheck{Holds: true, Found: "touches no sensitive path or facet"}, nil, nil, d
}

// sensitivePaths are the files a glob matches: a glob with no slash matches
// the file name at any depth, one with a slash starts at the repository root
// or, for a file under the project dir (prefix, from the root), at the
// project dir.
func sensitivePaths(files, globs []string, prefix string) []string {
	var out []string
	for _, g := range globs {
		// Written as gitignore or Windows would, a pattern must not silently
		// match nothing: backslashes are slashes, a leading slash goes, and a
		// trailing one is the folder's whole tree.
		g = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(g), `\`, "/"), "/")
		if strings.HasSuffix(g, "/") {
			g += "**"
		}
		if g == "" {
			continue
		}
		if !strings.Contains(g, "/") {
			g = "**/" + g
		}
		re, err := domain.GlobRegexp(g)
		if err != nil {
			continue // every glob compiles: GlobRegexp quotes all but * and ?
		}
		for _, f := range files {
			rel, under := strings.CutPrefix(f, prefix)
			if (re.MatchString(f) || under && prefix != "" && re.MatchString(rel)) && !slices.Contains(out, f) {
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

// riskGauntlet is the second condition (crit. 4): the highest-numbered round
// recorded in the result's ## Gauntlet is a readable win of ours against a
// real bar. A tie, a loss, an exempt bar, a last round with no readable
// verdict (an older win never stands in for it), no round and the Gauntlet
// off never hold.
func riskGauntlet(result []byte, gauntletOn bool) (c RiskCheck, verdict string) {
	if !gauntletOn {
		return RiskCheck{Found: "the Gauntlet is off in this project (light mode)"}, "off"
	}
	// An exempt bar is recorded as exempt, whatever its alternative rounds say.
	if lint.GauntletExempt(result) {
		return RiskCheck{Found: "the Gauntlet's bar is exempt (not applicable or waived): no win against a real bar"}, "exempt"
	}
	rounds := lint.GauntletRoundList(result)
	if len(rounds) == 0 {
		return RiskCheck{Found: "the result records no Gauntlet round with a verdict"}, "none"
	}
	last := rounds[len(rounds)-1]
	verdict = last.Outcome
	switch {
	case verdict == "":
		return RiskCheck{Found: fmt.Sprintf("the Gauntlet's last round (%d) has no readable verdict", last.N)}, "none"
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
