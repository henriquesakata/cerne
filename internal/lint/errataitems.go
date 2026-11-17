package lint

import (
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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
			case !it.Open(): // merged, or step-less and decided (the decision retires it)
			case it.Decided():
				out = append(out, Finding{Rule: RuleErrataPending, Entity: rb.ID, File: "content/errata/" + rb.ID + ".md",
					Detail: fmt.Sprintf("erratum %s of %s is decided and not yet merged into the body", it.ID, rb.ID),
					Fix:    "cerne runbook merge " + rb.ID})
			case it.Target == "" && !it.Stepless: // F-0345: forgotten, not chosen
				out = append(out, Finding{Rule: RuleErrataPending, Entity: rb.ID, File: "content/errata/" + rb.ID + ".md",
					Detail: fmt.Sprintf("erratum %s of %s declares no step: --consolidated cannot place it and merge cannot fold it", it.ID, rb.ID),
					Fix: fmt.Sprintf("cerne runbook errata %s --step <step> --file <text>, then cerne runbook decide %s %s --by <alias> --quote \"rewritten with a step\"",
						rb.ID, rb.ID, it.ID)})
			default:
				out = append(out, Finding{Rule: RuleErrataPending, Entity: rb.ID, File: "content/errata/" + rb.ID + ".md",
					Detail: fmt.Sprintf("erratum %s of %s is still a proposal, with no decision", it.ID, rb.ID),
					Fix:    fmt.Sprintf("cerne runbook decide %s %s --by <alias> --quote \"<decision>\"", rb.ID, it.ID)})
			}
		}
	}
	return out
}
