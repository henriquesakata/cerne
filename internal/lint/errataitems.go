package lint

import (
	"fmt"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// errataPendingFindings (F-0872) flags the errata items of live runbooks that
// sit parked: a proposal nobody decided, or a decision nobody merged. Advisory
// (like stale-pending): a proposal may be waiting on purpose, but never silently.
func errataPendingFindings(repoDir string, runbooks []store.Runbook) []Finding {
	var out []Finding
	for _, rb := range runbooks {
		if rb.State != domain.RunbookActive {
			continue
		}
		doc, err := store.LoadErrata(repoDir, rb.ID)
		if err != nil {
			continue
		}
		for _, it := range store.ParseErrataItems(doc) {
			switch {
			case it.Merged != "":
			case it.Decided() && it.Target == "": // step-less: nothing to fold, the decision retires it
			case it.Decided():
				out = append(out, Finding{Rule: RuleErrataPending, Entity: rb.ID, File: "content/errata/" + rb.ID + ".md",
					Detail: fmt.Sprintf("erratum %s de %s decidido e ainda não fundido no corpo", it.ID, rb.ID),
					Fix:    "cerne runbook merge " + rb.ID})
			default:
				out = append(out, Finding{Rule: RuleErrataPending, Entity: rb.ID, File: "content/errata/" + rb.ID + ".md",
					Detail: fmt.Sprintf("erratum %s de %s ainda é proposta, sem decisão", it.ID, rb.ID),
					Fix:    fmt.Sprintf("cerne runbook decide %s %s --by <apelido> --quote \"<decisão>\"", rb.ID, it.ID)})
			}
		}
	}
	return out
}
