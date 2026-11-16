package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// specBarFindings flags candidates whose written spec names no valid Gauntlet
// bar (F-0308) — the start-of-cycle sibling of result-missing-gauntlet, so the
// approve gate's refusal is visible before anyone asks for the approve. A stub
// spec is incomplete-doc's finding, and past candidacy the bar was accepted.
func specBarFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateCandidate {
			continue
		}
		rel := "content/features/" + f.ID + "/spec.md"
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !doccheck.HasRealContent(data) {
			continue
		}
		barIssue := ""
		for _, is := range doccheck.CheckSpecBody(data, store.DocsLanguage(repoDir), false) {
			if is.Code == doccheck.IssueBarMissing || is.Code == doccheck.IssueBarMalformed {
				barIssue = is.Detail
			}
		}
		if barIssue != "" {
			out = append(out, Finding{
				Rule:   RuleSpecMissingBar,
				Entity: f.ID,
				File:   rel,
				Detail: fmt.Sprintf("%s (%s) — the approve refuses it; 'cerne feature approve %s --check' lists the accepted forms", barIssue, f.Title, f.ID),
			})
		}
	}
	return out, nil
}
