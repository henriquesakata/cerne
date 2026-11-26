package app

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gauntlet"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// readDelivery and trackedFiles read the checkout for the risk gate; vars so
// a test counts how often a complete reads it.
var (
	readDelivery = gauntlet.ReadDelivery
	trackedFiles = gauntlet.TrackedFiles
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
	// HeldTasks are the implemented tasks the gate holds: their work rides
	// this card's PR, so any of them holds the card too.
	HeldTasks   []HeldTask
	tasks       map[string]RiskAssessment // every implemented task measured, by id
	Mode        string                    // the card's accept_mode (F-0518): human, auto or empty
	Calibration string                    // "<owner answers>/<target>" while the gate calibrates (F-0518); empty past it
	// Files is every file the delivery changed and Patterns the sensitive
	// globs that hit (F-0518): what the owner marks and the proposals count.
	Files, Patterns []string
}

// taskReading is the reading of implemented task id the assessment of its
// parent already made; false when there is none (a nil r included).
func (r *RiskAssessment) taskReading(id string) (RiskAssessment, bool) {
	if r == nil {
		return RiskAssessment{}, false
	}
	t, ok := r.tasks[id]
	return t, ok
}

// HeldTask is an implemented task the risk gate holds, with its own reading.
type HeldTask struct {
	ID   string
	Risk RiskAssessment
}

// would is what the gate decides on its own (F-0518): all three conditions
// hold, no implemented task of the card is held, and the owner did not mark
// the card as needing a person. Calibration records it and holds anyway.
func (r RiskAssessment) would() bool {
	return r.Area.Holds && r.Gauntlet.Holds && r.Tests.Holds && len(r.HeldTasks) == 0 && r.Mode != AcceptModeHuman
}

// rides reports whether an implemented task goes with its parent's decision
// (F-0518): what the gate would decide on the task's own measurements. The
// calibration and the parent's auto mark are the parent's to apply — one rule
// for the parent's check and for the cascade.
func (r RiskAssessment) rides() bool { return r.would() }

// Low reports a delivery of low risk: the gate would accept it and is past
// its calibration, or the owner marked the card to pass alone (F-0518).
func (r RiskAssessment) Low() bool {
	return r.would() && (r.Calibration == "" || r.Mode == AcceptModeAuto)
}

// ownerHold is the line of a card the owner marked as needing a person.
const ownerHold = "the owner marked this card as one that needs a person (accept_mode human)"

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
	for _, t := range r.HeldTasks {
		out = append(out, "task "+t.ID+" is held: "+strings.Join(t.Risk.Held(), "; "))
	}
	if r.Mode == AcceptModeHuman {
		out = append(out, ownerHold)
	}
	if r.Calibration != "" && r.Mode != AcceptModeAuto {
		out = append(out, fmt.Sprintf("the risk gate is in calibration (%s owner answers): it holds every delivery for a person and records what it would have decided (%s)", r.Calibration, r.wouldWord()))
	}
	return out
}

func (r RiskAssessment) wouldWord() string {
	if r.would() {
		return "low"
	}
	return "held"
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
	p := map[string]string{
		"risk": risk, "risk_area": r.Area.Found, "risk_gauntlet": r.Gauntlet.Found, "risk_tests": r.Tests.Found,
		"risk_facets": strings.Join(r.Facets, ", "),
		"risk_verdict": r.Verdict, "risk_round": strconv.Itoa(r.Round), "risk_criteria_tested": r.Tested,
		"risk_base": r.Base, "risk_head": r.Head, "risk_tasks": strings.Join(heldTaskLines(r.HeldTasks), " | "),
	}
	if r.Mode != "" {
		p["risk_mode"] = r.Mode
	}
	if r.Calibration != "" {
		p["risk_calibration"], p["risk_would"] = r.Calibration, r.wouldWord()
	}
	// The file lists ride every event that copies the decision (F-0518):
	// capped, with the total beside them.
	p["risk_files"], p["risk_files_total"] = capped(r.Files), strconv.Itoa(len(r.Files))
	p["risk_paths"], p["risk_paths_total"] = capped(r.Paths), strconv.Itoa(len(r.Paths))
	p["risk_patterns"] = strings.Join(r.Patterns, ", ")
	return p
}

// maxEventList is how many entries of a file list an event keeps.
const maxEventList = 200

// capped joins the first maxEventList items.
func capped(items []string) string {
	return strings.Join(items[:min(len(items), maxEventList)], ", ")
}

func heldTaskLines(tasks []HeldTask) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID + ": " + strings.Join(t.Risk.Held(), "; ")
	}
	return out
}

