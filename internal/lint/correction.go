package lint

import (
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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

// resultFix renders, in Portuguese like the lint details, the way to correct
// the result of f in its state; section names what the errata must carry for
// the rule that fired (the lint reads the errata, F-0305).
func resultFix(f store.Feature, section string) string {
	kind, cmd := Correction(f.State, f.ID, "result")
	switch kind {
	case CorrectByPush:
		return "edite o result em .docs/current/features/ e rode '" + cmd + "'"
	case CorrectByAmend:
		return fmt.Sprintf("o result não volta ao .docs/; corrija com '%s' (texto atual: 'cerne show %s --doc result')", cmd, f.ID)
	case CorrectByErrata:
		return fmt.Sprintf("documento congelado; anexe com '%s', %s (o lint lê a errata)", cmd, section)
	}
	return "nenhum comando altera o documento (histórico terminal)"
}
