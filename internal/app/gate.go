package app

import (
	"errors"
	"fmt"
	"os"

	"gitlab.com/rockt/cerne/internal/project"
)

// ErrHumanGate is the sentinel for a lifecycle gate reserved to humans
// (F-0131): an agent actor tried to cross approve/accept while the project's
// [gates] config keeps that end human.
var ErrHumanGate = errors.New("gate humano")

// CheckHumanGate refuses when gate is human and the caller is an agent
// (CERNE_ACTOR=agent, stamped into agent sessions by `cerne hook install`).
// Anything else passes — fail-open by design: the gate exists to stop the
// *configured* agent from approving its own spec, not to authenticate humans.
func CheckHumanGate(gate string) error {
	if gate == project.GateHuman && os.Getenv("CERNE_ACTOR") == "agent" {
		return fmt.Errorf("%w: esta transição é um ato humano neste projeto — peça a aprovação/aceite a um humano (ou configure [gates] no .cernerc para bypass explícito)", ErrHumanGate)
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