// riskEnv is what one complete reads once for every card it measures, inside
// the backend's critical section: the config, the checkout's delivery and
// the test marks of the files git tracks.
type riskEnv struct {
	cfg     project.Config
	d       gauntlet.Delivery
	derr    error
	testsOn bool
	idx     lint.TestIndex
}

func readRiskEnv(projDir string) riskEnv {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		cfg = project.Config{}
	}
	env := riskEnv{cfg: cfg, testsOn: project.CriterionTestsEnabled(projDir)}
	env.d, env.derr = readDelivery(projDir, cfg.PinBranch)
	if env.testsOn {
		// Only what git tracks: an ignored or never-added test proves nothing
		// the delivery carries. A tree git cannot read has no test file.
		tracked, _ := trackedFiles(projDir)
		env.idx = lint.ScanTrackedTestMarks(projDir, tracked, cfg.TestPatterns)
	}
	return env
}

// assessRisk measures the three conditions of the risk gate on feature f,
// whose delivery is the git checkout at projDir.
func assessRisk(repoDir, projDir string, f store.Feature) RiskAssessment {
	return assessWith(repoDir, readRiskEnv(projDir), f)
}

// assessWith is assessRisk on a checkout already read.
func assessWith(repoDir string, env riskEnv, f store.Feature) RiskAssessment {
	cfg := env.cfg
	var r RiskAssessment
	r.Area, r.Paths, r.Facets = areaOf(repoDir, env.d, env.derr, f, cfg.Gates)
	r.Base, r.Head, r.Mode, r.Files = env.d.Base, env.d.Head, f.AcceptMode, env.d.Files
	for _, g := range cfg.Gates.RiskHumanPaths() {
		if len(sensitivePaths(r.Paths, []string{g}, env.d.Prefix)) > 0 {
			r.Patterns = append(r.Patterns, g)
		}
	}
	// A native task with an inherited bar is judged by its parent's rounds.
	result, _ := os.ReadFile(FeatureDocPath(repoDir, gauntletSource(repoDir, f), "result.md"))
	r.Gauntlet, r.Verdict = riskGauntlet(result, cfg.Gates.GauntletEnabled())
	if rounds := lint.GauntletRoundList(result); len(rounds) > 0 {
		r.Round = rounds[len(rounds)-1].N
	}
	if prev, reverted := revertedRound(repoDir, f.ID); r.Gauntlet.Holds && reverted && r.Round <= prev {
		r.Gauntlet = RiskCheck{Found: revertedFound(prev)}
	}
	r.Tests, r.Tested = testsOf(repoDir, env, f)
	// The implemented tasks ride this card's PR: each is measured on its own,
	// and any one held holds the card. A task is held by what the gate would
	// decide on it (F-0518): the calibration is the project's, read once here.
	if !isTask(f.ID) {
		tasks, err := childrenOf(repoDir, f.ID, func(c store.Feature) bool { return isTask(c.ID) && c.State == domain.StateImplemented })
		if err != nil { // in doubt, hold
			r.Area = RiskCheck{Found: strings.TrimPrefix(r.Area.Found+"; ", "touches no sensitive path or facet; ") + "the card's tasks cannot be read: " + err.Error()}
		}
		r.tasks = map[string]RiskAssessment{}
		for _, c := range tasks {
			tr := assessWith(repoDir, env, c)
			r.tasks[c.ID] = tr // the cascade reuses it
			if !tr.rides() {
				r.HeldTasks = append(r.HeldTasks, HeldTask{ID: c.ID, Risk: tr})
			}
		}
	}
	if answers, target := riskCalibration(repoDir, cfg.Gates); answers < target {
		r.Calibration = fmt.Sprintf("%d/%d", answers, target)
	}
	return r
}

// riskCalibration is how many cards the owner answered and how many answers
// the gate collects before it decides on its own (F-0518 crit. 12). A log
// that cannot be read counts none: in doubt, calibrate.
func riskCalibration(repoDir string, g project.GatesConfig) (answers, target int) {
	target = g.RiskCalibration()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return 0, target
	}
	return len(answeredCards(evs)), target
}

// answeredCards are the cards with at least one answer of the owner.
func answeredCards(evs []domain.Event) map[string]bool {
	out := map[string]bool{}
	for _, ev := range evs {
		if ev.Type == riskFeedbackEvent {
			out[ev.EntityID] = true
		}
	}
	return out
}

// RiskHeldWait says what a card the risk gate holds waits for (F-0516), the
// way it really closes: a task rides its parent's acceptance while the parent
// is open; any other card, and a task whose parent is already done, waits for
// the human merge of its PR (the task's recorded with accept --merged).
func RiskHeldWait(repoDir string, f store.Feature) string {
	if !isTask(f.ID) || f.Parent == "" {
		return "the human merge of its PR"
	}
	if p, err := store.LoadFeature(repoDir, f.Parent); err == nil && p.State != domain.StateDone {
		return "the acceptance of its parent " + f.Parent
	}
	return "the human merge of its PR (record it with 'cerne feature accept " + f.ID + " --merged')"
}

