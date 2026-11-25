package lint

import (
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// RuleReferenceGone flags a current external reference whose last recorded
// check (`cerne reference check`) found the source gone: 404, 410 or a domain
// that no longer exists (F-0501). It reads the stored check only — the lint
// never touches the network. A `changed` source is not a finding: it shows
// only in the output of `reference check`. Blocking, ranked last.
const RuleReferenceGone = "reference-gone"

func referenceGoneFindings(references []store.Reference) []Finding {
	var out []Finding
	for _, r := range references {
		if r.State != domain.ReferenceCurrent || r.Check == nil || r.Check.Status != "gone" {
			continue
		}
		why := "domain not found"
		if r.Check.HTTPStatus != 0 {
			why = fmt.Sprintf("HTTP %d", r.Check.HTTPStatus)
		}
		detail := fmt.Sprintf("source gone at the check of %s (%s)", r.Check.CheckedAt.Format("2006-01-02"), why)
		if len(r.Informs) > 0 {
			detail += "; informs " + strings.Join(r.Informs, ", ")
		}
		out = append(out, Finding{Rule: RuleReferenceGone, Entity: r.ID, Detail: detail})
	}
	return out
}
