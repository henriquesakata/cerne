package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/mdlink"
	"github.com/henriquesakata/cerne/internal/store"
)

// amendmentHeading is a level 2-4 heading opening with Emenda/Emendas/
// Amendment/Amendments, any case: an amendment appended to a document instead
// of written into it (F-0338). The plural is explicit — "## Emendas da
// auditoria" would slip past the singular's \b.
var amendmentHeading = regexp.MustCompile(`(?i)^#{2,4}\s+(emendas?|amendments?)\b`)

// AmendmentLines returns the 1-based lines of text holding an appended
// amendment heading, outside fenced code. The one matcher of the
// appended-amendment rule and of the amend's refusal of such a body.
func AmendmentLines(text string) []int {
	var lines []int
	inFence := false
	for i, l := range strings.Split(text, "\n") {
		if mdlink.IsFenceDelim(l) {
			inFence = !inFence
			continue
		}
		if !inFence && amendmentHeading.MatchString(l) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// appendedAmendmentFindings flags each appended amendment heading in the spec
// or plan of an approved or in-development feature — the states where
// `cerne feature amend --doc` rewrites the document instead. Advisory: live
// documents with such sections exist, and an error would block their push.
func appendedAmendmentFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateApproved && f.State != domain.StateInDevelopment {
			continue
		}
		for _, doc := range []string{"spec", "plan"} {
			rel := "content/features/" + f.ID + "/" + doc + ".md"
			data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, n := range AmendmentLines(string(data)) {
				out = append(out, Finding{
					Rule:   RuleAppendedAmendment,
					Entity: f.ID,
					File:   rel,
					Detail: fmt.Sprintf(`%s line %d: amendment appended as a section — write it into the criterion or task with 'cerne feature amend %s --doc %s --file <%s.md> --reason "<why>"'`, doc, n, f.ID, doc, doc),
				})
			}
		}
	}
	return out, nil
}