// revertedRound reports whether feature id was reverted after its last
// complete or accept, whatever its via (a person, the merge, the auto or the
// risk gate, a complete the gate held), and, if so, the Gauntlet round current
// then. An event that recorded none counts as the highest, so no round passes
// it.
func revertedRound(repoDir, id string) (round int, reverted bool) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return math.MaxInt, true // in doubt, hold
	}
	accepted := -1
	for i, ev := range evs {
		if ev.EntityID == id && (ev.Type == eventTypes[domain.ActionAccept] || ev.Type == eventTypes[domain.ActionComplete]) {
			accepted = i
		}
	}
	if accepted < 0 || !slices.ContainsFunc(evs[accepted+1:], func(ev domain.Event) bool {
		return ev.EntityID == id && ev.Type == eventTypes[domain.ActionRevert]
	}) {
		return 0, false
	}
	p := evs[accepted].Payload
	n, err := strconv.Atoi(cmp.Or(p["gauntlet_round"], p["risk_round"]))
	if err != nil {
		return math.MaxInt, true
	}
	return n, true
}

// revertedFound is the held line of a win that predates a revert.
func revertedFound(prev int) string {
	if prev == math.MaxInt {
		return "reverted after a complete or accept that recorded no Gauntlet round: the win cannot be told from an old one — a human accepts on the merge"
	}
	return fmt.Sprintf("reverted after its complete or accept on round %d: the win predates the revert, record a new Gauntlet round", prev)
}

// gauntletRoundOf is the number of the highest round of the Gauntlet record
// that speaks for f ("0" with none): every accept records it, so a revert can
// tell an old win from a new one (F-0516).
func gauntletRoundOf(repoDir string, f store.Feature) string {
	result, _ := os.ReadFile(FeatureDocPath(repoDir, gauntletSource(repoDir, f), "result.md"))
	if rounds := lint.GauntletRoundList(result); len(rounds) > 0 {
		return strconv.Itoa(rounds[len(rounds)-1].N)
	}
	return "0"
}

// gauntletSource is the card whose Gauntlet record speaks for f: a task whose
// bar is inherited from its own parent, which names a bar, owes no round of
// its own (T-0007) and is judged by the parent's rounds; any other card, an
// exempt bar included, by its own.
func gauntletSource(repoDir string, f store.Feature) string {
	if !isTask(f.ID) || f.Parent == "" {
		return f.ID
	}
	spec, _ := os.ReadFile(FeatureDocPath(repoDir, f.ID, "spec.md"))
	parent, inherited := doccheck.BarInherited(spec)
	if !inherited || parent != f.Parent {
		return f.ID
	}
	if pspec, _ := os.ReadFile(FeatureDocPath(repoDir, parent, "spec.md")); !doccheck.BarNamed(pspec) {
		return f.ID
	}
	return parent
}

// riskArea is the first condition (crit. 3): the delivery touches no
// sensitive area — no file of the diff from the merge-base with the default
// branch to HEAD matches a sensitive path, and no facet of the card is a
// sensitive one. In doubt it does not hold: a diff that cannot be read, an
// empty one (complete ran outside the delivery's branch) and a card with no
// facet all wait for a person. Its own function: a per-card waiver drops only
// this condition. d is the delivery read, empty when git could not read it.
// The owner's accept_mode auto (F-0518) waives the sensitivity judgment — the
// paths, the facets, a card with no facet — and keeps measuring them; a
// delivery that cannot be measured (no diff, another card's checkout,
// uncommitted changes) still holds: the owner judged the card, not this
// checkout.
func riskArea(repoDir, projDir, pinBranch string, f store.Feature, g project.GatesConfig) (c RiskCheck, paths, facets []string, d gauntlet.Delivery) {
	d, err := readDelivery(projDir, pinBranch)
	c, paths, facets = areaOf(repoDir, d, err, f, g)
	return c, paths, facets, d
}

