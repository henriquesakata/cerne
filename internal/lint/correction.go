package lint

import (
	"fmt"

	"gitlab.com/rockt/cerne/internal/domain"
)

// CorrectionKind says which path corrects a document of a feature in a state.
type CorrectionKind int

const (
	CorrectByPush   CorrectionKind = iota // live: edit under .docs/ and push
	CorrectByAmend                        // implemented result: feature amend, before the accept
	CorrectByErrata                       // done, or a spec/plan once implemented: an erratum
	CorrectNone                           // rejected, superseded: terminal history
)

// Correction returns how document doc (spec|plan|result) of feature id in the
// given state is corrected, and the command to run with the id interpolated
// ("" when no command changes it). The single source for every message that
// tells a reader how to fix a feature document (F-0307): lint details, push
// and write refusals, the pull hint.
func Correction(state domain.FeatureState, id, doc string) (CorrectionKind, string) {
	switch {
	case domain.TitleEditable(state):
		return CorrectByPush, "cerne push"
	case state == domain.StateRejected || state == domain.StateSuperseded:
		return CorrectNone, ""
	case state == domain.StateImplemented && doc == "result":
		return CorrectByAmend, fmt.Sprintf(`cerne feature amend %s --file <result.md> --reason "<why>"`, id)
	default:
		return CorrectByErrata, fmt.Sprintf("cerne feature errata %s --file <errata.md>", id)
	}
}
