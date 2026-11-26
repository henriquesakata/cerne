package app

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// The owner's answers to a decision of the risk gate (F-0518). In
// calibration, where the gate holds everything, they read "it did need me"
// and "it did not".
const (
	FeedbackShouldHaveAsked = "should_have_asked" // the delivery needed the owner
	FeedbackNotNeeded       = "not_needed"        // the delivery did not need the owner
)

// The owner's marks on a card for the risk gate (F-0518 crit. 14), stored in
// accept_mode; "gate", stored as empty, lets the gate decide.
const (
	AcceptModeHuman = "human" // held for a person, in any phase
	AcceptModeAuto  = "auto"  // the sensitive-area condition is waived
)

// ErrInvalidAcceptMode refuses a mark outside human, auto and gate.
var ErrInvalidAcceptMode = errors.New("invalid accept mode")

// riskFeedbackEvent is the event an answer appends to the card.
const riskFeedbackEvent = "feature.risk_feedback"

// Refusals of a feedback (F-0518 crit. 2).
var (
	ErrNoRiskDecision   = errors.New("no risk-gate decision")
	ErrFeedbackMismatch = errors.New("the answer does not match the risk gate's decision")
)

// RiskFeedback is one answer of the owner on a card the risk gate decided.
type RiskFeedback struct {
	Answer string
	Reason string
	// AddPaths and AddFacets go with should-have-asked (crit. 5): what the
	// owner marks to need a person from now on, added to the lists at once.
	AddPaths, AddFacets []string
}

