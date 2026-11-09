package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// preconditionFindings flags, in active runbooks, a precondition line the
// preflight cannot use as written (F-0874): a verifiable kind malformed or with
// an unsafe argument, or a manual line with no S<n> (it would warn on every
// step of every run). Advisory: the preflight already lists them as manual.
func preconditionFindings(repoDir string, runbooks []store.Runbook) ([]Finding, error) {
	var out []Finding
	for _, rb := range runbooks {
		if rb.State != domain.RunbookActive {
			continue
		}
		rel := "content/runbooks/" + rb.ID + ".md"
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, p := range store.ParsePreconditions(string(data)) {
			if p.Problem != "" {
				out = append(out, Finding{
					Rule:   RulePreconditionMalformed,
					Entity: rb.ID,
					File:   rel,
					Detail: fmt.Sprintf("%s: pré-condição '%s' — %s; o preflight a trata como manual", rel, p.Text, p.Problem),
				})
			}
		}
	}
	return out, nil
}
