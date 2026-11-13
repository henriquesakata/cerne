package app

import (
	"errors"
	"fmt"
	"os"

	"github.com/henriquesakata/cerne/internal/project"
)

// ErrHumanGate is the sentinel for a lifecycle gate reserved to humans
// (F-0131): an agent actor tried to cross approve/accept while the project's
// [gates] config keeps that end human.
var ErrHumanGate = errors.New("human gate")

// CheckHumanGate refuses when gate is human and the caller is an agent
// (CERNE_ACTOR=agent, stamped into agent sessions by `cerne hook install`).
// Anything else passes — fail-open by design: the gate exists to stop the
// *configured* agent from approving its own spec, not to authenticate humans.
func CheckHumanGate(gate string) error {
	if gate == project.GateHuman && isAgentActor(os.Getenv("CERNE_ACTOR")) {
		return fmt.Errorf("%w: this transition is a human act in this project — ask a human for the approval/acceptance (or configure [gates] in .cernerc for an explicit bypass)", ErrHumanGate)
	}
	return nil
}

// CheckHumanSetting refuses an agent actor on a setting only a human writes
// (F-0392). Fail-open like CheckHumanGate; the UI server does not call it.
func CheckHumanSetting(id string) error {
	if project.SettingHumanOnly(id) && isAgentActor(os.Getenv("CERNE_ACTOR")) {
		return fmt.Errorf("%w: %s is the project owner's decision — changing it is done in the Settings screen of cerne ui, or by the owner in their own terminal", ErrHumanGate, id)
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