// RiskAnswer is the answer that counts on a card: the latest one given after
// the gate's last decision.
type RiskAnswer struct {
	Answer string    `json:"answer"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
	// What the answer added to the lists (crit. 5).
	AddedPaths  []string `json:"added_paths,omitempty"`
	AddedFacets []string `json:"added_facets,omitempty"`
	idx         int      // the answer's position in the log
}

func newRiskAnswer(ev domain.Event, idx int) *RiskAnswer {
	p := ev.Payload
	return &RiskAnswer{Answer: p["answer"], Reason: p["reason"], At: ev.At, AddedPaths: splitList(p["added_paths"]), AddedFacets: splitList(p["added_facets"]), idx: idx}
}

// RiskDecision is the risk gate's last decision on a card, read from its
// events (F-0516 records them), with the owner's answer to it.
type RiskDecision struct {
	Risk        string      `json:"risk"`                  // low or held
	Calibration bool        `json:"calibration,omitempty"` // held because the gate was calibrating
	Calibrating bool        `json:"calibrating,omitempty"` // the gate is still calibrating: a held-in-calibration card is still asked
	Would       string      `json:"would,omitempty"`       // in calibration, what the gate would have decided: low or held
	Mode        string      `json:"mode,omitempty"`        // the card's accept_mode at the complete
	Files       []string    `json:"files"`                 // every file the delivery changed
	Dirs        []string    `json:"dirs"`                  // the folders of Files, as globs (dir/**); a root file stands for itself
	Facets      []string    `json:"facets"`                // the sensitive facets the gate found
	At          time.Time   `json:"at"`
	Feedback    *RiskAnswer `json:"feedback,omitempty"`
	payload     map[string]string
	idx         int // the decision's position in the log
}

// Says is the decision in words, for a refusal and the CLI.
func (d RiskDecision) Says() string {
	switch {
	case d.Calibration && d.Would == "low":
		return "held in calibration (the gate would have accepted it as low risk)"
	case d.Calibration:
		return "held in calibration (the gate would have held it too)"
	case d.Risk == "low":
		return "accepted as low risk"
	}
	return "held for a person"
}

// takes reports whether answer fits the decision: in calibration both do;
// otherwise should-have-asked answers an accept and not-needed a hold.
func (d RiskDecision) takes(answer string) bool {
	return d.Calibration || (d.Risk == "low") == (answer == FeedbackShouldHaveAsked)
}

// isRiskDecision reports whether ev records a decision of the risk gate on
// its card: a complete under the gate, a task held in its parent's cascade,
// or a task accepted along with its parent on its own measurements.
func isRiskDecision(ev domain.Event) bool {
	if ev.Payload["risk"] == "" {
		return false
	}
	switch ev.Type {
	case eventTypes[domain.ActionComplete], "feature.risk_held":
		return true
	case eventTypes[domain.ActionAccept]:
		return ev.Payload["cascade_from"] != ""
	}
	return false
}

// riskDecisions maps each card to the gate's last decision on it, with the
// answer given after it. A complete under another gate clears the decision:
// the card's last delivery did not cross the risk gate.
func riskDecisions(evs []domain.Event) map[string]*RiskDecision {
	out := map[string]*RiskDecision{}
	for i, ev := range evs {
		switch {
		case isRiskDecision(ev):
			out[ev.EntityID] = newRiskDecision(ev, i)
		case ev.Type == eventTypes[domain.ActionComplete]:
			delete(out, ev.EntityID)
		case ev.Type == riskFeedbackEvent && out[ev.EntityID] != nil:
			out[ev.EntityID].Feedback = newRiskAnswer(ev, i)
		}
	}
	return out
}

func newRiskDecision(ev domain.Event, idx int) *RiskDecision {
	p := ev.Payload
	// An owner's auto mark passes in calibration: that card was accepted, not
	// held in calibration.
	d := &RiskDecision{Risk: p["risk"], Calibration: p["risk_calibration"] != "" && p["risk"] == "held", Would: p["risk_would"], Mode: p["risk_mode"],
		Files: splitList(p["risk_files"]), Facets: splitList(p["risk_facets"]), At: ev.At, payload: p, idx: idx}
	d.Dirs = dirGlobs(d.Files)
	return d
}

// splitList reads a list the payload joins with ", ".
func splitList(s string) []string {
	out := []string{}
	for x := range strings.SplitSeq(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// dirGlobs are the folders of files as path globs (dir/**), sorted; a file
// at the root stands for itself.
func dirGlobs(files []string) []string {
	out := []string{}
	for _, f := range files {
		g := f
		if d := path.Dir(f); d != "." {
			g = d + "/**"
		}
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	slices.Sort(out)
	return out
}

// RiskDecisionOf is the risk gate's last decision on card id, with the
// owner's answer to it; nil when the gate never decided the card.
func RiskDecisionOf(repoDir, projDir, id string) (*RiskDecision, error) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil, err
	}
	d := riskDecisions(evs)[domain.CanonicalID(id)]
	if d != nil {
		d.Calibrating = len(answeredCards(evs)) < projectGates(projDir).RiskCalibration()
	}
	return d, nil
}

// RecordRiskFeedback records the owner's answer on card id (F-0518 crit. 2
// to 4): an event of the card with the answer, the reason and the
// measurements of the decision it answers. A later answer replaces it; the
// log keeps both. The caller checks the actor: the CLI refuses an agent, the
// ui is the person.
func RecordRiskFeedback(repoDir, projDir, id string, fb RiskFeedback) (*RiskDecision, error) {
	if fb.Answer != FeedbackShouldHaveAsked && fb.Answer != FeedbackNotNeeded {
		return nil, fmt.Errorf("unknown answer %q: want %s or %s", fb.Answer, FeedbackShouldHaveAsked, FeedbackNotNeeded)
	}
	if fb.Answer != FeedbackShouldHaveAsked && len(fb.AddPaths)+len(fb.AddFacets) > 0 {
		return nil, fmt.Errorf("%w: paths and facets to mark go with %s, the answer that tightens the gate", ErrFeedbackMismatch, FeedbackShouldHaveAsked)
	}
	// A facet is one of the closed set (domain.ErrUnknownFacet), checked
	// before anything is written.
	facets, err := domain.NormalizeFacets(fb.AddFacets)
	if err != nil {
		return nil, err
	}
	fb.AddFacets = facets
	var d *RiskDecision
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			return "", err
		}
		evs, err := store.ReadEvents(repoDir)
		if err != nil {
			return "", err
		}
		if d = riskDecisions(evs)[f.ID]; d == nil {
			return "", fmt.Errorf("%w: %s was not completed under the risk gate ([gates] accept = \"risk\"), so there is no decision to answer", ErrNoRiskDecision, f.ID)
		}
		if !d.takes(fb.Answer) {
			want := FeedbackNotNeeded
			if d.Risk == "low" {
				want = FeedbackShouldHaveAsked
			}
			return "", fmt.Errorf("%w: %s was %s by the risk gate; the answer to that is %s", ErrFeedbackMismatch, f.ID, d.Says(), want)
		}
		// Tightening is at once (crit. 5): the owner asked, in this act.
		g := projectGates(projDir)
		addedPaths, err := addToList(projDir, "gates.risk_human_paths", g.RiskHumanPaths(), fb.AddPaths)
		if err != nil {
			return "", err
		}
		addedFacets, err := addToList(projDir, "gates.risk_human_facets", g.RiskHumanFacets(), fb.AddFacets)
		if err != nil {
			return "", err
		}
		payload := map[string]string{"answer": fb.Answer}
		maps.Copy(payload, riskKeys(d.payload))
		if r := strings.TrimSpace(fb.Reason); r != "" {
			payload["reason"] = r
		}
		if len(addedPaths) > 0 {
			payload["added_paths"] = strings.Join(addedPaths, ", ")
		}
		if len(addedFacets) > 0 {
			payload["added_facets"] = strings.Join(addedFacets, ", ")
		}
		ev := domain.NewEvent(f.ID, riskFeedbackEvent, payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		d.Feedback = newRiskAnswer(ev, len(evs))
		return "cerne: risk feedback " + f.ID, nil
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// SetFeatureAcceptMode stores the owner's mark on a card not yet concluded
// (F-0518 crit. 14): human, auto, or gate (stored empty), with a feature.set
// event. ErrInvalidAcceptMode for any other value and ErrFeatureFrozen past
// in_development, both wrapped, nothing written. The caller checks the actor.
func SetFeatureAcceptMode(repoDir, projDir, id, mode string) (store.Feature, error) {
	switch mode {
	case AcceptModeHuman, AcceptModeAuto:
	case "gate":
		mode = ""
	default:
		return store.Feature{}, fmt.Errorf("%w %q: want human, auto or gate", ErrInvalidAcceptMode, mode)
	}
	var f store.Feature
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if f, err = store.LoadFeature(repoDir, id); err != nil {
			return "", err
		}
		if !domain.TitleEditable(f.State) {
			return "", fmt.Errorf("%w: %s (%s): the mark goes on a card before its complete", ErrFeatureFrozen, f.ID, f.State)
		}
		f.AcceptMode, f.UpdatedAt = mode, time.Now().UTC()
		if err := store.SaveFeature(repoDir, f); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(f.ID, "feature.set", map[string]string{"accept_mode": mode}, EventActor())); err != nil {
			return "", err
		}
		return "cerne: set " + f.ID + " accept mode", nil
	})
	if err != nil {
		return store.Feature{}, err
	}
	return f, nil
}

// RiskCalibration is the risk gate's calibration (F-0518 crit. 12, 13): how
// many answers it collects, how many it has, and how the decisions it would
// have taken compare with the answers given in calibration.
type RiskCalibration struct {
	Target    int `json:"target"`    // gates.risk_calibration; 0 = off
	Answers   int `json:"answers"`   // cards the owner answered
	Remaining int `json:"remaining"` // answers left before the gate decides on its own
	Answered  int `json:"answered"`  // answers on cards decided in calibration
	Matched   int `json:"matched"`   // of those, where the decision the gate would have taken matched the answer
	Missed    int `json:"missed"`    // of those, the gate would have let pass and the owner said it needed them
}

// RiskGateView is what the risk gate learned (F-0518), for next and the ui.
type RiskGateView struct {
	Calibration RiskCalibration `json:"calibration"`
	// Questions are the cards decided in calibration, accepted at the merge,
	// that the owner has not answered yet.
	Questions []string `json:"questions"`
	// Proposals are the open changes to the lists (crit. 6 to 8): none
	// applies on its own.
	Proposals []GateProposal `json:"proposals"`
	// Stats counts the gate's last decision on each card in the period.
	Stats RiskStats `json:"stats"`
}

// RiskStats are the cards the gate accepted alone and the ones it held in a
// period, and how many of each got an answer, a fix or a revert (crit. 10).
type RiskStats struct {
	Accepted RiskGroup `json:"accepted"`
	Held     RiskGroup `json:"held"`
}

// RiskGroup is one side of RiskStats.
type RiskGroup struct {
	Cards    int `json:"cards"`
	Feedback int `json:"feedback"`
	Fixed    int `json:"fixed"`
	Reverted int `json:"reverted"`
}

// GateProposal is a change to one of the risk gate's lists that the signals
// call for (F-0518 crit. 6 to 8): to drop a pattern or facet the owner keeps
// answering "not needed" for (relax), or to add the folders of a delivery
// accepted as low risk that was then fixed or reverted (tighten).
type GateProposal struct {
	ID      string   `json:"id"`      // relax:<setting>:<item> or tighten:<card>
	Kind    string   `json:"kind"`    // relax or tighten
	Setting string   `json:"setting"` // gates.risk_human_paths or gates.risk_human_facets
	Add     []string `json:"add,omitempty"`
	Remove  []string `json:"remove,omitempty"`
	After   []string `json:"after"`   // the list once applied
	Cards   []string `json:"cards"`   // the cards that motivated it
	Signals []string `json:"signals"` // what each card said, one line each
	Command string   `json:"command"` // the exact cerne settings set that applies it
}

// The owner's decisions on a proposal.
const (
	ProposalAccepted  = "accepted"
	ProposalDismissed = "dismissed"
)

// ErrUnknownProposal: no open proposal has that id.
var ErrUnknownProposal = errors.New("no open gate proposal")

// ReadRiskGate reads the risk gate's learning from the log; since bounds the
// period of the counts.
func ReadRiskGate(repoDir, projDir string, since time.Time) (RiskGateView, error) {
	v := RiskGateView{Questions: []string{}}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return v, err
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return v, err
	}
	decisions := riskDecisions(evs)
	c := &v.Calibration
	c.Target = projectGates(projDir).RiskCalibration()
	c.Answers = len(answeredCards(evs))
	c.Remaining = max(0, c.Target-c.Answers)
	for _, f := range features {
		d := decisions[f.ID]
		if d == nil || !d.Calibration {
			continue
		}
		if d.Feedback == nil {
			// Asked only while the gate calibrates: past the number, or off,
			// it decides as F-0516 and stops asking (crit. 12).
			if f.State == domain.StateDone && c.Remaining > 0 {
				v.Questions = append(v.Questions, f.ID)
			}
			continue
		}
		c.Answered++
		needed := d.Feedback.Answer == FeedbackShouldHaveAsked
		if (d.Would == "low") != needed {
			c.Matched++
		}
		if d.Would == "low" && needed {
			c.Missed++
		}
	}
	signals := riskSignals(evs)
	v.Proposals = gateProposals(repoDir, projectGates(projDir), evs, features, decisions, signals)
	for _, f := range features {
		d := decisions[f.ID]
		if d == nil || d.At.Before(since) {
			continue
		}
		g := &v.Stats.Held
		if d.Risk == "low" {
			g = &v.Stats.Accepted
		}
		g.Cards++
		if d.Feedback != nil {
			g.Feedback++
		}
		var fixed, reverted bool
		for _, s := range signals[f.ID] {
			if s.idx > d.idx {
				fixed, reverted = fixed || s.fix, reverted || !s.fix
			}
		}
		if fixed {
			g.Fixed++
		}
		if reverted {
			g.Reverted++
		}
	}
	return v, nil
}

// addToList writes into the list key id the items of add it does not hold yet,
// after the current ones, and returns them; nothing to add writes nothing.
// The list's checks (no backslash, one line) refuse a bad item, unwritten.
func addToList(projDir, id string, current, add []string) ([]string, error) {
	var added []string
	for _, x := range add {
		if x = strings.TrimSpace(x); x != "" && !slices.Contains(current, x) && !slices.Contains(added, x) {
			added = append(added, x)
		}
	}
	if len(added) == 0 {
		return nil, nil
	}
	return added, project.SetSettingValue(projDir, id, project.ScopeProject, append(slices.Clone(current), added...))
}

// riskKeys are the gate's measurements of a decision payload: its risk* keys.
func riskKeys(p map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range p {
		if strings.HasPrefix(k, "risk") {
			out[k] = v
		}
	}
	return out
}
