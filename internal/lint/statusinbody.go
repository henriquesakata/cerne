package lint

import (
	"fmt"
	"regexp"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// statusLine is a line opening with **Status:** or **Estado:**, any case: a
// lifecycle state written into the body (F-0338).
var statusLine = regexp.MustCompile(`(?i)^\*\*(status|estado):\*\*`)

// statusInBodyFindings flags each status line in the spec or plan of a
// candidate, approved or in-development feature. The closed states are left
// out: their text is frozen, and a warning there has no action.
func statusInBodyFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	return docLineFindings(repoDir, features, []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment}, statusLine, RuleStatusInBody,
		func(id, doc string, n int) string {
			return fmt.Sprintf("%s line %d: a status line in the body goes stale — the state is the card's (line 1 of 'cerne show %s')", doc, n, id)
		})
}
