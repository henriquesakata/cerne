package app

import (
	"errors"
	"fmt"

	"github.com/henriquesakata/cerne/internal/project"
)

// ErrHumanGate is the sentinel for a lifecycle gate reserved to humans
// (F-0131): an agent actor tried to cross approve/accept while the project's
// [gates] config keeps that end human.
var ErrHumanGate = errors.New("human gate")

// humanEscape ends every human-gate refusal (B-0030): a human typing in an
// agent's shell (CLAUDECODE=1 and the other markers make it one) is told how
// to say they are the human, and the agent reading the same text that it is
// not its move.
const humanEscape = ". A human in an agent's shell runs the command in their own terminal, or prefixes it with CERNE_ACTOR=human; an agent never sets it"

// CheckHumanGate refuses when gate is not auto and the caller is an agent
// (CERNE_ACTOR=agent, stamped into agent sessions by `cerne hook install`, or
// an agent-shell marker with no CERNE_ACTOR — see actorEnv). Every gate but
// auto keeps the typed act human: under merge (F-0488) only the forge's
// merge, recorded by AcceptMerged, crosses it.
// Anything else passes — fail-open by design: the gate exists to stop the
// *configured* agent from approving its own spec, not to authenticate humans.
func CheckHumanGate(gate string) error {
	if gate != project.GateAuto && isAgentActor(actorEnv()) {
		return fmt.Errorf("%w: this transition is a human act in this project — ask a human for the approval/acceptance (or configure [gates] in .cernerc for an explicit bypass)%s", ErrHumanGate, humanEscape)
	}
	return nil
}

// autoVia is the mark of an act crossing gate (F-0515): "auto" when an agent
// crosses it set to auto; "" for a person's act, whatever the gate.
func autoVia(gate string, agentAct bool) string {
	if gate == project.GateAuto && agentAct {
		return project.GateAuto
	}
	return ""
}

// actSource is who stands behind a transition, where the environment alone
// cannot tell (F-0515).
type actSource int

const (
	srcCLI  actSource = iota // the environment declares the actor
	srcUI                    // the cerne ui form: the person, whatever env the server runs in
	srcSeed                  // a scripted seed (cerne demo): nobody crosses a gate, nothing is marked
)

// agentAct reports whether a transition is an agent's act: only a CLI call
// whose environment declares an agent.
func agentAct(src actSource) bool { return src == srcCLI && isAgentActor(actorEnv()) }

// CheckHumanSetting refuses an agent actor on a setting only a human writes
// (F-0392). Fail-open like CheckHumanGate; the UI server does not call it.
func CheckHumanSetting(id string) error {
	if project.SettingHumanOnly(id) && isAgentActor(actorEnv()) {
		return fmt.Errorf("%w: %s is the project owner's decision — changing it is done in the Settings screen of cerne ui, or by the owner in their own terminal%s", ErrHumanGate, id, humanEscape)
	}
	return nil
}

// CheckOwnerAct refuses an agent actor on an act only the project owner does
// (F-0518): answering a decision of the risk gate, marking a card's accept
// mode. Fail-open like CheckHumanGate; the ui server does not call it, the
// person at the form is the owner.
func CheckOwnerAct(act string) error {
	if isAgentActor(actorEnv()) {
		return fmt.Errorf("%w: %s is the project owner's act — the owner does it in cerne ui or in their own terminal; an agent never does it to pass a delivery%s", ErrHumanGate, act, humanEscape)
	}
	return nil
}

// AgentEnv reports whether the environment declares an agent, by the same
// test as the human gates: `cerne update` leaves the binary to the human
// (F-0476 crit. 10).
func AgentEnv() bool { return isAgentActor(actorEnv()) }

// checkHumanMirror refuses an agent actor on the mirror opt-in (F-0449):
// publishing a card leaves the backend and is hard to undo, like approve and
// accept. Fail-open like CheckHumanGate: it stops the configured agent, it
// does not authenticate a human.
func checkHumanMirror() error {
	if isAgentActor(actorEnv()) {
		return fmt.Errorf("%w: publishing a card to a tracker is a human act — ask a human to run 'cerne feature mirror' in their own terminal", ErrHumanGate)
	}
	return nil
}

// CheckHumanFacilitatorReset refuses an agent actor on `room facilitator
// reset` (B-0083): taking a room's facilitation without its capability is the
// human's recovery, not a participant's shortcut to close the room. Fail-open
// like CheckHumanGate.
func CheckHumanFacilitatorReset() error {
	if isAgentActor(actorEnv()) {
		return fmt.Errorf("%w: recovering a room's facilitation is a human act — ask a human to run 'cerne room facilitator reset' in their own terminal%s", ErrHumanGate, humanEscape)
	}
	return nil
}

// projectGates loads the [gates] block from the project's .cernerc; a missing
// or unreadable config yields the zero value, whose accessors resolve to the
// human default on both ends.
func projectGates(projDir string) project.GatesConfig {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return project.GatesConfig{}
	}
	return cfg.Gates
}
