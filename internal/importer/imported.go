package importer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
)

// ImportedSources returns the set of imported_from sources already present in
// the backend's content tree, so a re-run of import can skip them. Documents
// without the frontmatter key (hand-authored) contribute nothing; a missing
// content/ directory yields an empty set.
func ImportedSources(repoDir string) (map[string]bool, error) {
	root := filepath.Join(repoDir, "content")
	sources := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if src := doccheck.FrontmatterValue(data, "imported_from"); src != "" {
			sources[src] = true
		}
		return nil
	})
	return sources, err
}

// Unimported splits cands into those whose Source is not yet in imported
// (kept, original order preserved) and counts the rest as skipped. It lets a
// re-run of import bring in only the documents added since the last run.
func Unimported(cands []Candidate, imported map[string]bool) (kept []Candidate, skipped int) {
	for _, c := range cands {
		if imported[c.Source] {
			skipped++
			continue
		}
		kept = append(kept, c)
	}
	return kept, skipped
}