// areaOf is riskArea on a delivery already read (err when it could not be).
func areaOf(repoDir string, d gauntlet.Delivery, err error, f store.Feature, g project.GatesConfig) (c RiskCheck, paths, facets []string) {
	var why, sensitive []string
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
	case !d.Cites(f.ID):
		// Another card's checkout (another worktree, the main checkout under
		// cerne ui) would be measured in its place.
		why = append(why, "no commit from "+base+" to HEAD cites "+f.ID+": this checkout is not its delivery — run complete in the delivery's worktree")
		paths = sensitivePaths(files, g.RiskHumanPaths(), d.Prefix)
	default:
		paths = sensitivePaths(files, g.RiskHumanPaths(), d.Prefix)
	}
	if len(paths) > 0 {
		sensitive = append(sensitive, "touches a sensitive path: "+someOf(paths))
	}
	if len(d.Dirty) > 0 {
		why = append(why, "uncommitted changes the diff cannot measure: "+someOf(d.Dirty))
	}
	human := g.RiskHumanFacets()
	chain := chainFacets(repoDir, f)
	for _, x := range chain {
		if slices.ContainsFunc(human, func(h string) bool { return strings.EqualFold(h, x) }) && !slices.Contains(facets, x) {
			facets = append(facets, x)
		}
	}
	// A card needs a facet of its own; its parents' only add sensitivity.
	if len(f.Facets) == 0 {
		sensitive = append(sensitive, "the card has no facet (cerne feature retag "+f.ID+" --facet …)")
	}
	if len(facets) > 0 {
		sensitive = append(sensitive, "carries a sensitive facet: "+strings.Join(facets, ", "))
	}
	waived := f.AcceptMode == AcceptModeAuto && len(sensitive) > 0
	if !waived {
		why = append(why, sensitive...)
	}
	// The gate's own files never pass alone (F-0518, the tech lead's call):
	// under auto, a delivery that changes them could loosen the gate and be
	// accepted by it. Whatever the lists hold.
	if f.AcceptMode == AcceptModeAuto {
		if own := sensitivePaths(files, gateOwnPaths, d.Prefix); len(own) > 0 {
			why = append(why, "touches the gate's own files, which the owner's auto mark never covers: "+someOf(own))
		}
	}
	if len(why) > 0 {
		return RiskCheck{Found: strings.Join(why, "; ")}, paths, facets
	}
	if waived {
		return RiskCheck{Holds: true, Found: "the owner marked this card to pass alone (accept_mode auto), which waives: " + strings.Join(sensitive, "; ")}, paths, facets
	}
	return RiskCheck{Holds: true, Found: "touches no sensitive path or facet"}, nil, nil
}

// gateOwnPaths are the files that configure the gate and the agents it
// judges: cerne's config, the agents' configs and instructions, and the
// hooks and guide cerne installs. Not the owner's list: fixed.
var gateOwnPaths = []string{
	".cernerc",
	"**/.claude/**", "**/.cursor/**", "**/.codex/**", "**/.gemini/**", "**/.agents/**", ".github/copilot-instructions.md",
	".github/hooks/**", ".github/instructions/**", ".github/skills/**",
	".mcp.json", "AGENTS.md", "CLAUDE.md", "GEMINI.md",
}

// chainFacets are the facets of f and of every ancestor up its parent chain:
// a child tagged code under an epic tagged auth carries auth too. A parent
// that cannot be read, or a cycle, ends the walk.
func chainFacets(repoDir string, f store.Feature) []string {
	out := slices.Clone(f.Facets)
	seen := map[string]bool{f.ID: true}
	for id := f.Parent; id != "" && !seen[id]; {
		seen[id] = true
		p, err := store.LoadFeature(repoDir, id)
		if err != nil {
			break
		}
		out = append(out, p.Facets...)
		id = p.Parent
	}
	return out
}

// sensitivePaths are the files a glob matches: a glob with no slash matches
// the file name at any depth, one with a slash starts at the repository root
// or, for a file under the project dir (prefix, from the root), at the
// project dir.
func sensitivePaths(files, globs []string, prefix string) []string {
	var out []string
	for _, g := range globs {
		// Written as gitignore or Windows would, a pattern must not silently
		// match nothing: backslashes are slashes, a leading "./" or "/" goes,
		// and a trailing slash is the folder's whole tree.
		g = strings.ReplaceAll(strings.TrimSpace(g), `\`, "/")
		g = strings.TrimPrefix(strings.TrimPrefix(g, "./"), "/")
		if strings.HasSuffix(g, "/") {
			g += "**"
		}
		if g == "" {
			continue
		}
		forms := []string{g}
		if !strings.Contains(g, "/") {
			// A bare name is a file or a folder at any depth (gitignore).
			forms = []string{"**/" + g, "**/" + g + "/**"}
		}
		for _, form := range forms {
			re, err := domain.GlobRegexp(form)
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
	return testsOf(repoDir, readRiskEnv(projDir), f)
}

// testsOf is riskTests on a checkout already read.
func testsOf(repoDir string, env riskEnv, f store.Feature) (c RiskCheck, tested string) {
	if !env.testsOn {
		return RiskCheck{Found: "lint.criterion_tests is off: no criterion is checked against a test"}, ""
	}
	idx := env.idx
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
