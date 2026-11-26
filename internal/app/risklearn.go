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
	"github.com/henriquesakata/cerne/internal/store"
)

// The owner's answers to a decision of the risk gate (F-0518). In
// calibration, where the gate holds everything, they read "it did need me"
// and "it did not".
const (
	FeedbackShouldHaveAsked = "should_have_asked" // the delivery needed the owner
	FeedbackNotNeeded       = "not_needed"        // the delivery did not need the owner
)

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
}

// RiskAnswer is the answer that counts on a card: the latest one given after
// the gate's last decision.
type RiskAnswer struct {
	Answer string    `json:"answer"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// RiskDecision is the risk gate's last decision on a card, read from its
// events (F-0516 records them), with the owner's answer to it.
type RiskDecision struct {
	Risk        string      `json:"risk"`                  // low or held
	Calibration bool        `json:"calibration,omitempty"` // held because the gate is still calibrating
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
			out[ev.EntityID].Feedback = &RiskAnswer{Answer: ev.Payload["answer"], Reason: ev.Payload["reason"], At: ev.At}
		}
	}
	return out
}

func newRiskDecision(ev domain.Event, idx int) *RiskDecision {
	p := ev.Payload
	d := &RiskDecision{Risk: p["risk"], Calibration: p["risk_calibration"] != "", Would: p["risk_would"], Mode: p["risk_mode"],
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
func RiskDecisionOf(repoDir, id string) (*RiskDecision, error) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil, err
	}
	return riskDecisions(evs)[domain.CanonicalID(id)], nil
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
	var d *RiskDecision
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
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
		payload := map[string]string{"answer": fb.Answer}
		maps.Copy(payload, riskKeys(d.payload))
		if r := strings.TrimSpace(fb.Reason); r != "" {
			payload["reason"] = r
		}
		ev := domain.NewEvent(f.ID, riskFeedbackEvent, payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		d.Feedback = &RiskAnswer{Answer: fb.Answer, Reason: payload["reason"], At: ev.At}
		return "cerne: risk feedback " + f.ID, nil
	})
	if err != nil {
		return nil, err
	}
	return d, nil
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
